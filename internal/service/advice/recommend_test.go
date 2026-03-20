package advice

import (
	"context"
	"encoding/json"
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

func TestDecodeAIJSONSupportsCodeFenceAndPreface(t *testing.T) {
	content := "这是推荐结果：\n```json\n{\"summary\":\"ok\",\"warnings\":[],\"build_items\":[],\"advice\":{\"reasons\":[],\"risks\":[],\"upgrade_advice\":[]}}\n```"
	var out aiOutput
	if err := decodeAIJSON(content, &out); err != nil {
		t.Fatalf("decodeAIJSON() error = %v", err)
	}
	if out.Summary != "ok" {
		t.Fatalf("expected summary ok, got %q", out.Summary)
	}
}

func TestExtractFirstJSONObjectWithNestedQuotes(t *testing.T) {
	text := `prefix {"summary":"a {b}","warnings":[],"build_items":[],"advice":{"reasons":[],"risks":[],"upgrade_advice":[]}} suffix`
	got, ok := extractFirstJSONObject(text)
	if !ok {
		t.Fatal("expected json object to be extracted")
	}
	var out aiOutput
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("unmarshal extracted json error = %v", err)
	}
	if out.Summary != "a {b}" {
		t.Fatalf("expected nested braces in string preserved, got %q", out.Summary)
	}
}

func TestDecodeAIJSONSupportsJSONStringWrapper(t *testing.T) {
	content := `"{\"summary\":\"ok\",\"warnings\":[],\"build_items\":[],\"advice\":{\"reasons\":[],\"risks\":[],\"upgrade_advice\":[]}}"`
	var out aiOutput
	if err := decodeAIJSON(content, &out); err != nil {
		t.Fatalf("decodeAIJSON() error = %v", err)
	}
	if out.Summary != "ok" {
		t.Fatalf("expected summary ok, got %q", out.Summary)
	}
}

func TestDecodeAIJSONSupportsOpenRouterVariantShape(t *testing.T) {
	content := `{
		"summary": {"budget": 6000, "use_case": "gaming", "build_mode": "mixed", "recommended_build_name": "6000元电竞主机"},
		"warnings": ["w1"],
		"build_items": [
			{
				"category":"CPU",
				"selected":{"display_name":"CPU AMD Ryzen 7600","avg_price":1114},
				"reason":"游戏更均衡",
				"suggested_keyword":"Ryzen 7600"
			}
		],
		"advice": {
			"estimated_total_price_yuan": 5980,
			"budget_fit": ["预算内"],
			"quick_adjustments": ["可升级 2TB SSD"],
			"compatibility_checks": ["确认主板 BIOS"]
		}
	}`
	var out aiOutput
	if err := decodeAIJSON(content, &out); err != nil {
		t.Fatalf("decodeAIJSON() error = %v", err)
	}
	if out.Summary == "" {
		t.Fatal("expected summary from object to be normalized")
	}
	if len(out.BuildItems) != 1 {
		t.Fatalf("expected one build item, got %d", len(out.BuildItems))
	}
	if out.BuildItems[0].TargetModel != "CPU AMD Ryzen 7600" {
		t.Fatalf("expected target model from selected.display_name, got %q", out.BuildItems[0].TargetModel)
	}
	if got := out.Advice.UpgradeAdvice; len(got) == 0 || got[0] != "可升级 2TB SSD" {
		t.Fatalf("expected quick_adjustments mapped to upgrade_advice, got %#v", got)
	}
}
