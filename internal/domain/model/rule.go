package model

// CompatibilityRule expresses machine-runnable constraints between part categories.
type CompatibilityRule struct {
	ID              ID
	Name            string
	CategoryA       PartCategory
	CategoryB       PartCategory
	Operator        string
	LeftField       string
	RightField      string
	ExpectedValue   string
	Priority        int
	Severity        RiskLevel
	IsActive        bool
	UseCase         UseCase
	MessageTemplate string
	AuditFields
}

// ScoringProfile drives budget strategy and ranking weights.
type ScoringProfile struct {
	ID             ID
	Name           string
	UseCase        UseCase
	BuildMode      BuildMode
	Weights        map[string]float64
	BudgetStrategy map[string]float64
	IsDefault      bool
	AuditFields
}

// RiskTag carries warnings or blocks attached to products or parts.
type RiskTag struct {
	ID             ID
	PartID         ID
	ProductID      ID
	SourcePlatform SourcePlatform
	RiskType       string
	RiskLevel      RiskLevel
	Title          string
	Description    string
	Evidence       map[string]any
	IsActive       bool
	AuditFields
}
