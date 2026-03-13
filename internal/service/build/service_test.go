package build

import (
	"context"
	"testing"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
)

type memoryRepo struct {
	request          model.BuildRequest
	results          []model.BuildResult
	items            map[model.ID][]model.BuildResultItem
	ListProductsFunc func(context.Context, []model.SourcePlatform, int) ([]model.Product, error)
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{items: map[model.ID][]model.BuildResultItem{}}
}

func (r *memoryRepo) ListProducts(ctx context.Context, platforms []model.SourcePlatform, limit int) ([]model.Product, error) {
	if r.ListProductsFunc != nil {
		return r.ListProductsFunc(ctx, platforms, limit)
	}
	return []model.Product{
		{ID: "gpu-1", SourcePlatform: model.PlatformJD, Title: "RTX 4060 官方自营", Price: 2099, Availability: "in_stock", Attributes: map[string]any{"category": "GPU"}},
		{ID: "cpu-1", SourcePlatform: model.PlatformJD, Title: "Ryzen 5 7500F 官方自营", Price: 1199, Availability: "in_stock", Attributes: map[string]any{"category": "CPU"}},
		{ID: "mb-1", SourcePlatform: model.PlatformJD, Title: "B650M 电竞主板", Price: 899, Availability: "in_stock", Attributes: map[string]any{"category": "MB"}},
	}, nil
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
	r.results = append(r.results, result)
	return result, nil
}

func (r *memoryRepo) CreateBuildResultItems(_ context.Context, resultID model.ID, items []model.BuildResultItem) error {
	r.items[resultID] = append([]model.BuildResultItem(nil), items...)
	return nil
}

func (r *memoryRepo) GetBuildAggregate(_ context.Context, _ model.ID) (Response, error) {
	return Response{}, nil
}

func TestGenerate(t *testing.T) {
	repo := newMemoryRepo()
	service := New(repo, func() time.Time {
		return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)
	})
	response, err := service.Generate(context.Background(), GenerateRequest{
		Budget:    6000,
		UseCase:   model.UseCaseGaming,
		BuildMode: model.ModeNewOnly,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(response.Results))
	}
	if len(response.Results[0].Items) != 8 {
		t.Fatalf("expected 8 items, got %d", len(response.Results[0].Items))
	}
	if repo.request.Status != model.BuildGenerated {
		t.Fatalf("expected request status generated, got %s", repo.request.Status)
	}
	if len(repo.results) != 2 {
		t.Fatalf("expected 2 persisted results, got %d", len(repo.results))
	}
	if _, ok := repo.results[0].Summary["compatibility"]; !ok {
		t.Fatal("expected persisted summary to include compatibility for later reads")
	}
}

func TestAlignCompatibilityUpdatesMainboardAndRAM(t *testing.T) {
	selected := map[model.PartCategory]normalizedCandidate{
		model.CategoryCPU: {
			Category:       model.CategoryCPU,
			DisplayName:    "CPU AMD Ryzen 5 7500F",
			PlatformFamily: "amd-am5",
			EstimatedPower: 65,
		},
		model.CategoryMB: {
			Category:       model.CategoryMB,
			DisplayName:    "MB ASUS B760M",
			PlatformFamily: "intel-lga1700",
			MemoryType:     "DDR4",
		},
		model.CategoryGPU:    {Category: model.CategoryGPU, DisplayName: "GPU RTX 4060", EstimatedPower: 115},
		model.CategoryRAM:    {Category: model.CategoryRAM, DisplayName: "RAM DDR4", MemoryType: "DDR4"},
		model.CategoryPSU:    {Category: model.CategoryPSU, DisplayName: "PSU 650W", PSUWattage: 650},
		model.CategoryCase:   {Category: model.CategoryCase, DisplayName: "CASE Generic"},
		model.CategoryCooler: {Category: model.CategoryCooler, DisplayName: "COOLER Generic"},
	}
	grouped := map[model.PartCategory][]normalizedCandidate{
		model.CategoryMB: {
			selected[model.CategoryMB],
			{
				Category:       model.CategoryMB,
				DisplayName:    "MB MSI B650M MORTAR WIFI",
				PlatformFamily: "amd-am5",
				MemoryType:     "DDR5",
			},
		},
		model.CategoryRAM: {
			selected[model.CategoryRAM],
			{
				Category:    model.CategoryRAM,
				DisplayName: "RAM Kingston DDR5",
				MemoryType:  "DDR5",
			},
		},
	}

	alignCompatibility(selected, grouped, model.UseCaseGaming)

	if selected[model.CategoryMB].PlatformFamily != "amd-am5" {
		t.Fatalf("expected mainboard to be aligned to amd-am5, got %s", selected[model.CategoryMB].PlatformFamily)
	}
	if selected[model.CategoryRAM].MemoryType != "DDR5" {
		t.Fatalf("expected RAM to be aligned to DDR5, got %s", selected[model.CategoryRAM].MemoryType)
	}
}

func TestEvaluateCompatibilityIncludesCoreRules(t *testing.T) {
	selected := map[model.PartCategory]normalizedCandidate{
		model.CategoryCPU:    {Category: model.CategoryCPU, PlatformFamily: "intel-lga1700", EstimatedPower: 125},
		model.CategoryMB:     {Category: model.CategoryMB, PlatformFamily: "amd-am5", MemoryType: "DDR5"},
		model.CategoryRAM:    {Category: model.CategoryRAM, MemoryType: "DDR4"},
		model.CategoryGPU:    {Category: model.CategoryGPU, EstimatedPower: 220},
		model.CategoryPSU:    {Category: model.CategoryPSU, PSUWattage: 550},
		model.CategoryCase:   {Category: model.CategoryCase},
		model.CategoryCooler: {Category: model.CategoryCooler},
	}

	findings := evaluateCompatibility(selected)
	index := map[string]CompatibilityFinding{}
	for _, finding := range findings {
		index[finding.Rule] = finding
	}

	if index["cpu_mb_platform"].Passed {
		t.Fatal("expected cpu_mb_platform to fail")
	}
	if index["mb_ram_memory_type"].Passed {
		t.Fatal("expected mb_ram_memory_type to fail")
	}
}

func TestBuildCandidatePoolPrefersRealJDSelfOperatedProducts(t *testing.T) {
	repo := newMemoryRepo()
	service := New(repo, func() time.Time { return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC) })

	products := []model.Product{
		{
			ID:             "gpu-mock",
			SourcePlatform: model.PlatformJD,
			Title:          "RTX 4060 官方自营",
			Price:          1999,
			Availability:   "in_stock",
			ShopType:       model.ShopType("self_operated"),
			Attributes:     map[string]any{"category": "GPU"},
			RawPayload:     map[string]any{"mock": true},
		},
		{
			ID:             "gpu-real",
			SourcePlatform: model.PlatformJD,
			Title:          "英伟达 RTX 4060 京东自营",
			Price:          3410,
			Availability:   "in_stock",
			ShopType:       model.ShopType("self_operated"),
			Attributes:     map[string]any{"category": "GPU"},
			RawPayload:     map[string]any{"risk_detected": false},
		},
	}

	grouped, stats := service.buildCandidatePool(products, model.UseCaseGaming, model.ModeNewOnly)
	gpuCandidates := grouped[model.CategoryGPU]
	if len(gpuCandidates) != 1 {
		t.Fatalf("expected preferred GPU candidate set size 1, got %d", len(gpuCandidates))
	}
	if gpuCandidates[0].Product.ID != "gpu-real" {
		t.Fatalf("expected real self-operated GPU candidate, got %s", gpuCandidates[0].Product.ID)
	}
	if stats["preferred_real_products"] != 1 {
		t.Fatalf("expected preferred_real_products 1, got %d", stats["preferred_real_products"])
	}
}

func TestNormalizeProductRejectsUnreasonableRAMPrice(t *testing.T) {
	_, ok := normalizeProduct(model.Product{
		ID:             "ram-1",
		SourcePlatform: model.PlatformJD,
		Title:          "铭瑄 32GB DDR5 6000 台式机内存条",
		Price:          2659,
		Availability:   "in_stock",
		ShopType:       model.ShopType("self_operated"),
		Attributes:     map[string]any{"category": "RAM"},
	}, model.UseCaseGaming)
	if ok {
		t.Fatal("expected unreasonable RAM price to be rejected")
	}
}

func TestNormalizeProductRejectsBundledCPUSet(t *testing.T) {
	_, ok := normalizeProduct(model.Product{
		ID:             "cpu-bundle",
		SourcePlatform: model.PlatformJD,
		Title:          "AMD 锐龙R5 7500F搭华硕PRIME B650M-K 主板CPU套装 板U套装",
		Price:          1629,
		Availability:   "in_stock",
		ShopType:       model.ShopType("self_operated"),
		Attributes:     map[string]any{"category": "CPU"},
	}, model.UseCaseGaming)
	if ok {
		t.Fatal("expected bundled CPU set to be rejected")
	}
}

func TestGeneratePriceCatalogAggregatesRealProducts(t *testing.T) {
	repo := newMemoryRepo()
	service := New(repo, func() time.Time {
		return time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)
	})

	repoProducts := []model.Product{
		{ID: "ram-1", SourcePlatform: model.PlatformJD, Title: "光威 32GB DDR5 6000 台式机内存条", Price: 499, Availability: "in_stock", Attributes: map[string]any{"category": "RAM"}},
		{ID: "ram-2", SourcePlatform: model.PlatformGoofish, Title: "金士顿 32GB DDR5 6000 台式机内存条", Price: 459, Availability: "in_stock", Attributes: map[string]any{"category": "RAM"}},
		{ID: "ram-mock", SourcePlatform: model.PlatformJD, Title: "海盗船 32GB DDR5 6000 台式机内存条", Price: 399, Availability: "in_stock", Attributes: map[string]any{"category": "RAM"}, RawPayload: map[string]any{"mock": true}},
	}
	repo.ListProductsFunc = func(_ context.Context, _ []model.SourcePlatform, _ int) ([]model.Product, error) {
		return repoProducts, nil
	}

	response, err := service.GeneratePriceCatalog(context.Background(), CatalogRequest{
		UseCase:   model.UseCaseGaming,
		BuildMode: model.ModeMixed,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("GeneratePriceCatalog() error = %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected 1 aggregated catalog item, got %d", len(response.Items))
	}
	item := response.Items[0]
	if item.SampleCount != 2 {
		t.Fatalf("expected sample_count 2, got %d", item.SampleCount)
	}
	if item.AvgPrice != 479 {
		t.Fatalf("expected avg_price 479, got %v", item.AvgPrice)
	}
	if len(item.SourceBreakdown) != 2 {
		t.Fatalf("expected 2 source breakdown entries, got %d", len(item.SourceBreakdown))
	}
}

func TestSelectBuildRealignsRAMAfterBudgetRebalance(t *testing.T) {
	grouped := map[model.PartCategory][]normalizedCandidate{
		model.CategoryCPU: {
			{Category: model.CategoryCPU, DisplayName: "CPU AMD Ryzen 5 7500F", PlatformFamily: "amd-am5", Product: model.Product{Price: 849}, EstimatedPower: 65},
		},
		model.CategoryMB: {
			{Category: model.CategoryMB, DisplayName: "MB B650M", PlatformFamily: "amd-am5", MemoryType: "DDR5", Product: model.Product{Price: 749}},
		},
		model.CategoryGPU: {
			{Category: model.CategoryGPU, DisplayName: "GPU RTX 4060", Product: model.Product{Price: 3410}, EstimatedPower: 115},
		},
		model.CategoryRAM: {
			{Category: model.CategoryRAM, DisplayName: "RAM Kingston DDR5", MemoryType: "DDR5", Product: model.Product{Price: 529}},
			{Category: model.CategoryRAM, DisplayName: "RAM Corsair DDR4", MemoryType: "DDR4", Product: model.Product{Price: 299}},
		},
		model.CategorySSD: {
			{Category: model.CategorySSD, DisplayName: "SSD SN770", Product: model.Product{Price: 429}},
		},
		model.CategoryPSU: {
			{Category: model.CategoryPSU, DisplayName: "PSU 650W", PSUWattage: 650, Product: model.Product{Price: 339}},
		},
		model.CategoryCase: {
			{Category: model.CategoryCase, DisplayName: "CASE AIR 100", Product: model.Product{Price: 279}},
		},
		model.CategoryCooler: {
			{Category: model.CategoryCooler, DisplayName: "COOLER AG400", Product: model.Product{Price: 129}},
		},
	}

	selected, findings, _ := selectBuild(grouped, 6000, model.UseCaseGaming, primaryProfile(model.UseCaseGaming))
	if selected[model.CategoryRAM].MemoryType != "DDR5" {
		t.Fatalf("expected RAM to be realigned to DDR5, got %s", selected[model.CategoryRAM].MemoryType)
	}
	index := map[string]CompatibilityFinding{}
	for _, finding := range findings {
		index[finding.Rule] = finding
	}
	if !index["mb_ram_memory_type"].Passed {
		t.Fatal("expected RAM/mainboard compatibility to pass after realignment")
	}
}
