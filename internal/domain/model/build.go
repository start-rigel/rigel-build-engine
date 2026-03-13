package model

import "time"

// BuildRequest is the normalized input persisted before generation.
type BuildRequest struct {
	ID            ID
	RequestNo     string
	Budget        float64
	UseCase       UseCase
	BuildMode     BuildMode
	PinnedPartIDs []ID
	Constraints   map[string]any
	Status        BuildStatus
	RequestedBy   string
	AuditFields
}

// BuildResult represents a generated build candidate.
type BuildResult struct {
	ID               ID
	BuildRequestID   ID
	ResultRole       ResultRole
	ScoringProfileID ID
	TotalPrice       float64
	Score            float64
	Currency         string
	Summary          map[string]any
	AuditFields
	Items []BuildResultItem
}

// BuildResultItem is a selected part/product inside a build result.
type BuildResultItem struct {
	ID             ID
	BuildResultID  ID
	PartID         ID
	ProductID      ID
	Category       PartCategory
	DisplayName    string
	UnitPrice      float64
	Quantity       int
	SourcePlatform SourcePlatform
	IsPrimary      bool
	Reasons        []string
	Risks          []string
	SortOrder      int
	AuditFields
}

// Job tracks collector and processing executions.
type Job struct {
	ID             ID
	JobType        JobType
	Status         JobStatus
	SourcePlatform SourcePlatform
	Payload        map[string]any
	Result         map[string]any
	ScheduledAt    *time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	RetryCount     int
	ErrorMessage   string
	AuditFields
}
