package model

import "time"

// MVP keeps identifiers as strings to avoid binding the domain layer to a specific UUID library.
type ID string

type PartCategory string

type SourcePlatform string

type ShopType string

type MappingStatus string

type BuildMode string

type UseCase string

type JobStatus string

type JobType string

const (
	CategoryCPU    PartCategory = "CPU"
	CategoryMB     PartCategory = "MB"
	CategoryGPU    PartCategory = "GPU"
	CategoryRAM    PartCategory = "RAM"
	CategorySSD    PartCategory = "SSD"
	CategoryHDD    PartCategory = "HDD"
	CategoryPSU    PartCategory = "PSU"
	CategoryCase   PartCategory = "CASE"
	CategoryCooler PartCategory = "COOLER"
)

const (
	PlatformJD SourcePlatform = "jd"
)

const (
	ModeNewOnly  BuildMode = "new_only"
	ModeUsedOnly BuildMode = "used_only"
	ModeMixed    BuildMode = "mixed"
)

const (
	UseCaseGaming UseCase = "gaming"
	UseCaseOffice UseCase = "office"
	UseCaseDesign UseCase = "design"
)

const (
	JobTypeJDCollect     JobType = "jd_collect"
	JobTypeMarketSummary JobType = "market_summary"
)

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

// AuditFields captures shared timestamp columns.
type AuditFields struct {
	CreatedAt time.Time
	UpdatedAt time.Time
}
