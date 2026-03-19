package build

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
)

type Repository interface {
	ListProducts(ctx context.Context, platforms []model.SourcePlatform, limit int) ([]model.Product, error)
	EnsurePart(ctx context.Context, part model.Part) (model.Part, error)
	UpsertProductMapping(ctx context.Context, mapping model.ProductPartMapping) error
	UpsertPartMarketSummary(ctx context.Context, summary model.PartMarketSummary) error
}

type Service struct {
	repo  Repository
	clock func() time.Time
}

type normalizedCandidate struct {
	Product         model.Product
	Category        model.PartCategory
	Brand           string
	Model           string
	DisplayName     string
	NormalizedKey   string
	SourcePlatform  model.SourcePlatform
	Availability    string
	MatchConfidence float64
}

type catalogAccumulator struct {
	Category      model.PartCategory
	Brand         string
	Model         string
	DisplayName   string
	NormalizedKey string
	prices        []float64
	platforms     map[model.SourcePlatform]*sourceAccumulator
	brands        map[string]struct{}
}

type sourceAccumulator struct {
	prices []float64
}

var (
	speedRegexp           = regexp.MustCompile(`(?i)\b(3200|3600|4800|5200|5600|6000|6400|6800)\b`)
	capacityRegexp        = regexp.MustCompile(`(?i)(\d+)\s*gb`)
	storageCapacityRegexp = regexp.MustCompile(`(?i)\b(\d+)\s*(tb|gb)\b`)
	intelCPURegexp        = regexp.MustCompile(`(?i)\b(i[3579][ -]?\d{4,5}[a-z]{0,2})\b`)
	amdCPURegexp          = regexp.MustCompile(`(?i)\b(?:ryzen\s*[3579]\s*|r[3579]\s*)?(\d{4,5}(?:x3d|[a-z]{0,2}))\b`)
	rtxGPURegexp          = regexp.MustCompile(`(?i)\b(rtx\s*40\d{2}(?:\s*ti)?(?:\s*super)?)\b`)
	rxGPURegexp           = regexp.MustCompile(`(?i)\b(rx\s*\d{4}(?:\s*xt)?)\b`)
	snSSDRegexp           = regexp.MustCompile(`(?i)\b(sn\d{3,4})\b`)
	evoSSDRegexp          = regexp.MustCompile(`(?i)\b(9[89]0\s*evo|9[89]0\s*pro)\b`)
	nmSSDRegexp           = regexp.MustCompile(`(?i)\b(nm\d{3})\b`)
	mpSSDRegexp           = regexp.MustCompile(`(?i)\b(mp44l)\b`)
	t500SSDRegexp         = regexp.MustCompile(`(?i)\b(t500)\b`)
	p3PlusSSDRegexp       = regexp.MustCompile(`(?i)\b(p3\s*plus)\b`)
)

func New(repo Repository, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{repo: repo, clock: clock}
}

func (s *Service) GeneratePriceCatalog(ctx context.Context, req CatalogRequest) (PriceCatalogResponse, error) {
	if req.UseCase == "" {
		req.UseCase = model.UseCaseGaming
	}
	if req.BuildMode == "" {
		req.BuildMode = model.ModeNewOnly
	}
	if req.Limit <= 0 {
		req.Limit = 500
	}

	platforms, warnings := allowedPlatforms(req.BuildMode)
	products, err := s.repo.ListProducts(ctx, platforms, req.Limit)
	if err != nil {
		return PriceCatalogResponse{}, err
	}

	aggregates := map[string]*catalogAccumulator{}
	groupedCandidates := map[string][]normalizedCandidate{}
	for _, product := range products {
		if isMockProduct(product) {
			continue
		}
		candidate, ok := normalizeProduct(product, req.UseCase)
		if !ok {
			continue
		}
		key := catalogGroupKey(candidate)
		groupedCandidates[key] = append(groupedCandidates[key], candidate)
		acc, ok := aggregates[key]
		if !ok {
			acc = &catalogAccumulator{
				Category:      candidate.Category,
				Brand:         candidate.Brand,
				Model:         candidate.Model,
				DisplayName:   catalogDisplayName(candidate.Category, candidate.Brand, candidate.Model),
				NormalizedKey: key,
				platforms:     map[model.SourcePlatform]*sourceAccumulator{},
				brands:        map[string]struct{}{},
			}
			aggregates[key] = acc
		}
		acc.add(candidate)
	}

	response := PriceCatalogResponse{
		UseCase:   req.UseCase,
		BuildMode: req.BuildMode,
		Warnings:  dedupeStrings(warnings),
		Items:     make([]PriceCatalogItem, 0, len(aggregates)),
	}
	for _, acc := range aggregates {
		response.Items = append(response.Items, acc.toPayload())
		if err := s.persistCatalogAggregate(ctx, acc, groupedCandidates[acc.NormalizedKey]); err != nil {
			return PriceCatalogResponse{}, err
		}
	}
	sort.SliceStable(response.Items, func(i, j int) bool {
		leftCategory := categoryOrder(response.Items[i].Category)
		rightCategory := categoryOrder(response.Items[j].Category)
		if leftCategory != rightCategory {
			return leftCategory < rightCategory
		}
		if response.Items[i].AvgPrice != response.Items[j].AvgPrice {
			return response.Items[i].AvgPrice < response.Items[j].AvgPrice
		}
		return response.Items[i].DisplayName < response.Items[j].DisplayName
	})
	return response, nil
}

func (s *Service) persistCatalogAggregate(ctx context.Context, acc *catalogAccumulator, candidates []normalizedCandidate) error {
	if acc == nil || len(candidates) == 0 {
		return nil
	}
	part, err := s.repo.EnsurePart(ctx, model.Part{
		Category:         acc.Category,
		Brand:            choosePartBrand(acc),
		Model:            acc.Model,
		DisplayName:      catalogDisplayName(acc.Category, choosePartBrand(acc), acc.Model),
		NormalizedKey:    acc.NormalizedKey,
		SourceConfidence: 0.88,
		AliasKeywords:    []string{acc.Model, acc.DisplayName},
	})
	if err != nil {
		return err
	}

	groupedByPlatform := map[model.SourcePlatform][]normalizedCandidate{}
	for _, candidate := range candidates {
		if err := s.repo.UpsertProductMapping(ctx, model.ProductPartMapping{
			ProductID:            candidate.Product.ID,
			PartID:               part.ID,
			MappingStatus:        model.MappingStatus("mapped"),
			MatchConfidence:      candidate.MatchConfidence,
			MatchedBy:            "catalog_aggregation",
			CandidateDisplayName: candidate.DisplayName,
			Reason:               "canonicalized from aggregated price catalog",
		}); err != nil {
			return err
		}
		groupedByPlatform[candidate.SourcePlatform] = append(groupedByPlatform[candidate.SourcePlatform], candidate)
	}

	collectedAt := s.clock().UTC()
	for platform, items := range groupedByPlatform {
		summary := summarizeCandidatesForPlatform(part.ID, platform, items, collectedAt)
		if err := s.repo.UpsertPartMarketSummary(ctx, summary); err != nil {
			return err
		}
	}
	return nil
}

func normalizeProduct(product model.Product, useCase model.UseCase) (normalizedCandidate, bool) {
	category := detectCategory(product)
	if category == "" {
		return normalizedCandidate{}, false
	}
	if isLikelyBundle(category, product.Title) {
		return normalizedCandidate{}, false
	}
	if !isReasonableCollectedPrice(category, product.Price) {
		return normalizedCandidate{}, false
	}
	brand := detectBrand(product.Title)
	modelName := inferCanonicalModel(category, product.Title, brand, useCase)
	normalizedKey := catalogGroupKey(normalizedCandidate{Category: category, Model: modelName})
	return normalizedCandidate{
		Product:         product,
		Category:        category,
		Brand:           brand,
		Model:           modelName,
		DisplayName:     displayName(category, brand, modelName),
		NormalizedKey:   normalizedKey,
		SourcePlatform:  product.SourcePlatform,
		Availability:    product.Availability,
		MatchConfidence: 0.83,
	}, true
}

func isLikelyBundle(category model.PartCategory, title string) bool {
	lower := strings.ToLower(title)
	switch category {
	case model.CategoryCPU, model.CategoryMB:
		return strings.Contains(lower, "套装") || strings.Contains(lower, "板u")
	default:
		return false
	}
}

func isReasonableCollectedPrice(category model.PartCategory, price float64) bool {
	if price <= 0 {
		return false
	}
	switch category {
	case model.CategoryCPU:
		return price <= 2500
	case model.CategoryMB:
		return price <= 2500
	case model.CategoryGPU:
		return price <= 7000
	case model.CategoryRAM:
		return price <= 1500
	case model.CategorySSD:
		return price <= 1500
	case model.CategoryPSU:
		return price <= 1500
	case model.CategoryCase:
		return price <= 1000
	case model.CategoryCooler:
		return price <= 800
	default:
		return true
	}
}

func summarizeCandidatesForPlatform(partID model.ID, platform model.SourcePlatform, candidates []normalizedCandidate, collectedAt time.Time) model.PartMarketSummary {
	prices := make([]float64, 0, len(candidates))
	latestPrice := 0.0
	latestSeen := time.Time{}
	for index, candidate := range candidates {
		prices = append(prices, candidate.Product.Price)
		seenAt := candidate.Product.LastSeenAt
		if seenAt.IsZero() {
			seenAt = candidate.Product.UpdatedAt
		}
		if seenAt.IsZero() {
			seenAt = candidate.Product.CreatedAt
		}
		if seenAt.IsZero() {
			seenAt = collectedAt.Add(time.Duration(index) * time.Millisecond)
		}
		if latestSeen.IsZero() || seenAt.After(latestSeen) {
			latestSeen = seenAt
			latestPrice = candidate.Product.Price
		}
	}
	collectedAtCopy := collectedAt
	return model.PartMarketSummary{
		PartID:          partID,
		SourcePlatform:  platform,
		SnapshotDate:    collectedAt.UTC().Truncate(24 * time.Hour),
		LatestPrice:     latestPrice,
		MinPrice:        minPrice(prices),
		MaxPrice:        maxPrice(prices),
		MedianPrice:     medianPrice(prices),
		P25Price:        percentilePrice(prices, 0.25),
		P75Price:        percentilePrice(prices, 0.75),
		SampleCount:     len(candidates),
		LastCollectedAt: &collectedAtCopy,
	}
}

func (c *catalogAccumulator) add(candidate normalizedCandidate) {
	c.prices = append(c.prices, candidate.Product.Price)
	if candidate.Brand != "" {
		c.brands[candidate.Brand] = struct{}{}
	}
	entry, ok := c.platforms[candidate.SourcePlatform]
	if !ok {
		entry = &sourceAccumulator{}
		c.platforms[candidate.SourcePlatform] = entry
	}
	entry.prices = append(entry.prices, candidate.Product.Price)
}

func (c *catalogAccumulator) toPayload() PriceCatalogItem {
	brand := c.Brand
	if len(c.brands) > 1 {
		brand = "Mixed"
	}
	item := PriceCatalogItem{
		Category:      c.Category,
		Brand:         brand,
		Model:         c.Model,
		DisplayName:   catalogDisplayName(c.Category, brand, c.Model),
		NormalizedKey: c.NormalizedKey,
		SampleCount:   len(c.prices),
		AvgPrice:      averagePrice(c.prices),
		MedianPrice:   medianPrice(c.prices),
		MinPrice:      minPrice(c.prices),
		MaxPrice:      maxPrice(c.prices),
		Platforms:     make([]model.SourcePlatform, 0, len(c.platforms)),
	}
	for platform, source := range c.platforms {
		item.Platforms = append(item.Platforms, platform)
		item.SourceBreakdown = append(item.SourceBreakdown, PriceCatalogSourceItem{
			SourcePlatform: platform,
			SampleCount:    len(source.prices),
			AvgPrice:       averagePrice(source.prices),
			MinPrice:       minPrice(source.prices),
			MaxPrice:       maxPrice(source.prices),
		})
	}
	sort.Slice(item.Platforms, func(i, j int) bool { return item.Platforms[i] < item.Platforms[j] })
	sort.Slice(item.SourceBreakdown, func(i, j int) bool {
		return item.SourceBreakdown[i].SourcePlatform < item.SourceBreakdown[j].SourcePlatform
	})
	return item
}

func averagePrice(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	total := 0.0
	for _, price := range prices {
		total += price
	}
	return math.Round((total/float64(len(prices)))*100) / 100
}

func medianPrice(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	sortedPrices := append([]float64(nil), prices...)
	sort.Float64s(sortedPrices)
	middle := len(sortedPrices) / 2
	if len(sortedPrices)%2 == 1 {
		return sortedPrices[middle]
	}
	return math.Round(((sortedPrices[middle-1]+sortedPrices[middle])/2)*100) / 100
}

func minPrice(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	minimum := prices[0]
	for _, price := range prices[1:] {
		if price < minimum {
			minimum = price
		}
	}
	return minimum
}

func maxPrice(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	maximum := prices[0]
	for _, price := range prices[1:] {
		if price > maximum {
			maximum = price
		}
	}
	return maximum
}

func percentilePrice(prices []float64, percentile float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	if percentile <= 0 {
		return minPrice(prices)
	}
	if percentile >= 1 {
		return maxPrice(prices)
	}
	sortedPrices := append([]float64(nil), prices...)
	sort.Float64s(sortedPrices)
	position := percentile * float64(len(sortedPrices)-1)
	lowerIndex := int(math.Floor(position))
	upperIndex := int(math.Ceil(position))
	if lowerIndex == upperIndex {
		return sortedPrices[lowerIndex]
	}
	weight := position - float64(lowerIndex)
	value := sortedPrices[lowerIndex] + (sortedPrices[upperIndex]-sortedPrices[lowerIndex])*weight
	return math.Round(value*100) / 100
}

func categoryOrder(category model.PartCategory) int {
	for index, item := range orderedCategories() {
		if item == category {
			return index
		}
	}
	return len(orderedCategories()) + 1
}

func catalogGroupKey(candidate normalizedCandidate) string {
	return slugify(string(candidate.Category) + "-" + candidate.Model)
}

func catalogDisplayName(category model.PartCategory, brand, modelName string) string {
	if brand == "" || brand == "Mixed" || strings.Contains(strings.ToLower(modelName), strings.ToLower(brand)) {
		return strings.TrimSpace(fmt.Sprintf("%s %s", category, modelName))
	}
	if strings.HasPrefix(modelName, "DDR") {
		return strings.TrimSpace(fmt.Sprintf("%s %s", category, modelName))
	}
	return displayName(category, brand, modelName)
}

func allowedPlatforms(mode model.BuildMode) ([]model.SourcePlatform, []string) {
	switch mode {
	case model.ModeMixed, model.ModeUsedOnly:
		return []model.SourcePlatform{model.PlatformJD}, []string{"current scope only uses JD collected data; non-JD modes fall back to JD price samples"}
	default:
		return []model.SourcePlatform{model.PlatformJD}, nil
	}
}

func choosePartBrand(acc *catalogAccumulator) string {
	if acc == nil {
		return ""
	}
	if len(acc.brands) == 1 {
		for brand := range acc.brands {
			return brand
		}
	}
	if acc.Brand != "" {
		return acc.Brand
	}
	return "Mixed"
}

func orderedCategories() []model.PartCategory {
	return []model.PartCategory{model.CategoryCPU, model.CategoryMB, model.CategoryGPU, model.CategoryRAM, model.CategorySSD, model.CategoryPSU, model.CategoryCase, model.CategoryCooler}
}

func isMockProduct(product model.Product) bool {
	if product.RawPayload == nil {
		return false
	}
	raw, ok := product.RawPayload["mock"]
	if !ok {
		return false
	}
	flag, ok := raw.(bool)
	return ok && flag
}

func detectCategory(product model.Product) model.PartCategory {
	if raw, ok := product.Attributes["category"].(string); ok {
		switch strings.ToUpper(strings.TrimSpace(raw)) {
		case "CPU":
			return model.CategoryCPU
		case "MB", "MOTHERBOARD":
			return model.CategoryMB
		case "GPU":
			return model.CategoryGPU
		case "RAM":
			return model.CategoryRAM
		case "SSD":
			return model.CategorySSD
		case "PSU":
			return model.CategoryPSU
		case "CASE":
			return model.CategoryCase
		case "COOLER":
			return model.CategoryCooler
		}
	}
	title := strings.ToLower(product.Title)
	switch {
	case strings.Contains(title, "rtx") || strings.Contains(title, "rx ") || strings.Contains(title, "显卡"):
		return model.CategoryGPU
	case strings.Contains(title, "ryzen") || strings.Contains(title, "intel") || strings.Contains(title, "cpu"):
		return model.CategoryCPU
	case strings.Contains(title, "b650") || strings.Contains(title, "b760") || strings.Contains(title, "主板"):
		return model.CategoryMB
	case strings.Contains(title, "ddr") || strings.Contains(title, "内存"):
		return model.CategoryRAM
	case strings.Contains(title, "nvme") || strings.Contains(title, "ssd"):
		return model.CategorySSD
	case strings.Contains(title, "电源") || strings.Contains(title, "psu"):
		return model.CategoryPSU
	case strings.Contains(title, "机箱") || strings.Contains(title, "case"):
		return model.CategoryCase
	case strings.Contains(title, "散热") || strings.Contains(title, "cooler"):
		return model.CategoryCooler
	default:
		return ""
	}
}

func detectBrand(title string) string {
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(lower, "intel"):
		return "Intel"
	case strings.Contains(lower, "amd") || strings.Contains(lower, "ryzen"):
		return "AMD"
	case strings.Contains(lower, "nvidia") || strings.Contains(lower, "rtx"):
		return "NVIDIA"
	case strings.Contains(lower, "asrock"):
		return "ASRock"
	case strings.Contains(lower, "msi"):
		return "MSI"
	case strings.Contains(lower, "asus"):
		return "ASUS"
	case strings.Contains(lower, "gigabyte"):
		return "Gigabyte"
	case strings.Contains(lower, "deepcool"):
		return "DeepCool"
	case strings.Contains(lower, "thermalright"):
		return "Thermalright"
	case strings.Contains(lower, "lian li"):
		return "Lian Li"
	case strings.Contains(lower, "montech"):
		return "Montech"
	case strings.Contains(lower, "samsung"):
		return "Samsung"
	case strings.Contains(lower, "western digital") || strings.Contains(lower, "wd "):
		return "WD"
	case strings.Contains(lower, "金士顿"):
		return "Kingston"
	case strings.Contains(lower, "海盗船"):
		return "Corsair"
	default:
		return "Generic"
	}
}

func inferModel(title, brand string) string {
	clean := strings.TrimSpace(title)
	if clean == "" {
		return brand + " model"
	}
	words := strings.Fields(clean)
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, " ")
}

func inferCanonicalModel(category model.PartCategory, title, brand string, useCase model.UseCase) string {
	switch category {
	case model.CategoryCPU:
		return inferCanonicalCPUModel(title, brand)
	case model.CategoryGPU:
		return inferCanonicalGPUModel(title, brand)
	case model.CategoryRAM:
		memoryType := inferMemoryType(category, title, useCase)
		speed := ""
		if match := speedRegexp.FindStringSubmatch(title); len(match) == 2 {
			speed = match[1]
		}
		capacity := ""
		if match := capacityRegexp.FindStringSubmatch(strings.ToLower(title)); len(match) == 2 {
			capacity = match[1]
		}
		if memoryType != "" && speed != "" && capacity != "" {
			return fmt.Sprintf("%s %s %sG", memoryType, speed, capacity)
		}
	case model.CategorySSD:
		return inferCanonicalSSDModel(title)
	}
	return inferModel(title, brand)
}

func inferCanonicalCPUModel(title, brand string) string {
	lower := strings.ToLower(title)
	if match := intelCPURegexp.FindStringSubmatch(lower); len(match) == 2 {
		modelName := strings.ToUpper(strings.ReplaceAll(match[1], " ", ""))
		modelName = strings.ReplaceAll(modelName, "I", "i")
		return "Core " + strings.ReplaceAll(modelName, "-", "-")
	}
	if match := amdCPURegexp.FindStringSubmatch(lower); len(match) == 2 {
		modelName := strings.ToUpper(strings.TrimSpace(match[1]))
		return "Ryzen " + modelName
	}
	return inferModel(title, brand)
}

func inferCanonicalGPUModel(title, brand string) string {
	lower := strings.ToLower(title)
	if match := rtxGPURegexp.FindStringSubmatch(lower); len(match) == 2 {
		return normalizeRTXModel(match[1])
	}
	if match := rxGPURegexp.FindStringSubmatch(lower); len(match) == 2 {
		return strings.ToUpper(strings.Join(strings.Fields(match[1]), " "))
	}
	return inferModel(title, brand)
}

func normalizeRTXModel(value string) string {
	upper := strings.ToUpper(strings.Join(strings.Fields(value), " "))
	upper = strings.ReplaceAll(upper, "RTX", "RTX ")
	upper = strings.ReplaceAll(upper, "  ", " ")
	upper = strings.ReplaceAll(upper, "TI", "Ti")
	upper = strings.ReplaceAll(upper, "SUPER", "SUPER")
	return strings.TrimSpace(upper)
}

func inferCanonicalSSDModel(title string) string {
	lower := strings.ToLower(title)
	modelName := ""
	for _, regexpItem := range []*regexp.Regexp{snSSDRegexp, evoSSDRegexp, nmSSDRegexp, mpSSDRegexp, t500SSDRegexp, p3PlusSSDRegexp} {
		if match := regexpItem.FindStringSubmatch(lower); len(match) == 2 {
			modelName = strings.ToUpper(strings.Join(strings.Fields(match[1]), " "))
			break
		}
	}
	capacity := ""
	if match := storageCapacityRegexp.FindStringSubmatch(lower); len(match) == 3 {
		capacity = strings.ToUpper(match[1] + match[2])
	}
	if strings.Contains(lower, "nvme") {
		if modelName != "" && capacity != "" {
			return fmt.Sprintf("%s %s NVMe", modelName, capacity)
		}
		if capacity != "" {
			return fmt.Sprintf("%s NVMe SSD", capacity)
		}
	}
	if modelName != "" && capacity != "" {
		return fmt.Sprintf("%s %s", modelName, capacity)
	}
	if capacity != "" {
		return fmt.Sprintf("%s SSD", capacity)
	}
	if modelName != "" {
		return modelName
	}
	return inferModel(title, "SSD")
}

func inferMemoryType(category model.PartCategory, title string, useCase model.UseCase) string {
	lower := strings.ToLower(title)
	if strings.Contains(lower, "ddr4") {
		return "DDR4"
	}
	if strings.Contains(lower, "ddr5") {
		return "DDR5"
	}
	if category == model.CategoryRAM || category == model.CategoryMB {
		if useCase == model.UseCaseOffice {
			return "DDR4"
		}
		return "DDR5"
	}
	return ""
}

func displayName(category model.PartCategory, brand, modelName string) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", category, brand, modelName))
}

func slugify(value string) string {
	replacer := strings.NewReplacer("/", "-", " ", "-", "_", "-", ".", "-", "(", "", ")", "", "+", "plus")
	value = strings.ToLower(strings.TrimSpace(replacer.Replace(value)))
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return strings.Trim(value, "-")
}

func dedupeStrings(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
