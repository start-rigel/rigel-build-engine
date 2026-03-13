package build

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
)

type Repository interface {
	ListProducts(ctx context.Context, platforms []model.SourcePlatform, limit int) ([]model.Product, error)
	SearchParts(ctx context.Context, keyword string, limit int) ([]model.PartSearchResult, error)
	EnsurePart(ctx context.Context, part model.Part) (model.Part, error)
	UpsertProductMapping(ctx context.Context, mapping model.ProductPartMapping) error
	CreateBuildRequest(ctx context.Context, req model.BuildRequest) (model.BuildRequest, error)
	UpdateBuildRequestStatus(ctx context.Context, requestID model.ID, status model.BuildStatus) error
	CreateBuildResult(ctx context.Context, result model.BuildResult) (model.BuildResult, error)
	CreateBuildResultItems(ctx context.Context, resultID model.ID, items []model.BuildResultItem) error
	GetBuildAggregate(ctx context.Context, requestID model.ID) (Response, error)
}

type Service struct {
	repo  Repository
	clock func() time.Time
}

type normalizedCandidate struct {
	Product         model.Product
	Synthetic       bool
	Category        model.PartCategory
	Brand           string
	Model           string
	DisplayName     string
	NormalizedKey   string
	PlatformFamily  string
	MemoryType      string
	PSUWattage      int
	EstimatedPower  int
	SourcePlatform  model.SourcePlatform
	Availability    string
	Reasons         []string
	Risks           []string
	MatchConfidence float64
}

type selectionProfile struct {
	Name        string
	Role        model.ResultRole
	BudgetShare map[model.PartCategory]float64
	Modifiers   map[model.PartCategory]float64
}

var (
	wattRegexp = regexp.MustCompile(`(?i)(\d{3,4})\s*w`)
)

const shopTypeSelfOperated = model.ShopType("self_operated")

func New(repo Repository, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{repo: repo, clock: clock}
}

func (s *Service) Generate(ctx context.Context, req GenerateRequest) (Response, error) {
	if req.Budget <= 0 {
		return Response{}, fmt.Errorf("budget must be greater than 0")
	}
	if req.UseCase == "" {
		return Response{}, fmt.Errorf("use_case is required")
	}
	if req.BuildMode == "" {
		req.BuildMode = model.ModeNewOnly
	}

	buildRequest, err := s.repo.CreateBuildRequest(ctx, model.BuildRequest{
		RequestNo:   newRequestNo(),
		Budget:      req.Budget,
		UseCase:     req.UseCase,
		BuildMode:   req.BuildMode,
		Constraints: map[string]any{"generator": "rigel-build-engine"},
		Status:      model.BuildPending,
	})
	if err != nil {
		return Response{}, err
	}

	response, err := s.generateAndPersist(ctx, buildRequest)
	if err != nil {
		_ = s.repo.UpdateBuildRequestStatus(ctx, buildRequest.ID, model.BuildFailed)
		return Response{}, err
	}
	if err := s.repo.UpdateBuildRequestStatus(ctx, buildRequest.ID, model.BuildGenerated); err != nil {
		return Response{}, err
	}
	response.Status = model.BuildGenerated
	return response, nil
}

func (s *Service) GetBuild(ctx context.Context, requestID string) (Response, error) {
	if requestID == "" {
		return Response{}, fmt.Errorf("build request id is required")
	}
	return s.repo.GetBuildAggregate(ctx, model.ID(requestID))
}

func (s *Service) SearchParts(ctx context.Context, keyword string, limit int) ([]model.PartSearchResult, error) {
	return s.repo.SearchParts(ctx, keyword, limit)
}

func (s *Service) generateAndPersist(ctx context.Context, req model.BuildRequest) (Response, error) {
	platforms, warnings := allowedPlatforms(req.BuildMode)
	products, err := s.repo.ListProducts(ctx, platforms, 300)
	if err != nil {
		return Response{}, err
	}

	grouped, sourceStats := s.buildCandidatePool(products, req.UseCase, req.BuildMode)
	profiles := []selectionProfile{primaryProfile(req.UseCase), alternativeProfile(req.UseCase)}
	response := Response{
		BuildRequestID: string(req.ID),
		RequestNo:      req.RequestNo,
		Budget:         req.Budget,
		UseCase:        req.UseCase,
		BuildMode:      req.BuildMode,
		Status:         model.BuildPending,
		Warnings:       warnings,
	}

	for _, profile := range profiles {
		selected, findings, localWarnings := selectBuild(grouped, req.Budget, req.UseCase, profile)
		response.Warnings = append(response.Warnings, localWarnings...)

		result := model.BuildResult{
			BuildRequestID: req.ID,
			ResultRole:     profile.Role,
			TotalPrice:     totalPrice(selected),
			Score:          totalScore(selected, req.Budget, findings),
			Currency:       "CNY",
			Summary: map[string]any{
				"profile":              profile.Name,
				"source_stats":         sourceStats,
				"compatibility_passed": compatibilityPassed(findings),
				"compatibility":        findings,
				"warnings":             localWarnings,
			},
		}
		storedResult, err := s.repo.CreateBuildResult(ctx, result)
		if err != nil {
			return Response{}, err
		}

		items, payloadItems, err := s.persistSelections(ctx, storedResult.ID, selected)
		if err != nil {
			return Response{}, err
		}
		if err := s.repo.CreateBuildResultItems(ctx, storedResult.ID, items); err != nil {
			return Response{}, err
		}

		response.Results = append(response.Results, ResultPayload{
			ResultID:      string(storedResult.ID),
			Role:          profile.Role,
			TotalPrice:    storedResult.TotalPrice,
			Score:         storedResult.Score,
			Currency:      storedResult.Currency,
			Items:         payloadItems,
			Compatibility: findings,
		})
	}

	return response, nil
}

func (s *Service) persistSelections(ctx context.Context, resultID model.ID, selected map[model.PartCategory]normalizedCandidate) ([]model.BuildResultItem, []ItemPayload, error) {
	categories := orderedCategories()
	items := make([]model.BuildResultItem, 0, len(categories))
	payload := make([]ItemPayload, 0, len(categories))

	for index, category := range categories {
		candidate := selected[category]
		part, err := s.repo.EnsurePart(ctx, model.Part{
			Category:         candidate.Category,
			Brand:            candidate.Brand,
			Model:            candidate.Model,
			DisplayName:      candidate.DisplayName,
			NormalizedKey:    candidate.NormalizedKey,
			LifecycleStatus:  "active",
			SourceConfidence: candidate.MatchConfidence,
			AliasKeywords:    []string{candidate.Product.Title},
		})
		if err != nil {
			return nil, nil, err
		}

		if candidate.Product.ID != "" {
			if err := s.repo.UpsertProductMapping(ctx, model.ProductPartMapping{
				ProductID:            candidate.Product.ID,
				PartID:               part.ID,
				MappingStatus:        "mapped",
				MatchConfidence:      candidate.MatchConfidence,
				MatchedBy:            "build-engine-rule",
				CandidateDisplayName: candidate.DisplayName,
				Reason:               "normalized during build generation",
			}); err != nil {
				return nil, nil, err
			}
		}

		item := model.BuildResultItem{
			BuildResultID:  resultID,
			PartID:         part.ID,
			ProductID:      candidate.Product.ID,
			Category:       candidate.Category,
			DisplayName:    candidate.DisplayName,
			UnitPrice:      candidate.Product.Price,
			Quantity:       1,
			SourcePlatform: candidate.SourcePlatform,
			IsPrimary:      true,
			Reasons:        candidate.Reasons,
			Risks:          candidate.Risks,
			SortOrder:      index,
		}
		items = append(items, item)
		payload = append(payload, ItemPayload{
			Category:       candidate.Category,
			DisplayName:    candidate.DisplayName,
			UnitPrice:      candidate.Product.Price,
			SourcePlatform: candidate.SourcePlatform,
			ProductID:      string(candidate.Product.ID),
			PartID:         string(part.ID),
			Reasons:        candidate.Reasons,
			Risks:          candidate.Risks,
		})
	}

	return items, payload, nil
}

func (s *Service) buildCandidatePool(products []model.Product, useCase model.UseCase, buildMode model.BuildMode) (map[model.PartCategory][]normalizedCandidate, map[string]int) {
	grouped := make(map[model.PartCategory][]normalizedCandidate)
	sourceStats := map[string]int{"db_products": 0, "starter_candidates": 0, "preferred_real_products": 0}
	for _, product := range products {
		candidate, ok := normalizeProduct(product, useCase)
		if !ok {
			continue
		}
		grouped[candidate.Category] = append(grouped[candidate.Category], candidate)
		sourceStats["db_products"]++
	}

	for _, candidate := range starterCatalog(useCase) {
		if len(grouped[candidate.Category]) < 2 {
			grouped[candidate.Category] = append(grouped[candidate.Category], candidate)
			sourceStats["starter_candidates"]++
		}
	}

	for category := range grouped {
		grouped[category], sourceStats["preferred_real_products"] = preferRealCandidates(grouped[category], sourceStats["preferred_real_products"])
		sort.SliceStable(grouped[category], func(i, j int) bool {
			left := candidatePriority(grouped[category][i])
			right := candidatePriority(grouped[category][j])
			if left != right {
				return left > right
			}
			return grouped[category][i].Product.Price < grouped[category][j].Product.Price
		})
	}

	if buildMode == model.ModeUsedOnly {
		sourceStats["used_only_fallback"] = 1
	}
	return grouped, sourceStats
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
	modelName := inferModel(product.Title, brand)
	platformFamily := inferPlatformFamily(category, product.Title, useCase)
	memoryType := inferMemoryType(category, product.Title, useCase)
	psuWattage := inferPSUWattage(category, product.Title, useCase)
	estimatedPower := inferEstimatedPower(category, product.Title, useCase)
	normalizedKey := slugify(string(category) + "-" + brand + "-" + modelName)
	return normalizedCandidate{
		Product:         product,
		Category:        category,
		Brand:           brand,
		Model:           modelName,
		DisplayName:     displayName(category, brand, modelName),
		NormalizedKey:   normalizedKey,
		PlatformFamily:  platformFamily,
		MemoryType:      memoryType,
		PSUWattage:      psuWattage,
		EstimatedPower:  estimatedPower,
		SourcePlatform:  product.SourcePlatform,
		Availability:    product.Availability,
		Reasons:         []string{"collected from marketplace data", "normalized by title and category rules"},
		Risks:           availabilityRisk(product.Availability),
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

func selectBuild(grouped map[model.PartCategory][]normalizedCandidate, budget float64, useCase model.UseCase, profile selectionProfile) (map[model.PartCategory]normalizedCandidate, []CompatibilityFinding, []string) {
	selected := make(map[model.PartCategory]normalizedCandidate)
	warnings := []string{}
	for _, category := range orderedCategories() {
		target := budget * profile.BudgetShare[category]
		candidate, ok := pickCandidate(grouped[category], target, profile.Modifiers[category])
		if !ok {
			warnings = append(warnings, fmt.Sprintf("missing candidate for category %s", category))
			candidate = starterFallbackCandidate(category, useCase)
		}
		selected[category] = candidate
	}

	alignCompatibility(selected, grouped, useCase)
	if downgraded, fitted := rebalanceBudget(selected, grouped, budget); downgraded > 0 {
		if fitted {
			warnings = append(warnings, fmt.Sprintf("rebalanced %d expensive categories to fit target budget", downgraded))
		} else {
			warnings = append(warnings, fmt.Sprintf("rebalanced %d expensive categories but build still exceeds target budget", downgraded))
		}
	} else if totalPrice(selected) > budget {
		warnings = append(warnings, "current candidate pool still exceeds target budget")
	}
	alignCompatibility(selected, grouped, useCase)

	findings := evaluateCompatibility(selected)
	return selected, findings, dedupeStrings(warnings)
}

func pickCandidate(candidates []normalizedCandidate, target float64, modifier float64) (normalizedCandidate, bool) {
	if len(candidates) == 0 {
		return normalizedCandidate{}, false
	}
	bestIndex := 0
	bestScore := math.Inf(-1)
	for index, candidate := range candidates {
		score := closenessScore(candidate.Product.Price, target) + modifierScore(candidate, modifier)
		if score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	return candidates[bestIndex], true
}

func alignCompatibility(selected map[model.PartCategory]normalizedCandidate, grouped map[model.PartCategory][]normalizedCandidate, useCase model.UseCase) {
	cpu := selected[model.CategoryCPU]
	mb := selected[model.CategoryMB]
	if cpu.PlatformFamily != "" && mb.PlatformFamily != cpu.PlatformFamily {
		if candidate, ok := findMatching(grouped[model.CategoryMB], func(item normalizedCandidate) bool {
			return item.PlatformFamily == cpu.PlatformFamily
		}); ok {
			candidate.Reasons = append(candidate.Reasons, "matched CPU platform family")
			selected[model.CategoryMB] = candidate
			mb = candidate
		}
	}

	ram := selected[model.CategoryRAM]
	if ram.MemoryType != "" && mb.MemoryType != "" && ram.MemoryType != mb.MemoryType {
		if candidate, ok := findMatching(grouped[model.CategoryRAM], func(item normalizedCandidate) bool {
			return item.MemoryType == selected[model.CategoryMB].MemoryType
		}); ok {
			candidate.Reasons = append(candidate.Reasons, "matched motherboard memory type")
			selected[model.CategoryRAM] = candidate
		}
	}

	psu := selected[model.CategoryPSU]
	requiredPower := selected[model.CategoryCPU].EstimatedPower + selected[model.CategoryGPU].EstimatedPower + 220
	if psu.PSUWattage < requiredPower {
		if candidate, ok := findMatching(grouped[model.CategoryPSU], func(item normalizedCandidate) bool {
			return item.PSUWattage >= requiredPower
		}); ok {
			candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("upgraded PSU to cover estimated %dW load", requiredPower))
			selected[model.CategoryPSU] = candidate
		}
	}

	_ = useCase
}

func rebalanceBudget(selected map[model.PartCategory]normalizedCandidate, grouped map[model.PartCategory][]normalizedCandidate, budget float64) (int, bool) {
	downgraded := 0
	for totalPrice(selected) > budget {
		changed := false
		for _, category := range []model.PartCategory{model.CategoryGPU, model.CategoryCPU, model.CategoryMB, model.CategorySSD, model.CategoryRAM, model.CategoryPSU, model.CategoryCase, model.CategoryCooler} {
			current := selected[category]
			cheaper, ok := nextCheaper(grouped[category], current)
			if !ok {
				continue
			}
			selected[category] = cheaper
			downgraded++
			changed = true
			if totalPrice(selected) <= budget {
				break
			}
		}
		if !changed {
			break
		}
	}
	return downgraded, totalPrice(selected) <= budget
}

func evaluateCompatibility(selected map[model.PartCategory]normalizedCandidate) []CompatibilityFinding {
	findings := []CompatibilityFinding{}
	cpu := selected[model.CategoryCPU]
	mb := selected[model.CategoryMB]
	findings = append(findings, CompatibilityFinding{
		Rule:     "cpu_mb_platform",
		Severity: model.RiskBlocked,
		Message:  fmt.Sprintf("CPU platform %s should match motherboard platform %s", cpu.PlatformFamily, mb.PlatformFamily),
		Passed:   cpu.PlatformFamily == "" || mb.PlatformFamily == "" || cpu.PlatformFamily == mb.PlatformFamily,
	})
	findings = append(findings, CompatibilityFinding{
		Rule:     "mb_ram_memory_type",
		Severity: model.RiskWarn,
		Message:  fmt.Sprintf("RAM %s should match motherboard memory type %s", selected[model.CategoryRAM].MemoryType, mb.MemoryType),
		Passed:   selected[model.CategoryRAM].MemoryType == "" || mb.MemoryType == "" || selected[model.CategoryRAM].MemoryType == mb.MemoryType,
	})
	requiredPower := selected[model.CategoryCPU].EstimatedPower + selected[model.CategoryGPU].EstimatedPower + 220
	findings = append(findings, CompatibilityFinding{
		Rule:     "gpu_psu_wattage",
		Severity: model.RiskWarn,
		Message:  fmt.Sprintf("PSU %dW should exceed estimated system load %dW", selected[model.CategoryPSU].PSUWattage, requiredPower),
		Passed:   selected[model.CategoryPSU].PSUWattage >= requiredPower,
	})
	return findings
}

func totalPrice(selected map[model.PartCategory]normalizedCandidate) float64 {
	total := 0.0
	for _, category := range orderedCategories() {
		total += selected[category].Product.Price
	}
	return math.Round(total*100) / 100
}

func totalScore(selected map[model.PartCategory]normalizedCandidate, budget float64, findings []CompatibilityFinding) float64 {
	score := 100.0 - math.Abs(totalPrice(selected)-budget)/budget*20
	for _, category := range orderedCategories() {
		if selected[category].Synthetic {
			score -= 1.5
		}
		if selected[category].Availability == "limited" {
			score -= 0.8
		}
	}
	for _, finding := range findings {
		if finding.Passed {
			continue
		}
		switch finding.Severity {
		case model.RiskBlocked:
			score -= 20
		case model.RiskHigh:
			score -= 10
		default:
			score -= 4
		}
	}
	if score < 1 {
		score = 1
	}
	return math.Round(score*100) / 100
}

func compatibilityPassed(findings []CompatibilityFinding) bool {
	for _, finding := range findings {
		if !finding.Passed && finding.Severity == model.RiskBlocked {
			return false
		}
	}
	return true
}

func allowedPlatforms(mode model.BuildMode) ([]model.SourcePlatform, []string) {
	switch mode {
	case model.ModeUsedOnly:
		return []model.SourcePlatform{model.PlatformGoofish}, []string{"used_only currently falls back to starter catalog when no used product records are available"}
	case model.ModeMixed:
		return []model.SourcePlatform{model.PlatformJD, model.PlatformGoofish}, []string{"mixed mode currently prefers JD products and uses used-market data only when present"}
	default:
		return []model.SourcePlatform{model.PlatformJD}, nil
	}
}

func primaryProfile(useCase model.UseCase) selectionProfile {
	shares := defaultBudgetShares(useCase)
	return selectionProfile{Name: "primary", Role: model.ResultPrimary, BudgetShare: shares, Modifiers: map[model.PartCategory]float64{model.CategoryGPU: 6, model.CategoryCPU: 4}}
}

func alternativeProfile(useCase model.UseCase) selectionProfile {
	shares := defaultBudgetShares(useCase)
	shares[model.CategoryGPU] -= 0.04
	shares[model.CategorySSD] += 0.02
	shares[model.CategoryPSU] += 0.01
	shares[model.CategoryCooler] += 0.01
	return selectionProfile{Name: "alternative", Role: model.ResultAlternative, BudgetShare: shares, Modifiers: map[model.PartCategory]float64{model.CategorySSD: 3, model.CategoryPSU: 2}}
}

func defaultBudgetShares(useCase model.UseCase) map[model.PartCategory]float64 {
	switch useCase {
	case model.UseCaseOffice:
		return map[model.PartCategory]float64{model.CategoryCPU: 0.22, model.CategoryMB: 0.14, model.CategoryGPU: 0.18, model.CategoryRAM: 0.11, model.CategorySSD: 0.11, model.CategoryPSU: 0.08, model.CategoryCase: 0.08, model.CategoryCooler: 0.08}
	case model.UseCaseDesign:
		return map[model.PartCategory]float64{model.CategoryCPU: 0.24, model.CategoryMB: 0.14, model.CategoryGPU: 0.27, model.CategoryRAM: 0.12, model.CategorySSD: 0.10, model.CategoryPSU: 0.06, model.CategoryCase: 0.04, model.CategoryCooler: 0.03}
	default:
		return map[model.PartCategory]float64{model.CategoryCPU: 0.21, model.CategoryMB: 0.13, model.CategoryGPU: 0.31, model.CategoryRAM: 0.10, model.CategorySSD: 0.08, model.CategoryPSU: 0.07, model.CategoryCase: 0.05, model.CategoryCooler: 0.05}
	}
}

func orderedCategories() []model.PartCategory {
	return []model.PartCategory{model.CategoryCPU, model.CategoryMB, model.CategoryGPU, model.CategoryRAM, model.CategorySSD, model.CategoryPSU, model.CategoryCase, model.CategoryCooler}
}

func findMatching(candidates []normalizedCandidate, predicate func(normalizedCandidate) bool) (normalizedCandidate, bool) {
	for _, candidate := range candidates {
		if predicate(candidate) {
			return candidate, true
		}
	}
	return normalizedCandidate{}, false
}

func nextCheaper(candidates []normalizedCandidate, current normalizedCandidate) (normalizedCandidate, bool) {
	best := normalizedCandidate{}
	found := false
	for _, candidate := range candidates {
		if candidate.DisplayName == current.DisplayName {
			continue
		}
		if candidate.Product.Price >= current.Product.Price {
			continue
		}
		if !found || candidate.Product.Price > best.Product.Price {
			best = candidate
			found = true
		}
	}
	return best, found
}

func closenessScore(price, target float64) float64 {
	if target <= 0 {
		return 0
	}
	delta := math.Abs(price-target) / target
	return 30 - delta*20
}

func modifierScore(candidate normalizedCandidate, modifier float64) float64 {
	score := modifier
	if candidate.Availability == "in_stock" {
		score += 3
	}
	if !candidate.Synthetic {
		score += 2
	}
	if candidate.SourcePlatform == model.PlatformJD && !isMockProduct(candidate.Product) {
		score += 3
	}
	if candidate.Product.ShopType == shopTypeSelfOperated {
		score += 4
	}
	if isMockProduct(candidate.Product) {
		score -= 4
	}
	return score
}

func preferRealCandidates(candidates []normalizedCandidate, preferredCount int) ([]normalizedCandidate, int) {
	if len(candidates) == 0 {
		return candidates, preferredCount
	}

	filtered := make([]normalizedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Synthetic && candidate.SourcePlatform == model.PlatformJD && candidate.Product.ShopType == shopTypeSelfOperated && !isMockProduct(candidate.Product) {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) > 0 {
		return filtered, preferredCount + len(filtered)
	}

	for _, candidate := range candidates {
		if !candidate.Synthetic && candidate.SourcePlatform == model.PlatformJD && !isMockProduct(candidate.Product) {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) > 0 {
		return filtered, preferredCount + len(filtered)
	}

	return candidates, preferredCount
}

func candidatePriority(candidate normalizedCandidate) int {
	priority := 0
	if candidate.SourcePlatform == model.PlatformJD && !isMockProduct(candidate.Product) {
		priority += 2
	}
	if candidate.Product.ShopType == shopTypeSelfOperated {
		priority += 2
	}
	if candidate.Synthetic {
		priority -= 1
	}
	if isMockProduct(candidate.Product) {
		priority -= 2
	}
	return priority
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

func availabilityRisk(status string) []string {
	if status == "limited" || status == "unknown" {
		return []string{"availability should be rechecked before purchase"}
	}
	return nil
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

func inferPlatformFamily(category model.PartCategory, title string, useCase model.UseCase) string {
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(lower, "lga1700"), strings.Contains(lower, "i5"), strings.Contains(lower, "i7"), strings.Contains(lower, "i9"), strings.Contains(lower, "b760"), strings.Contains(lower, "b660"), strings.Contains(lower, "z790"), strings.Contains(lower, "z690"), strings.Contains(lower, "intel"):
		return "intel-lga1700"
	case strings.Contains(lower, "am5"), strings.Contains(lower, "b650"), strings.Contains(lower, "b850"), strings.Contains(lower, "x670"), strings.Contains(lower, "x870"), strings.Contains(lower, "ryzen 7 7"), strings.Contains(lower, "ryzen 5 7"), strings.Contains(lower, "amd"):
		return "amd-am5"
	case strings.Contains(lower, "am4"), strings.Contains(lower, "b550"), strings.Contains(lower, "b450"), strings.Contains(lower, "x570"), strings.Contains(lower, "ryzen 7 5"), strings.Contains(lower, "ryzen 5 5"):
		return "amd-am4"
	}
	if category == model.CategoryCPU || category == model.CategoryMB || category == model.CategoryCooler {
		if useCase == model.UseCaseOffice {
			return "intel-lga1700"
		}
		return "amd-am5"
	}
	return ""
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

func inferPSUWattage(category model.PartCategory, title string, useCase model.UseCase) int {
	if category != model.CategoryPSU {
		return 0
	}
	match := wattRegexp.FindStringSubmatch(strings.ToLower(title))
	if len(match) == 2 {
		if value, err := strconv.Atoi(match[1]); err == nil {
			return value
		}
	}
	if useCase == model.UseCaseOffice {
		return 550
	}
	return 650
}

func inferEstimatedPower(category model.PartCategory, title string, useCase model.UseCase) int {
	lower := strings.ToLower(title)
	switch category {
	case model.CategoryGPU:
		switch {
		case strings.Contains(lower, "4090"):
			return 450
		case strings.Contains(lower, "4080"):
			return 320
		case strings.Contains(lower, "4070"):
			return 220
		case strings.Contains(lower, "4060"):
			return 115
		case strings.Contains(lower, "7700"):
			return 245
		default:
			if useCase == model.UseCaseOffice {
				return 90
			}
			return 180
		}
	case model.CategoryCPU:
		switch {
		case strings.Contains(lower, "i7") || strings.Contains(lower, "7700"):
			return 125
		case strings.Contains(lower, "i5") || strings.Contains(lower, "7500") || strings.Contains(lower, "7600"):
			return 65
		default:
			if useCase == model.UseCaseDesign {
				return 105
			}
			return 65
		}
	default:
		return 0
	}
}

func displayName(category model.PartCategory, brand, modelName string) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", category, brand, modelName))
}

func starterCatalog(useCase model.UseCase) []normalizedCandidate {
	items := []normalizedCandidate{
		seedCandidate(model.CategoryCPU, "AMD", "Ryzen 5 7500F", 1199, "amd-am5", "", 0, 65, useCase),
		seedCandidate(model.CategoryCPU, "Intel", "Core i5-14400F", 1499, "intel-lga1700", "", 0, 65, useCase),
		seedCandidate(model.CategoryMB, "MSI", "B650M MORTAR WIFI", 899, "amd-am5", "DDR5", 0, 0, useCase),
		seedCandidate(model.CategoryMB, "ASUS", "B760M TUF GAMING", 999, "intel-lga1700", "DDR4", 0, 0, useCase),
		seedCandidate(model.CategoryGPU, "NVIDIA", "GeForce RTX 4060", 2099, "", "", 0, 115, useCase),
		seedCandidate(model.CategoryGPU, "AMD", "Radeon RX 7700 XT", 2999, "", "", 0, 245, useCase),
		seedCandidate(model.CategoryRAM, "Kingston", "FURY 32GB DDR5 6000", 529, "", "DDR5", 0, 0, useCase),
		seedCandidate(model.CategoryRAM, "Corsair", "Vengeance 16GB DDR4 3200", 299, "", "DDR4", 0, 0, useCase),
		seedCandidate(model.CategorySSD, "WD", "SN770 1TB NVMe", 429, "", "", 0, 0, useCase),
		seedCandidate(model.CategorySSD, "Samsung", "990 EVO 1TB NVMe", 559, "", "", 0, 0, useCase),
		seedCandidate(model.CategoryPSU, "Corsair", "RM650e 650W Gold", 489, "", "", 650, 0, useCase),
		seedCandidate(model.CategoryPSU, "MSI", "MAG A750GL 750W Gold", 559, "", "", 750, 0, useCase),
		seedCandidate(model.CategoryCase, "Montech", "AIR 100 ARGB", 279, "", "", 0, 0, useCase),
		seedCandidate(model.CategoryCase, "Lian Li", "Lancool 205 Mesh", 399, "", "", 0, 0, useCase),
		seedCandidate(model.CategoryCooler, "DeepCool", "AG400", 129, "amd-am5", "", 0, 0, useCase),
		seedCandidate(model.CategoryCooler, "Thermalright", "Peerless Assassin 120", 219, "intel-lga1700", "", 0, 0, useCase),
	}
	return items
}

func starterFallbackCandidate(category model.PartCategory, useCase model.UseCase) normalizedCandidate {
	for _, candidate := range starterCatalog(useCase) {
		if candidate.Category == category {
			return candidate
		}
	}
	return seedCandidate(category, "Generic", string(category)+" Starter", 199, "", "", 0, 0, useCase)
}

func seedCandidate(category model.PartCategory, brand, modelName string, price float64, platformFamily, memoryType string, psuWattage, estimatedPower int, useCase model.UseCase) normalizedCandidate {
	title := strings.TrimSpace(brand + " " + modelName)
	product := model.Product{
		SourcePlatform: model.PlatformJD,
		ExternalID:     slugify(string(category) + "-" + title),
		Title:          title,
		Price:          price,
		Currency:       "CNY",
		Availability:   "in_stock",
		Attributes: map[string]any{
			"category": string(category),
			"seed":     true,
		},
		RawPayload: map[string]any{"seed": true, "use_case": useCase},
	}
	return normalizedCandidate{
		Product:         product,
		Synthetic:       true,
		Category:        category,
		Brand:           brand,
		Model:           modelName,
		DisplayName:     displayName(category, brand, modelName),
		NormalizedKey:   slugify(string(category) + "-" + brand + "-" + modelName),
		PlatformFamily:  platformFamily,
		MemoryType:      memoryType,
		PSUWattage:      psuWattage,
		EstimatedPower:  estimatedPower,
		SourcePlatform:  model.PlatformJD,
		Availability:    "in_stock",
		Reasons:         []string{"starter catalog fallback", "used when collected candidates are insufficient"},
		Risks:           []string{"verify current market price before checkout"},
		MatchConfidence: 0.65,
	}
}

func newRequestNo() string {
	return fmt.Sprintf("BR-%s", time.Now().UTC().Format("20060102-150405.000"))
}

func slugify(value string) string {
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-", "(", "", ")", "", "+", "-plus-")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return strings.Trim(value, "-")
}

func dedupeStrings(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
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
