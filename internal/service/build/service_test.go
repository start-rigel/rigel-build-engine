package build

import (
	"context"
	"testing"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
)

type memoryRepo struct {
	mappings  []model.ProductPartMapping
	summaries []model.PartMarketSummary
	products  []model.Product
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{}
}

func (r *memoryRepo) ListProducts(_ context.Context, _ []model.SourcePlatform, _ int) ([]model.Product, error) {
	if len(r.products) > 0 {
		return r.products, nil
	}
	return []model.Product{
		{ID: "cpu-1", SourcePlatform: model.PlatformJD, Title: "AMD Ryzen 5 7500F 盒装", Price: 899, Availability: "in_stock", Attributes: map[string]any{"category": "CPU"}},
		{ID: "gpu-1", SourcePlatform: model.PlatformJD, Title: "NVIDIA RTX 4060 8G 京东自营", Price: 2399, Availability: "in_stock", ShopType: model.ShopType("self_operated"), Attributes: map[string]any{"category": "GPU"}},
		{ID: "ram-1", SourcePlatform: model.PlatformJD, Title: "光威 32GB DDR5 6000 台式机内存条", Price: 459, Availability: "in_stock", Attributes: map[string]any{"category": "RAM"}},
		{ID: "ram-2", SourcePlatform: model.PlatformJD, Title: "金士顿 32GB DDR5 6000 台式机内存条", Price: 559, Availability: "in_stock", Attributes: map[string]any{"category": "RAM"}},
		{ID: "ssd-1", SourcePlatform: model.PlatformJD, Title: "西数 SN770 1TB NVMe 固态硬盘", Price: 399, Availability: "in_stock", Attributes: map[string]any{"category": "SSD"}},
	}, nil
}

func (r *memoryRepo) EnsurePart(_ context.Context, part model.Part) (model.Part, error) {
	part.ID = model.ID(part.NormalizedKey)
	return part, nil
}

func (r *memoryRepo) UpsertProductMapping(_ context.Context, mapping model.ProductPartMapping) error {
	r.mappings = append(r.mappings, mapping)
	return nil
}

func (r *memoryRepo) UpsertPartMarketSummary(_ context.Context, summary model.PartMarketSummary) error {
	r.summaries = append(r.summaries, summary)
	return nil
}

func TestGeneratePriceCatalog(t *testing.T) {
	repo := newMemoryRepo()
	service := New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })

	response, err := service.GeneratePriceCatalog(context.Background(), CatalogRequest{
		UseCase:   model.UseCaseGaming,
		BuildMode: model.ModeMixed,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("GeneratePriceCatalog() error = %v", err)
	}
	if len(response.Items) == 0 {
		t.Fatal("expected catalog items")
	}
	if len(repo.mappings) == 0 {
		t.Fatal("expected product mappings to be persisted")
	}
	if len(repo.summaries) == 0 {
		t.Fatal("expected part market summaries to be persisted")
	}
}

func TestGeneratePriceCatalogAggregatesRAMCanonicalModel(t *testing.T) {
	repo := newMemoryRepo()
	service := New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })

	response, err := service.GeneratePriceCatalog(context.Background(), CatalogRequest{UseCase: model.UseCaseGaming, BuildMode: model.ModeNewOnly, Limit: 20})
	if err != nil {
		t.Fatalf("GeneratePriceCatalog() error = %v", err)
	}

	found := false
	for _, item := range response.Items {
		if item.NormalizedKey == "ram-ddr5-6000-32g" {
			found = true
			if item.SampleCount != 2 {
				t.Fatalf("expected 2 RAM samples, got %d", item.SampleCount)
			}
		}
	}
	if !found {
		t.Fatal("expected canonical RAM entry")
	}
}

func TestGeneratePriceCatalogRejectsMockProducts(t *testing.T) {
	repo := newMemoryRepo()
	repo.products = []model.Product{
		{ID: "gpu-mock", SourcePlatform: model.PlatformJD, Title: "RTX 4060 官方自营", Price: 1999, Availability: "in_stock", Attributes: map[string]any{"category": "GPU"}, RawPayload: map[string]any{"mock": true}},
		{ID: "gpu-real", SourcePlatform: model.PlatformJD, Title: "NVIDIA RTX 4060 8G 京东自营", Price: 2399, Availability: "in_stock", Attributes: map[string]any{"category": "GPU"}},
	}
	service := New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })

	response, err := service.GeneratePriceCatalog(context.Background(), CatalogRequest{UseCase: model.UseCaseGaming, BuildMode: model.ModeNewOnly, Limit: 20})
	if err != nil {
		t.Fatalf("GeneratePriceCatalog() error = %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected 1 real catalog item, got %d", len(response.Items))
	}
}

func TestGeneratePriceCatalogFallsBackToMockProductsWhenNoRealSamplesExist(t *testing.T) {
	repo := newMemoryRepo()
	repo.products = []model.Product{
		{ID: "cpu-mock", SourcePlatform: model.PlatformJD, Title: "AMD Ryzen 5 7500F 盒装", Price: 899, Availability: "in_stock", Attributes: map[string]any{"category": "CPU"}, RawPayload: map[string]any{"mock": true}},
		{ID: "gpu-mock", SourcePlatform: model.PlatformJD, Title: "NVIDIA RTX 4060 8G 京东自营", Price: 2399, Availability: "in_stock", Attributes: map[string]any{"category": "GPU"}, RawPayload: map[string]any{"mock": true}},
	}
	service := New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })

	response, err := service.GeneratePriceCatalog(context.Background(), CatalogRequest{UseCase: model.UseCaseGaming, BuildMode: model.ModeNewOnly, Limit: 20})
	if err != nil {
		t.Fatalf("GeneratePriceCatalog() error = %v", err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("expected 2 mock fallback catalog items, got %d", len(response.Items))
	}
	found := false
	for _, warning := range response.Warnings {
		if warning == "no real collected products found; falling back to mock JD samples for local catalog generation" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mock fallback warning")
	}
}
