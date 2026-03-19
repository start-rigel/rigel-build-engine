package advice

import (
	"context"
	"testing"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
	"github.com/rigel-labs/rigel-build-engine/internal/service/settings"
)

type settingsStub struct{}

func (settingsStub) GetEffective(context.Context) (settings.AIRuntime, settings.CatalogAILimits, error) {
	return settings.AIRuntime{
		BaseURL:      "https://api.example",
		GatewayToken: "gateway",
		APIToken:     "token",
		Model:        "openai/gpt-5.4-nano",
		Enabled:      true,
	}, settings.CatalogAILimits{MaxModelsPerCategory: 5}, nil
}
func (settingsStub) AIEnabled(runtime settings.AIRuntime) bool {
	return runtime.Enabled
}
func (settingsStub) Timeout(settings.AIRuntime) time.Duration {
	return 25 * time.Second
}

type chatClientStub struct {
	content string
}

func (c chatClientStub) ChatCompletion(context.Context, settings.AIRuntime, string, time.Duration) (string, error) {
	return c.content, nil
}

func TestTrimCatalogByCategory(t *testing.T) {
	catalog := buildservice.PriceCatalogResponse{
		Items: []buildservice.PriceCatalogItem{
			{Category: model.CategoryCPU, Model: "a"},
			{Category: model.CategoryCPU, Model: "b"},
			{Category: model.CategoryCPU, Model: "c"},
			{Category: model.CategoryGPU, Model: "x"},
		},
	}
	trimmed := trimCatalogByCategory(catalog, 2)
	if got := len(trimmed.Items); got != 3 {
		t.Fatalf("expected 3 items after trim, got %d", got)
	}
}

func TestGenerateBuildRecommendationFallback(t *testing.T) {
	svc := New("build-engine")
	resp, err := svc.GenerateBuildRecommendation(context.Background(), BuildRecommendRequest{
		Budget:    6000,
		UseCase:   model.UseCaseGaming,
		BuildMode: model.ModeMixed,
	}, buildservice.PriceCatalogResponse{
		UseCase: model.UseCaseGaming,
		Items: []buildservice.PriceCatalogItem{
			{Category: model.CategoryCPU, DisplayName: "Ryzen 5 7500F", Model: "7500F", MedianPrice: 899, AvgPrice: 920, SampleCount: 5},
			{Category: model.CategoryGPU, DisplayName: "RTX 4060", Model: "RTX 4060", MedianPrice: 2399, AvgPrice: 2410, SampleCount: 6},
		},
	})
	if err != nil {
		t.Fatalf("GenerateBuildRecommendation() error = %v", err)
	}
	if !resp.FallbackUsed {
		t.Fatal("expected fallback path when settings are not bound")
	}
	if len(resp.BuildItems) == 0 {
		t.Fatal("expected build items in fallback response")
	}
}

func TestGenerateBuildRecommendationAlwaysCoversEightCategories(t *testing.T) {
	svc := New("build-engine")
	svc.settings = settingsStub{}
	svc.chatClient = chatClientStub{content: `{
		"summary":"ok",
		"warnings":[],
		"build_items":[{"category":"CPU","target_model":"Ryzen 5 7500F","selection_reason":"test","confidence":0.8}],
		"advice":{"reasons":["r1"],"risks":["k1"],"upgrade_advice":["u1"]}
	}`}
	resp, err := svc.GenerateBuildRecommendation(context.Background(), BuildRecommendRequest{
		Budget:    6000,
		UseCase:   model.UseCaseGaming,
		BuildMode: model.ModeMixed,
	}, buildservice.PriceCatalogResponse{
		UseCase: model.UseCaseGaming,
		Items: []buildservice.PriceCatalogItem{
			{Category: model.CategoryCPU, DisplayName: "Ryzen 5 7500F", Model: "7500F", MedianPrice: 899, AvgPrice: 920, SampleCount: 5},
			{Category: model.CategoryGPU, DisplayName: "RTX 4060", Model: "RTX 4060", MedianPrice: 2399, AvgPrice: 2410, SampleCount: 6},
		},
	})
	if err != nil {
		t.Fatalf("GenerateBuildRecommendation() error = %v", err)
	}
	if resp.FallbackUsed {
		t.Fatal("expected ai path")
	}
	if got := len(resp.BuildItems); got != 8 {
		t.Fatalf("expected 8 categories, got %d", got)
	}
}
