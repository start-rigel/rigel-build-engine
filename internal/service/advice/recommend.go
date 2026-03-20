package advice

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
	"github.com/rigel-labs/rigel-build-engine/internal/service/settings"
)

type SettingsProvider interface {
	GetEffective(ctx context.Context) (settings.AIRuntime, settings.CatalogAILimits, error)
	AIEnabled(runtime settings.AIRuntime) bool
	Timeout(runtime settings.AIRuntime) time.Duration
}

type ChatClient interface {
	ChatCompletion(ctx context.Context, runtime settings.AIRuntime, prompt string, timeout time.Duration) (string, error)
}

type aiOutput struct {
	Summary    string              `json:"summary"`
	Warnings   []string            `json:"warnings"`
	BuildItems []aiOutputBuildItem `json:"build_items"`
	Advice     BuildAdviceDetail   `json:"advice"`
}

func (s *Service) GenerateBuildRecommendation(ctx context.Context, req BuildRecommendRequest, catalog buildservice.PriceCatalogResponse) (BuildRecommendResponse, error) {
	if req.Budget <= 0 {
		return BuildRecommendResponse{}, fmt.Errorf("budget must be greater than 0")
	}
	if len(catalog.Items) == 0 {
		return BuildRecommendResponse{}, fmt.Errorf("catalog.items must not be empty")
	}
	if req.UseCase == "" {
		req.UseCase = catalog.UseCase
	}
	if req.UseCase == "" {
		req.UseCase = model.UseCaseGaming
	}
	if req.BuildMode == "" {
		req.BuildMode = catalog.BuildMode
	}
	if req.BuildMode == "" {
		req.BuildMode = model.ModeMixed
	}

	selection := selectCatalogItems(req.Budget, req.UseCase, req.BuildMode, catalog)
	fallback := s.buildFallback(req, selection, catalog.Warnings)
	warnings := append([]string{}, catalog.Warnings...)

	limit := 5
	var runtime settings.AIRuntime
	canUseAI := false
	if s.settings != nil {
		var limits settings.CatalogAILimits
		var err error
		runtime, limits, err = s.settings.GetEffective(ctx)
		if err == nil {
			limit = limits.MaxModelsPerCategory
			canUseAI = s.settings.AIEnabled(runtime)
		} else {
			warnings = append(warnings, "加载系统设置失败，已回退到目录模板推荐")
		}
	}
	trimmedCatalog := trimCatalogByCategory(catalog, limit)

	if !canUseAI {
		fallback.Warnings = dedupe(append(fallback.Warnings, warnings...))
		fallback.Warnings = dedupe(append(fallback.Warnings, "AI 运行时未配置或已禁用，已回退到目录模板推荐"))
		return fallback, nil
	}

	prompt := buildPrompt(req, trimmedCatalog)
	content, err := s.chatClient.ChatCompletion(ctx, runtime, prompt, s.settings.Timeout(runtime))
	if err != nil {
		fallback.Warnings = dedupe(append(fallback.Warnings, warnings...))
		fallback.Warnings = dedupe(append(fallback.Warnings, fmt.Sprintf("AI 请求失败（%v），已回退到目录模板推荐", err)))
		return fallback, nil
	}
	var out aiOutput
	if err := decodeAIJSON(content, &out); err != nil {
		fallback.Warnings = dedupe(append(fallback.Warnings, warnings...))
		fallback.Warnings = dedupe(append(fallback.Warnings, "AI 返回结果不是合法 JSON，已回退到目录模板推荐"))
		return fallback, nil
	}

	result := BuildRecommendResponse{
		Provider:       s.providerName,
		FallbackUsed:   false,
		Request:        BuildRequestEcho{Budget: req.Budget, UseCase: req.UseCase, BuildMode: req.BuildMode, Notes: req.Notes},
		Summary:        strings.TrimSpace(out.Summary),
		EstimatedTotal: 0,
		WithinBudget:   true,
		Warnings:       dedupe(append(warnings, out.Warnings...)),
		Advice:         out.Advice,
	}
	if strings.TrimSpace(result.Summary) == "" {
		result.Summary = fallback.Summary
	}

	categoryCandidates := groupedCandidates(trimmedCatalog)
	aiItems := map[model.PartCategory]aiOutputBuildItem{}
	for _, item := range out.BuildItems {
		category := model.PartCategory(strings.ToUpper(strings.TrimSpace(item.Category)))
		if category == "" {
			continue
		}
		if _, exists := aiItems[category]; exists {
			continue
		}
		aiItems[category] = item
	}
	fallbackByCategory := map[model.PartCategory]CatalogRecommendationItem{}
	for _, item := range selection.SelectedItems {
		fallbackByCategory[item.Category] = item
	}
	for _, category := range requiredBuildCategories() {
		item, hasAI := aiItems[category]
		candidates := categoryCandidates[category]
		buildItem := BuildItem{
			Category: category,
			Missing:  len(candidates) == 0,
		}
		if hasAI {
			buildItem.TargetModel = strings.TrimSpace(item.TargetModel)
			buildItem.SelectionReason = firstNonBlank(strings.TrimSpace(item.SelectionReason), "AI selected from current catalog")
			buildItem.Confidence = normalizeConfidence(item.Confidence)
			buildItem.Reason = strings.TrimSpace(item.Reason)
			buildItem.SuggestedKeyword = strings.TrimSpace(item.SuggestedKeyword)
		} else if fallbackItem, ok := fallbackByCategory[category]; ok {
			buildItem.TargetModel = strings.TrimSpace(fallbackItem.DisplayName)
			buildItem.SelectionReason = "AI 未返回该类别，已使用目录候选补齐"
			buildItem.Confidence = 0.6
			buildItem.SuggestedKeyword = strings.TrimSpace(fallbackItem.DisplayName)
		} else {
			buildItem.SelectionReason = "AI 未返回该类别，且目录中无可用候选"
			buildItem.Confidence = 0.5
			buildItem.Reason = "catalog has no candidates for this category"
			buildItem.Missing = true
		}
		buildItem.CandidateProducts = mapCandidates(candidates)
		recommended := pickRecommended(candidates, buildItem.TargetModel)
		if recommended != nil {
			buildItem.RecommendedProduct = recommended
			buildItem.PriceBasis = fmt.Sprintf("median %.0f / avg %.0f / samples %d", recommended.MinPrice+(recommended.MaxPrice-recommended.MinPrice)/2, recommended.Price, recommended.SampleCount)
			result.EstimatedTotal += recommended.Price
			buildItem.Missing = false
		}
		if buildItem.Missing {
			result.WithinBudget = false
		}
		result.BuildItems = append(result.BuildItems, buildItem)
	}

	if len(result.BuildItems) == 0 {
		return fallback, nil
	}
	if result.EstimatedTotal > req.Budget {
		result.WithinBudget = false
	}
	result.Advice.Reasons = dedupe(result.Advice.Reasons)
	result.Advice.Risks = dedupe(result.Advice.Risks)
	result.Advice.UpgradeAdvice = dedupe(result.Advice.UpgradeAdvice)
	return result, nil
}

func (s *Service) buildFallback(req BuildRecommendRequest, selection CatalogSelection, warnings []string) BuildRecommendResponse {
	advice := templateCatalogAdvisory(GenerateCatalogRequest{
		Budget:    req.Budget,
		UseCase:   req.UseCase,
		BuildMode: req.BuildMode,
	}, selection)
	items := make([]BuildItem, 0, len(selection.SelectedItems))
	for _, item := range selection.SelectedItems {
		ref := &ProductRef{
			DisplayName: item.DisplayName,
			Model:       item.DisplayName,
			Price:       item.SelectedPrice,
			MinPrice:    item.SelectedPrice,
			MaxPrice:    item.SelectedPrice,
			SampleCount: item.SampleCount,
		}
		items = append(items, BuildItem{
			Category:           item.Category,
			TargetModel:        item.DisplayName,
			SelectionReason:    strings.Join(item.Reasons, "；"),
			PriceBasis:         fmt.Sprintf("selected %.0f / median %.0f / samples %d", item.SelectedPrice, item.MedianPrice, item.SampleCount),
			Confidence:         0.75,
			RecommendedProduct: ref,
			CandidateProducts:  []ProductRef{*ref},
			Missing:            false,
		})
	}
	return BuildRecommendResponse{
		Provider:       s.providerName,
		FallbackUsed:   true,
		Request:        BuildRequestEcho{Budget: req.Budget, UseCase: req.UseCase, BuildMode: req.BuildMode, Notes: req.Notes},
		Summary:        advice.Summary,
		EstimatedTotal: selection.EstimatedTotal,
		WithinBudget:   selection.EstimatedTotal <= req.Budget,
		Warnings:       dedupe(append(selection.Warnings, warnings...)),
		BuildItems:     items,
		Advice: BuildAdviceDetail{
			Reasons:       advice.Reasons,
			Risks:         advice.Risks,
			UpgradeAdvice: advice.UpgradeAdvice,
		},
	}
}

func trimCatalogByCategory(catalog buildservice.PriceCatalogResponse, maxPerCategory int) buildservice.PriceCatalogResponse {
	if maxPerCategory <= 0 {
		maxPerCategory = 5
	}
	counter := map[model.PartCategory]int{}
	items := make([]buildservice.PriceCatalogItem, 0, len(catalog.Items))
	for _, item := range catalog.Items {
		counter[item.Category]++
		if counter[item.Category] > maxPerCategory {
			continue
		}
		items = append(items, item)
	}
	catalog.Items = items
	return catalog
}

func buildPrompt(req BuildRecommendRequest, catalog buildservice.PriceCatalogResponse) string {
	payload := map[string]any{
		"request": map[string]any{
			"budget": req.Budget, "use_case": req.UseCase, "build_mode": req.BuildMode, "notes": req.Notes,
		},
		"catalog":     catalog,
		"instruction": "Return JSON only. Required top-level fields: summary, warnings, build_items, advice. build_items must cover CPU,GPU,MB,RAM,SSD,PSU,CASE,COOLER. Do not omit missing categories; set reason and suggested_keyword for missing.",
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func decodeAIJSON(content string, target any) error {
	raw, err := parseAIResponseObject(content)
	if err != nil {
		return err
	}
	normalized := normalizeAIOutput(raw)
	data, _ := json.Marshal(normalized)
	return json.Unmarshal(data, target)
}

func sanitizeAIContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func extractFirstJSONObject(text string) (string, bool) {
	inString := false
	escaped := false
	depth := 0
	start := -1
	for i, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case !inString && r == '{':
			if depth == 0 {
				start = i
			}
			depth++
		case !inString && r == '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				return strings.TrimSpace(text[start : i+1]), true
			}
		}
	}
	return "", false
}

func parseAIResponseObject(content string) (map[string]any, error) {
	content = sanitizeAIContent(content)
	if raw, ok := tryParseJSONObject(content); ok {
		return raw, nil
	}
	var wrapped string
	if err := json.Unmarshal([]byte(content), &wrapped); err == nil {
		wrapped = sanitizeAIContent(wrapped)
		if raw, ok := tryParseJSONObject(wrapped); ok {
			return raw, nil
		}
		content = wrapped
	}
	extracted, ok := extractFirstJSONObject(content)
	if !ok {
		return nil, fmt.Errorf("no json object found")
	}
	if raw, ok := tryParseJSONObject(extracted); ok {
		return raw, nil
	}
	return nil, fmt.Errorf("no json object found")
}

func tryParseJSONObject(text string) (map[string]any, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, false
	}
	if len(raw) == 0 {
		return nil, false
	}
	return raw, true
}

func normalizeAIOutput(raw map[string]any) aiOutput {
	out := aiOutput{
		Summary:  normalizeAISummary(raw["summary"]),
		Warnings: toStringSlice(raw["warnings"]),
		Advice:   normalizeAIAdvice(raw["advice"]),
	}
	for _, item := range toAnySlice(raw["build_items"]) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		buildItem := aiOutputBuildItem{
			Category:         strings.ToUpper(strings.TrimSpace(toString(m["category"]))),
			TargetModel:      strings.TrimSpace(toString(m["target_model"])),
			SelectionReason:  strings.TrimSpace(toString(m["selection_reason"])),
			Confidence:       toFloat(m["confidence"]),
			Reason:           strings.TrimSpace(toString(m["reason"])),
			SuggestedKeyword: strings.TrimSpace(toString(m["suggested_keyword"])),
		}
		if buildItem.TargetModel == "" {
			buildItem.TargetModel = extractSelectedModel(m["selected"])
		}
		if buildItem.SelectionReason == "" {
			buildItem.SelectionReason = firstNonBlank(buildItem.Reason, "AI selected from current catalog")
		}
		if buildItem.Category == "" {
			continue
		}
		out.BuildItems = append(out.BuildItems, buildItem)
	}
	if strings.TrimSpace(out.Summary) == "" {
		out.Summary = firstNonBlank(strings.Join(out.Advice.Reasons, "；"), strings.Join(out.Warnings, "；"))
	}
	return out
}

func normalizeAISummary(value any) string {
	summary := strings.TrimSpace(toString(value))
	if summary != "" {
		return summary
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	parts := []string{}
	if name := strings.TrimSpace(toString(raw["recommended_build_name"])); name != "" {
		parts = append(parts, name)
	}
	if focus := strings.TrimSpace(toString(raw["target_focus"])); focus != "" {
		parts = append(parts, focus)
	}
	if note := strings.TrimSpace(toString(raw["note"])); note != "" {
		parts = append(parts, note)
	}
	budget := toFloat(raw["budget"])
	useCase := strings.TrimSpace(toString(raw["use_case"]))
	buildMode := strings.TrimSpace(toString(raw["build_mode"]))
	if budget > 0 || useCase != "" || buildMode != "" {
		parts = append(parts, strings.TrimSpace(fmt.Sprintf("预算 %.0f 元，用途 %s，模式 %s。", budget, firstNonBlank(useCase, "未知"), firstNonBlank(buildMode, "未知"))))
	}
	return strings.TrimSpace(strings.Join(parts, "；"))
}

func normalizeAIAdvice(value any) BuildAdviceDetail {
	raw, ok := value.(map[string]any)
	if !ok {
		return BuildAdviceDetail{}
	}
	reasons := toStringSlice(raw["reasons"])
	risks := toStringSlice(raw["risks"])
	upgrade := toStringSlice(raw["upgrade_advice"])
	if len(reasons) == 0 {
		if total := toFloat(raw["estimated_total_price_yuan"]); total > 0 {
			reasons = append(reasons, fmt.Sprintf("AI 估算总价约 %.0f 元。", total))
		}
		reasons = append(reasons, toStringSlice(raw["budget_fit"])...)
		reasons = append(reasons, toStringSlice(raw["compatibility_checks"])...)
	}
	if len(risks) == 0 {
		risks = append(risks, toStringSlice(raw["compatibility_checks"])...)
	}
	if len(upgrade) == 0 {
		upgrade = append(upgrade, toStringSlice(raw["quick_adjustments"])...)
	}
	return BuildAdviceDetail{
		Reasons:       dedupe(reasons),
		Risks:         dedupe(risks),
		UpgradeAdvice: dedupe(upgrade),
	}
}

func extractSelectedModel(value any) string {
	selected, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return firstNonBlank(
		strings.TrimSpace(toString(selected["display_name"])),
		strings.TrimSpace(toString(selected["model"])),
		strings.TrimSpace(toString(selected["normalized_key"])),
	)
}

func toAnySlice(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	return items
}

func toStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return dedupe(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(toString(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return dedupe(out)
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		return []string{s}
	default:
		return nil
	}
}

func toString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func toFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func groupedCandidates(catalog buildservice.PriceCatalogResponse) map[model.PartCategory][]buildservice.PriceCatalogItem {
	grouped := map[model.PartCategory][]buildservice.PriceCatalogItem{}
	for _, item := range catalog.Items {
		grouped[item.Category] = append(grouped[item.Category], item)
	}
	for category := range grouped {
		sort.SliceStable(grouped[category], func(i, j int) bool {
			if grouped[category][i].MedianPrice != grouped[category][j].MedianPrice {
				return grouped[category][i].MedianPrice < grouped[category][j].MedianPrice
			}
			return grouped[category][i].DisplayName < grouped[category][j].DisplayName
		})
	}
	return grouped
}

func pickRecommended(items []buildservice.PriceCatalogItem, targetModel string) *ProductRef {
	if len(items) == 0 {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(targetModel))
	for _, item := range items {
		if target != "" && strings.Contains(strings.ToLower(item.DisplayName+" "+item.Model), target) {
			ref := toProductRef(item)
			return &ref
		}
	}
	ref := toProductRef(items[0])
	return &ref
}

func mapCandidates(items []buildservice.PriceCatalogItem) []ProductRef {
	candidates := make([]ProductRef, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, toProductRef(item))
	}
	return candidates
}

func toProductRef(item buildservice.PriceCatalogItem) ProductRef {
	price := item.MedianPrice
	if price <= 0 {
		price = item.AvgPrice
	}
	return ProductRef{
		DisplayName: item.DisplayName,
		Model:       item.Model,
		Price:       price,
		MinPrice:    item.MinPrice,
		MaxPrice:    item.MaxPrice,
		SampleCount: item.SampleCount,
	}
}

func normalizeConfidence(v float64) float64 {
	if v <= 0 {
		return 0.7
	}
	if v > 1 {
		return 1
	}
	return v
}

type aiOutputBuildItem struct {
	Category         string  `json:"category"`
	TargetModel      string  `json:"target_model"`
	SelectionReason  string  `json:"selection_reason"`
	Confidence       float64 `json:"confidence"`
	Reason           string  `json:"reason"`
	SuggestedKeyword string  `json:"suggested_keyword"`
}

func requiredBuildCategories() []model.PartCategory {
	return []model.PartCategory{
		model.CategoryCPU,
		model.CategoryGPU,
		model.CategoryMB,
		model.CategoryRAM,
		model.CategorySSD,
		model.CategoryPSU,
		model.CategoryCase,
		model.CategoryCooler,
	}
}
