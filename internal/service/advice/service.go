package advice

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

// Service keeps recommendation-expression logic inside build-engine.
type Service struct {
	providerName string
}

func New(providerName string) *Service {
	return &Service{providerName: providerName}
}

func (s *Service) Generate(payload GenerateRequest) (GenerateResponse, error) {
	if payload.Build.BuildRequestID == "" {
		return GenerateResponse{}, fmt.Errorf("build.build_request_id is required")
	}
	if len(payload.Build.Results) == 0 {
		return GenerateResponse{}, fmt.Errorf("build.results must not be empty")
	}
	primary, alternative := selectResults(payload.Build.Results)
	return GenerateResponse{
		BuildRequestID: payload.Build.BuildRequestID,
		ResultID:       primary.ResultID,
		Provider:       s.providerName,
		FallbackUsed:   true,
		Advisory:       templateAdvisory(payload.Build, primary, alternative),
	}, nil
}

func (s *Service) GenerateFromCatalog(payload GenerateCatalogRequest) (GenerateCatalogResponse, error) {
	if payload.Budget <= 0 {
		return GenerateCatalogResponse{}, fmt.Errorf("budget must be greater than 0")
	}
	if len(payload.Catalog.Items) == 0 {
		return GenerateCatalogResponse{}, fmt.Errorf("catalog.items must not be empty")
	}
	useCase := firstNonBlank(string(payload.UseCase), string(payload.Catalog.UseCase), string(model.UseCaseGaming))
	buildMode := firstNonBlank(string(payload.BuildMode), string(payload.Catalog.BuildMode), string(model.ModeMixed))
	payload.UseCase = model.UseCase(useCase)
	payload.BuildMode = model.BuildMode(buildMode)

	selection := selectCatalogItems(payload.Budget, payload.UseCase, payload.BuildMode, payload.Catalog)
	return GenerateCatalogResponse{
		Provider:     s.providerName,
		FallbackUsed: true,
		Selection:    selection,
		Advisory:     templateCatalogAdvisory(payload, selection),
	}, nil
}

func selectResults(results []buildservice.ResultPayload) (buildservice.ResultPayload, *buildservice.ResultPayload) {
	primary := results[0]
	var alternative *buildservice.ResultPayload
	for index := range results {
		if results[index].Role == model.ResultPrimary {
			primary = results[index]
			break
		}
	}
	for index := range results {
		if results[index].Role == model.ResultAlternative {
			candidate := results[index]
			alternative = &candidate
			break
		}
	}
	return primary, alternative
}

func templateAdvisory(payload buildservice.Response, primary buildservice.ResultPayload, alternative *buildservice.ResultPayload) Advice {
	cpu := findItem(primary.Items, model.CategoryCPU)
	gpu := findItem(primary.Items, model.CategoryGPU)
	ram := findItem(primary.Items, model.CategoryRAM)
	ssd := findItem(primary.Items, model.CategorySSD)

	reasons := dedupe(append(flattenReasons(primary.Items), compatibilityReasons(primary.Compatibility)...))
	if len(reasons) == 0 {
		reasons = []string{"build-engine 已完成基础兼容校验", "方案价格结构与用途匹配"}
	}

	risks := dedupe(append(payload.Warnings, flattenRisks(primary.Items)...))
	for _, finding := range primary.Compatibility {
		if !finding.Passed {
			risks = append(risks, finding.Message)
		}
	}
	if len(risks) == 0 {
		risks = []string{"当前没有命中阻断级兼容问题，但下单前仍应复核库存与价格"}
	}

	fit := fitFor(payload.UseCase, cpu.DisplayName, gpu.DisplayName)
	upgrade := upgradeAdvice(payload.UseCase, ram.DisplayName, ssd.DisplayName, primary.TotalPrice, payload.Budget)
	alternativeNote := alternativeSummary(primary, alternative)

	summary := fmt.Sprintf("这套%s方案以 %s 和 %s 为核心，总价约 %.0f %s，适合作为当前预算下的主推荐。", payload.UseCase, blankFallback(cpu.DisplayName, "CPU"), blankFallback(gpu.DisplayName, "GPU"), primary.TotalPrice, blankFallback(primary.Currency, "CNY"))
	return Advice{
		Summary:         summary,
		Reasons:         reasons,
		FitFor:          fit,
		Risks:           risks,
		UpgradeAdvice:   upgrade,
		AlternativeNote: alternativeNote,
	}
}

func compatibilityReasons(findings []buildservice.CompatibilityFinding) []string {
	reasons := []string{}
	for _, finding := range findings {
		if finding.Passed {
			reasons = append(reasons, finding.Message)
		}
	}
	return reasons
}

func flattenReasons(items []buildservice.ItemPayload) []string {
	var reasons []string
	for _, item := range items {
		reasons = append(reasons, item.Reasons...)
	}
	return reasons
}

func flattenRisks(items []buildservice.ItemPayload) []string {
	var risks []string
	for _, item := range items {
		risks = append(risks, item.Risks...)
	}
	return risks
}

func fitFor(useCase model.UseCase, cpu, gpu string) []string {
	switch useCase {
	case model.UseCaseOffice:
		return []string{"多任务办公与日常网页/文档处理", fmt.Sprintf("以 %s 为核心的稳定生产力主机", blankFallback(cpu, "CPU"))}
	case model.UseCaseDesign:
		return []string{"1080p/2K 设计剪辑与内容创作", fmt.Sprintf("依赖 %s 提供图形与渲染余量", blankFallback(gpu, "GPU"))}
	default:
		return []string{"1080p/2K 主流游戏场景", fmt.Sprintf("以 %s + %s 为核心的均衡游戏平台", blankFallback(cpu, "CPU"), blankFallback(gpu, "GPU"))}
	}
}

func upgradeAdvice(useCase model.UseCase, ram, ssd string, totalPrice, budget float64) []string {
	advice := []string{}
	if strings.Contains(strings.ToLower(ram), "16") {
		advice = append(advice, "如果后续会开更多后台程序或进行创作类任务，建议优先升级到 32GB 内存。")
	}
	if strings.Contains(strings.ToLower(ssd), "1tb") {
		advice = append(advice, "如果游戏或素材库较大，下一步优先升级到 2TB SSD。")
	}
	if budget-totalPrice > 500 {
		if useCase == model.UseCaseGaming {
			advice = append(advice, "预算还有余量时，优先考虑升级显卡档位，其次再提升散热和电源余量。")
		} else {
			advice = append(advice, "预算还有余量时，可以优先提升 CPU 或更大容量 SSD。")
		}
	}
	if len(advice) == 0 {
		advice = append(advice, "当前方案已经较均衡，后续升级建议优先围绕内存、SSD 和显卡三个方向。")
	}
	return advice
}

func alternativeSummary(primary buildservice.ResultPayload, alternative *buildservice.ResultPayload) string {
	if alternative == nil {
		return "当前没有单独生成备选方案，建议先按主方案评估价格和库存。"
	}
	diff := alternative.TotalPrice - primary.TotalPrice
	switch {
	case diff > 0:
		return fmt.Sprintf("备选方案总价比主方案高约 %.0f 元，通常换来更宽松的供电、散热或存储余量。", diff)
	case diff < 0:
		return fmt.Sprintf("备选方案总价比主方案低约 %.0f 元，更适合优先控制预算。", -diff)
	default:
		return "备选方案与主方案总价接近，更适合用来比较品牌偏好和供货情况。"
	}
}

func findItem(items []buildservice.ItemPayload, category model.PartCategory) buildservice.ItemPayload {
	for _, item := range items {
		if item.Category == category {
			return item
		}
	}
	return buildservice.ItemPayload{}
}

func dedupe(items []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
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

func blankFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func templateCatalogAdvisory(payload GenerateCatalogRequest, selection CatalogSelection) Advice {
	cpu := findSelection(selection.SelectedItems, model.CategoryCPU)
	gpu := findSelection(selection.SelectedItems, model.CategoryGPU)
	ram := findSelection(selection.SelectedItems, model.CategoryRAM)
	ssd := findSelection(selection.SelectedItems, model.CategorySSD)

	reasons := []string{
		fmt.Sprintf("本次按 %.0f 元预算和 %s 用途，从当前价格目录中挑选了更接近预算中心的型号。", selection.Budget, selection.UseCase),
		fmt.Sprintf("草案总价约 %.0f 元，优先参考了各型号的中位价和样本量。", selection.EstimatedTotal),
		"这份结果现在直接由 build-engine 基于价格目录生成，避免跨模块重复编排。",
	}
	if cpu.DisplayName != "" || gpu.DisplayName != "" {
		reasons = append(reasons, fmt.Sprintf("核心组合当前倾向于 %s + %s。", blankFallback(cpu.DisplayName, "CPU"), blankFallback(gpu.DisplayName, "GPU")))
	}

	risks := append([]string{}, selection.Warnings...)
	risks = append(risks, "价格目录会随平台活动和库存变化波动，建议下单前重新抓取一次最新价格。")
	risks = append(risks, "这份结果更适合做采购起点，不应替代 build-engine 的最终兼容结论。")

	fit := fitFor(selection.UseCase, cpu.DisplayName, gpu.DisplayName)
	upgrade := catalogUpgradeAdvice(selection, ram.DisplayName, ssd.DisplayName)

	summary := fmt.Sprintf("基于当前价格目录，这份 %s 采购草案总价约 %.0f 元，核心组合为 %s 和 %s。", selection.UseCase, selection.EstimatedTotal, blankFallback(cpu.DisplayName, "CPU"), blankFallback(gpu.DisplayName, "GPU"))
	return Advice{
		Summary:         summary,
		Reasons:         dedupe(reasons),
		FitFor:          fit,
		Risks:           dedupe(risks),
		UpgradeAdvice:   dedupe(upgrade),
		AlternativeNote: "如果你更看重品牌、静音或不同采购偏好，可以在同一份价格目录上再生成一版草案。",
	}
}

func catalogUpgradeAdvice(selection CatalogSelection, ram, ssd string) []string {
	advice := []string{}
	if strings.Contains(strings.ToLower(ram), "16g") || strings.Contains(strings.ToLower(ram), "16gb") {
		advice = append(advice, "如果后续会同时开更多后台程序，优先把内存升级到 32GB。")
	}
	if strings.Contains(strings.ToLower(ssd), "1tb") {
		advice = append(advice, "如果游戏库会持续变大，优先把 SSD 升到 2TB。")
	}
	if selection.Budget-selection.EstimatedTotal > 500 {
		advice = append(advice, "预算仍有余量时，可以先把显卡或 CPU 提升一个档位，再复核整机兼容性。")
	}
	if len(advice) == 0 {
		advice = append(advice, "当前草案已经偏均衡，后续升级可优先考虑显卡、SSD 和内存。")
	}
	return advice
}

func findSelection(items []CatalogRecommendationItem, category model.PartCategory) CatalogRecommendationItem {
	for _, item := range items {
		if item.Category == category {
			return item
		}
	}
	return CatalogRecommendationItem{}
}

func selectCatalogItems(budget float64, useCase model.UseCase, buildMode model.BuildMode, catalog buildservice.PriceCatalogResponse) CatalogSelection {
	grouped := map[model.PartCategory][]catalogCandidate{}
	for _, item := range catalog.Items {
		if strings.TrimSpace(string(item.Category)) == "" {
			continue
		}
		grouped[item.Category] = append(grouped[item.Category], toCatalogCandidate(item))
	}
	for category := range grouped {
		sort.Slice(grouped[category], func(i, j int) bool {
			return grouped[category][i].Price < grouped[category][j].Price
		})
	}

	categories := categoriesForUseCase(useCase)
	shares := budgetShares(useCase)
	indices := map[model.PartCategory]int{}
	selected := map[model.PartCategory]catalogCandidate{}

	for _, category := range categories {
		candidates := grouped[category]
		if len(candidates) == 0 {
			continue
		}
		index := chooseCandidateIndex(candidates, budget*shares[category])
		indices[category] = index
		selected[category] = candidates[index]
	}

	estimatedTotal := sumCandidates(selected)
	for _, category := range downgradeOrder(useCase) {
		for estimatedTotal > budget {
			candidates := grouped[category]
			index, ok := indices[category]
			if !ok || len(candidates) == 0 || index == 0 {
				break
			}
			index--
			indices[category] = index
			selected[category] = candidates[index]
			estimatedTotal = sumCandidates(selected)
		}
	}

	items := make([]CatalogRecommendationItem, 0, len(selected))
	for _, category := range categories {
		candidate, ok := selected[category]
		if !ok {
			continue
		}
		items = append(items, CatalogRecommendationItem{
			Category:        candidate.Category,
			DisplayName:     candidate.DisplayName,
			NormalizedKey:   candidate.NormalizedKey,
			SampleCount:     candidate.SampleCount,
			SelectedPrice:   candidate.Price,
			MedianPrice:     candidate.MedianPrice,
			SourcePlatforms: append([]string{}, candidate.Platforms...),
			Reasons: []string{
				fmt.Sprintf("当前类别按 %.0f 元目标预算挑选了更接近中位价的型号。", budget*shares[category]),
				fmt.Sprintf("已参考 %d 个价格样本。", candidate.SampleCount),
			},
		})
	}

	warnings := append([]string{}, catalog.Warnings...)
	missing := missingCategories(categories, selected)
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for _, item := range missing {
			names = append(names, string(item))
		}
		warnings = append(warnings, fmt.Sprintf("当前价格目录缺少这些类别的数据：%s。", strings.Join(names, "、")))
	}
	if estimatedTotal > budget {
		warnings = append(warnings, fmt.Sprintf("按当前价格目录选出的草案仍超预算约 %.0f 元，需要进一步压缩型号档位。", estimatedTotal-budget))
	}
	if buildMode == model.ModeUsedOnly {
		warnings = append(warnings, "当前草案仍只反映价格目录，不代表所有型号都已有稳定二手样本。")
	}

	return CatalogSelection{
		Budget:         budget,
		UseCase:        useCase,
		BuildMode:      buildMode,
		EstimatedTotal: estimatedTotal,
		Warnings:       dedupe(warnings),
		SelectedItems:  items,
	}
}

type catalogCandidate struct {
	Category      model.PartCategory
	DisplayName   string
	NormalizedKey string
	SampleCount   int
	Price         float64
	MedianPrice   float64
	Platforms     []string
}

func toCatalogCandidate(item buildservice.PriceCatalogItem) catalogCandidate {
	price := item.MedianPrice
	if price <= 0 {
		price = item.AvgPrice
	}
	if price <= 0 {
		price = item.MinPrice
	}
	platforms := make([]string, 0, len(item.Platforms))
	for _, platform := range item.Platforms {
		platforms = append(platforms, string(platform))
	}
	return catalogCandidate{
		Category:      item.Category,
		DisplayName:   blankFallback(item.DisplayName, item.Model),
		NormalizedKey: item.NormalizedKey,
		SampleCount:   item.SampleCount,
		Price:         price,
		MedianPrice:   item.MedianPrice,
		Platforms:     platforms,
	}
}

func chooseCandidateIndex(candidates []catalogCandidate, target float64) int {
	if len(candidates) == 0 {
		return 0
	}
	bestIndex := 0
	bestPenalty := math.MaxFloat64
	for index, candidate := range candidates {
		penalty := math.Abs(candidate.Price - target)
		if candidate.Price > target {
			penalty += target * 0.15
		}
		if candidate.SampleCount > 0 {
			penalty -= math.Min(float64(candidate.SampleCount)*0.2, 3)
		}
		if penalty < bestPenalty {
			bestPenalty = penalty
			bestIndex = index
		}
	}
	return bestIndex
}

func sumCandidates(selected map[model.PartCategory]catalogCandidate) float64 {
	total := 0.0
	for _, candidate := range selected {
		total += candidate.Price
	}
	return total
}

func categoriesForUseCase(useCase model.UseCase) []model.PartCategory {
	switch useCase {
	case model.UseCaseOffice:
		return []model.PartCategory{model.CategoryCPU, model.CategoryMB, model.CategoryRAM, model.CategorySSD, model.CategoryPSU, model.CategoryCase, model.CategoryCooler, model.CategoryGPU}
	case model.UseCaseDesign:
		return []model.PartCategory{model.CategoryCPU, model.CategoryGPU, model.CategoryMB, model.CategoryRAM, model.CategorySSD, model.CategoryPSU, model.CategoryCase, model.CategoryCooler}
	default:
		return []model.PartCategory{model.CategoryCPU, model.CategoryGPU, model.CategoryMB, model.CategoryRAM, model.CategorySSD, model.CategoryPSU, model.CategoryCase, model.CategoryCooler}
	}
}

func budgetShares(useCase model.UseCase) map[model.PartCategory]float64 {
	switch useCase {
	case model.UseCaseOffice:
		return map[model.PartCategory]float64{model.CategoryCPU: 0.24, model.CategoryMB: 0.14, model.CategoryRAM: 0.14, model.CategorySSD: 0.14, model.CategoryPSU: 0.08, model.CategoryCase: 0.08, model.CategoryCooler: 0.06, model.CategoryGPU: 0.12}
	case model.UseCaseDesign:
		return map[model.PartCategory]float64{model.CategoryCPU: 0.22, model.CategoryGPU: 0.28, model.CategoryMB: 0.10, model.CategoryRAM: 0.14, model.CategorySSD: 0.12, model.CategoryPSU: 0.06, model.CategoryCase: 0.05, model.CategoryCooler: 0.03}
	default:
		return map[model.PartCategory]float64{model.CategoryCPU: 0.18, model.CategoryGPU: 0.38, model.CategoryMB: 0.10, model.CategoryRAM: 0.10, model.CategorySSD: 0.08, model.CategoryPSU: 0.07, model.CategoryCase: 0.05, model.CategoryCooler: 0.04}
	}
}

func downgradeOrder(useCase model.UseCase) []model.PartCategory {
	switch useCase {
	case model.UseCaseOffice, model.UseCaseDesign, model.UseCaseGaming:
		return []model.PartCategory{model.CategoryGPU, model.CategoryCPU, model.CategorySSD, model.CategoryRAM, model.CategoryMB, model.CategoryPSU, model.CategoryCase, model.CategoryCooler}
	default:
		return []model.PartCategory{model.CategoryGPU, model.CategoryCPU, model.CategorySSD, model.CategoryRAM, model.CategoryMB, model.CategoryPSU, model.CategoryCase, model.CategoryCooler}
	}
}

func missingCategories(categories []model.PartCategory, selected map[model.PartCategory]catalogCandidate) []model.PartCategory {
	result := []model.PartCategory{}
	for _, category := range categories {
		if _, ok := selected[category]; !ok {
			result = append(result, category)
		}
	}
	return result
}
