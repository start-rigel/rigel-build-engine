package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/config"
	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	adviceservice "github.com/rigel-labs/rigel-build-engine/internal/service/advice"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

type memoryRepo struct{}

func (r *memoryRepo) ListProducts(_ context.Context, _ []model.SourcePlatform, _ int) ([]model.Product, error) {
	return []model.Product{{ID: "gpu-1", SourcePlatform: model.PlatformJD, Title: "RTX 4060 官方自营", Price: 2099, Availability: "in_stock", Attributes: map[string]any{"category": "GPU"}}}, nil
}
func (r *memoryRepo) EnsurePart(_ context.Context, part model.Part) (model.Part, error) {
	part.ID = model.ID(part.NormalizedKey)
	return part, nil
}
func (r *memoryRepo) UpsertProductMapping(_ context.Context, _ model.ProductPartMapping) error {
	return nil
}
func (r *memoryRepo) UpsertPartMarketSummary(_ context.Context, _ model.PartMarketSummary) error {
	return nil
}

func TestHealthz(t *testing.T) {
	repo := &memoryRepo{}
	builder := buildservice.New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })
	application := New(config.Config{ServiceName: "rigel-build-engine", BuildEngineMode: "local"}, builder, adviceservice.New("build-engine"))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCatalogPricesRoute(t *testing.T) {
	repo := &memoryRepo{}
	builder := buildservice.New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })
	application := New(config.Config{ServiceName: "rigel-build-engine", BuildEngineMode: "local"}, builder, adviceservice.New("build-engine"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/prices?use_case=gaming&build_mode=new_only&limit=20", nil)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGenerateCatalogAdviceRoute(t *testing.T) {
	repo := &memoryRepo{}
	builder := buildservice.New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })
	application := New(config.Config{ServiceName: "rigel-build-engine", BuildEngineMode: "local"}, builder, adviceservice.New("build-engine"))
	body := []byte(`{"budget":6000,"use_case":"gaming","build_mode":"mixed","catalog":{"use_case":"gaming","build_mode":"mixed","items":[{"category":"CPU","display_name":"Ryzen 5 7500F","normalized_key":"cpu-ryzen-7500f","sample_count":5,"avg_price":920,"median_price":899},{"category":"GPU","display_name":"RTX 4060","normalized_key":"gpu-rtx-4060","sample_count":6,"avg_price":2410,"median_price":2399}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/advice/catalog", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
