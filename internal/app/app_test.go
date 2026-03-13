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
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

type memoryRepo struct {
	request model.BuildRequest
}

func (r *memoryRepo) ListProducts(_ context.Context, _ []model.SourcePlatform, _ int) ([]model.Product, error) {
	return []model.Product{{ID: "gpu-1", SourcePlatform: model.PlatformJD, Title: "RTX 4060 官方自营", Price: 2099, Availability: "in_stock", Attributes: map[string]any{"category": "GPU"}}}, nil
}
func (r *memoryRepo) SearchParts(_ context.Context, _ string, _ int) ([]model.PartSearchResult, error) {
	return []model.PartSearchResult{{ID: "part-1", Category: model.CategoryCPU, Brand: "AMD", Model: "Ryzen 5 7500F", DisplayName: "CPU AMD Ryzen 5 7500F"}}, nil
}
func (r *memoryRepo) EnsurePart(_ context.Context, part model.Part) (model.Part, error) {
	part.ID = model.ID(part.NormalizedKey)
	return part, nil
}
func (r *memoryRepo) UpsertProductMapping(_ context.Context, _ model.ProductPartMapping) error {
	return nil
}
func (r *memoryRepo) CreateBuildRequest(_ context.Context, req model.BuildRequest) (model.BuildRequest, error) {
	req.ID = "request-1"
	r.request = req
	return req, nil
}
func (r *memoryRepo) UpdateBuildRequestStatus(_ context.Context, requestID model.ID, status model.BuildStatus) error {
	r.request.ID = requestID
	r.request.Status = status
	return nil
}
func (r *memoryRepo) CreateBuildResult(_ context.Context, result model.BuildResult) (model.BuildResult, error) {
	result.ID = model.ID(result.ResultRole + "-result")
	return result, nil
}
func (r *memoryRepo) CreateBuildResultItems(_ context.Context, _ model.ID, _ []model.BuildResultItem) error {
	return nil
}
func (r *memoryRepo) GetBuildAggregate(_ context.Context, _ model.ID) (buildservice.Response, error) {
	return buildservice.Response{BuildRequestID: "request-1"}, nil
}

func TestHealthz(t *testing.T) {
	repo := &memoryRepo{}
	builder := buildservice.New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })
	application := New(config.Config{ServiceName: "rigel-build-engine", BuildEngineMode: "local"}, builder)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestGenerateRoute(t *testing.T) {
	repo := &memoryRepo{}
	builder := buildservice.New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })
	application := New(config.Config{ServiceName: "rigel-build-engine", BuildEngineMode: "local"}, builder)
	body := []byte(`{"budget":6000,"use_case":"gaming","build_mode":"new_only"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/generate", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCatalogPricesRoute(t *testing.T) {
	repo := &memoryRepo{}
	builder := buildservice.New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })
	application := New(config.Config{ServiceName: "rigel-build-engine", BuildEngineMode: "local"}, builder)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/prices?use_case=gaming&build_mode=new_only&limit=20", nil)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
