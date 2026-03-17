package advice

import (
	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

// Advice is the normalized recommendation block returned to callers.
type Advice struct {
	Summary         string   `json:"summary"`
	Reasons         []string `json:"reasons"`
	FitFor          []string `json:"fit_for"`
	Risks           []string `json:"risks"`
	UpgradeAdvice   []string `json:"upgrade_advice"`
	AlternativeNote string   `json:"alternative_note"`
}

// GenerateCatalogRequest asks the service to produce a recommendation draft from a price catalog.
type GenerateCatalogRequest struct {
	Budget    float64                           `json:"budget"`
	UseCase   model.UseCase                     `json:"use_case"`
	BuildMode model.BuildMode                   `json:"build_mode"`
	Catalog   buildservice.PriceCatalogResponse `json:"catalog"`
}

// CatalogRecommendationItem is one selected line item in the recommendation draft.
type CatalogRecommendationItem struct {
	Category        model.PartCategory `json:"category"`
	DisplayName     string             `json:"display_name"`
	NormalizedKey   string             `json:"normalized_key"`
	SampleCount     int                `json:"sample_count"`
	SelectedPrice   float64            `json:"selected_price"`
	MedianPrice     float64            `json:"median_price"`
	SourcePlatforms []string           `json:"source_platforms,omitempty"`
	Reasons         []string           `json:"reasons,omitempty"`
}

// CatalogSelection captures the draft shopping list chosen from the catalog.
type CatalogSelection struct {
	Budget         float64                     `json:"budget"`
	UseCase        model.UseCase               `json:"use_case"`
	BuildMode      model.BuildMode             `json:"build_mode"`
	EstimatedTotal float64                     `json:"estimated_total"`
	Warnings       []string                    `json:"warnings,omitempty"`
	SelectedItems  []CatalogRecommendationItem `json:"selected_items"`
}

// GenerateCatalogResponse is returned when the service works from a price catalog.
type GenerateCatalogResponse struct {
	Provider     string           `json:"provider"`
	FallbackUsed bool             `json:"fallback_used"`
	Selection    CatalogSelection `json:"selection"`
	Advisory     Advice           `json:"advisory"`
}
