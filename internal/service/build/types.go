package build

import "github.com/rigel-labs/rigel-build-engine/internal/domain/model"

// GenerateRequest is the minimum viable input for build generation.
type GenerateRequest struct {
	Budget    float64         `json:"budget"`
	UseCase   model.UseCase   `json:"use_case"`
	BuildMode model.BuildMode `json:"build_mode"`
}

// Response is the public JSON structure returned by the build engine.
type Response struct {
	BuildRequestID string            `json:"build_request_id"`
	RequestNo      string            `json:"request_no"`
	Budget         float64           `json:"budget"`
	UseCase        model.UseCase     `json:"use_case"`
	BuildMode      model.BuildMode   `json:"build_mode"`
	Status         model.BuildStatus `json:"status"`
	Warnings       []string          `json:"warnings"`
	Results        []ResultPayload   `json:"results"`
}

// ResultPayload is one generated configuration candidate.
type ResultPayload struct {
	ResultID      string                 `json:"result_id"`
	Role          model.ResultRole       `json:"role"`
	TotalPrice    float64                `json:"total_price"`
	Score         float64                `json:"score"`
	Currency      string                 `json:"currency"`
	Items         []ItemPayload          `json:"items"`
	Compatibility []CompatibilityFinding `json:"compatibility"`
}

// ItemPayload is the selected item output shape consumed by later services.
type ItemPayload struct {
	Category       model.PartCategory   `json:"category"`
	DisplayName    string               `json:"display_name"`
	UnitPrice      float64              `json:"unit_price"`
	SourcePlatform model.SourcePlatform `json:"source_platform"`
	ProductID      string               `json:"product_id,omitempty"`
	PartID         string               `json:"part_id,omitempty"`
	Reasons        []string             `json:"reasons"`
	Risks          []string             `json:"risks"`
}

// CompatibilityFinding records hard or soft rule evaluation results.
type CompatibilityFinding struct {
	Rule     string          `json:"rule"`
	Severity model.RiskLevel `json:"severity"`
	Message  string          `json:"message"`
	Passed   bool            `json:"passed"`
}
