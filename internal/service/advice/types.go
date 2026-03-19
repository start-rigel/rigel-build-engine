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

type BuildRecommendRequest struct {
	Budget    float64         `json:"budget"`
	UseCase   model.UseCase   `json:"use_case"`
	BuildMode model.BuildMode `json:"build_mode"`
	Notes     string          `json:"notes,omitempty"`
}

type BuildRecommendResponse struct {
	Provider       string            `json:"provider"`
	FallbackUsed   bool              `json:"fallback_used"`
	Request        BuildRequestEcho  `json:"request"`
	Summary        string            `json:"summary"`
	EstimatedTotal float64           `json:"estimated_total"`
	WithinBudget   bool              `json:"within_budget"`
	Warnings       []string          `json:"warnings,omitempty"`
	BuildItems     []BuildItem       `json:"build_items"`
	Advice         BuildAdviceDetail `json:"advice"`
}

type BuildRequestEcho struct {
	Budget    float64         `json:"budget"`
	UseCase   model.UseCase   `json:"use_case"`
	BuildMode model.BuildMode `json:"build_mode"`
	Notes     string          `json:"notes,omitempty"`
}

type BuildAdviceDetail struct {
	Reasons       []string `json:"reasons"`
	Risks         []string `json:"risks"`
	UpgradeAdvice []string `json:"upgrade_advice"`
}

type ProductRef struct {
	DisplayName string  `json:"display_name"`
	Model       string  `json:"model"`
	Price       float64 `json:"price"`
	MinPrice    float64 `json:"min_price"`
	MaxPrice    float64 `json:"max_price"`
	SampleCount int     `json:"sample_count"`
}

type BuildItem struct {
	Category           model.PartCategory `json:"category"`
	TargetModel        string             `json:"target_model"`
	SelectionReason    string             `json:"selection_reason"`
	PriceBasis         string             `json:"price_basis"`
	Confidence         float64            `json:"confidence"`
	RecommendedProduct *ProductRef        `json:"recommended_product,omitempty"`
	CandidateProducts  []ProductRef       `json:"candidate_products,omitempty"`
	Missing            bool               `json:"missing"`
	Reason             string             `json:"reason,omitempty"`
	SuggestedKeyword   string             `json:"suggested_keyword,omitempty"`
}
