package build

import "github.com/rigel-labs/rigel-build-engine/internal/domain/model"

type CatalogRequest struct {
	UseCase   model.UseCase   `json:"use_case"`
	BuildMode model.BuildMode `json:"build_mode"`
	Limit     int             `json:"limit"`
}

type PriceCatalogResponse struct {
	UseCase   model.UseCase      `json:"use_case"`
	BuildMode model.BuildMode    `json:"build_mode"`
	Warnings  []string           `json:"warnings,omitempty"`
	Items     []PriceCatalogItem `json:"items"`
}

type PriceCatalogItem struct {
	Category        model.PartCategory       `json:"category"`
	Brand           string                   `json:"brand"`
	Model           string                   `json:"model"`
	DisplayName     string                   `json:"display_name"`
	NormalizedKey   string                   `json:"normalized_key"`
	SampleCount     int                      `json:"sample_count"`
	AvgPrice        float64                  `json:"avg_price"`
	MedianPrice     float64                  `json:"median_price"`
	MinPrice        float64                  `json:"min_price"`
	MaxPrice        float64                  `json:"max_price"`
	Platforms       []model.SourcePlatform   `json:"platforms"`
	SourceBreakdown []PriceCatalogSourceItem `json:"source_breakdown,omitempty"`
}

type PriceCatalogSourceItem struct {
	SourcePlatform model.SourcePlatform `json:"source_platform"`
	SampleCount    int                  `json:"sample_count"`
	AvgPrice       float64              `json:"avg_price"`
	MinPrice       float64              `json:"min_price"`
	MaxPrice       float64              `json:"max_price"`
}
