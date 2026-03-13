package model

// PartSearchResult is the lightweight part payload exposed to upstream services.
type PartSearchResult struct {
	ID          ID           `json:"id"`
	Category    PartCategory `json:"category"`
	Brand       string       `json:"brand"`
	Model       string       `json:"model"`
	DisplayName string       `json:"display_name"`
}
