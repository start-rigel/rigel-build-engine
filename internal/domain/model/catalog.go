package model

import "time"

// Part is the canonical hardware entry used by the build engine.
type Part struct {
	ID               ID
	Category         PartCategory
	Brand            string
	Series           string
	Model            string
	DisplayName      string
	NormalizedKey    string
	Generation       string
	MSRP             float64
	ReleaseYear      int
	LifecycleStatus  string
	SourceConfidence float64
	AliasKeywords    []string
	AuditFields
}

// Product preserves raw marketplace information for later normalization.
type Product struct {
	ID             ID
	SourcePlatform SourcePlatform
	ExternalID     string
	SKUID          string
	Title          string
	Subtitle       string
	URL            string
	ImageURL       string
	ShopName       string
	ShopType       ShopType
	SellerName     string
	Region         string
	Price          float64
	Currency       string
	Availability   string
	Attributes     map[string]any
	RawPayload     map[string]any
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	AuditFields
}

// ProductPartMapping links a raw product to a canonical part candidate.
type ProductPartMapping struct {
	ID                   ID
	ProductID            ID
	PartID               ID
	MappingStatus        MappingStatus
	MatchConfidence      float64
	MatchedBy            string
	CandidateDisplayName string
	Reason               string
	AuditFields
}

// PriceSnapshot stores append-only historical prices.
type PriceSnapshot struct {
	ID             ID
	ProductID      ID
	PartID         ID
	SourcePlatform SourcePlatform
	Price          float64
	InStock        bool
	CapturedAt     time.Time
	Metadata       map[string]any
}

// PartMarketSummary exposes aggregated price references per source.
type PartMarketSummary struct {
	ID              ID
	PartID          ID
	SourcePlatform  SourcePlatform
	LatestPrice     float64
	MinPrice        float64
	MaxPrice        float64
	MedianPrice     float64
	P25Price        float64
	P75Price        float64
	SampleCount     int
	WindowDays      int
	LastCollectedAt *time.Time
	AuditFields
}
