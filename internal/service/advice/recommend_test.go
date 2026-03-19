package advice

import (
	"context"
	"testing"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

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
