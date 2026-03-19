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
	Summary    string   `json:"summary"`
	Warnings   []string `json:"warnings"`
	BuildItems []struct {
		Category         string  `json:"category"`
		TargetModel      string  `json:"target_model"`
		SelectionReason  string  `json:"selection_reason"`
		Confidence       float64 `json:"confidence"`
		Reason           string  `json:"reason"`
		SuggestedKeyword string  `json:"suggested_keyword"`
	} `json:"build_items"`
	Advice BuildAdviceDetail `json:"advice"`
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
			warnings = append(warnings, "load system settings failed, fallback enabled")
		}
	}
	trimmedCatalog := trimCatalogByCategory(catalog, limit)

	if !canUseAI {
		fallback.Warnings = dedupe(append(fallback.Warnings, warnings...))
		fallback.Warnings = dedupe(append(fallback.Warnings, "ai runtime not configured or disabled, fallback enabled"))
		return fallback, nil
	}

	prompt := buildPrompt(req, trimmedCatalog)
	content, err := s.chatClient.ChatCompletion(ctx, runtime, prompt, s.settings.Timeout(runtime))
	if err != nil {
		fallback.Warnings = dedupe(append(fallback.Warnings, warnings...))
		fallback.Warnings = dedupe(append(fallback.Warnings, fmt.Sprintf("ai request failed (%v), fallback enabled", err)))
		return fallback, nil
	}
	var out aiOutput
	if err := decodeAIJSON(content, &out); err != nil {
		fallback.Warnings = dedupe(append(fallback.Warnings, warnings...))
		fallback.Warnings = dedupe(append(fallback.Warnings, "ai returned invalid json, fallback enabled"))
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
	for _, item := range out.BuildItems {
		category := model.PartCategory(strings.ToUpper(strings.TrimSpace(item.Category)))
		if category == "" {
			continue
		}
		candidates := categoryCandidates[category]
		buildItem := BuildItem{
			Category:         category,
			TargetModel:      strings.TrimSpace(item.TargetModel),
			SelectionReason:  firstNonBlank(strings.TrimSpace(item.SelectionReason), "AI selected from current catalog"),
			Confidence:       normalizeConfidence(item.Confidence),
			Missing:          len(candidates) == 0,
			Reason:           strings.TrimSpace(item.Reason),
			SuggestedKeyword: strings.TrimSpace(item.SuggestedKeyword),
		}
		buildItem.CandidateProducts = mapCandidates(candidates)
		recommended := pickRecommended(candidates, buildItem.TargetModel)
		if recommended != nil {
			buildItem.RecommendedProduct = recommended
			buildItem.PriceBasis = fmt.Sprintf("median %.0f / avg %.0f / samples %d", recommended.MinPrice+(recommended.MaxPrice-recommended.MinPrice)/2, recommended.Price, recommended.SampleCount)
			result.EstimatedTotal += recommended.Price
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
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return json.Unmarshal([]byte(strings.TrimSpace(content)), target)
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
