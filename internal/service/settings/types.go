package settings

type AIRuntime struct {
	BaseURL        string `json:"base_url"`
	GatewayToken   string `json:"gateway_token,omitempty"`
	APIToken       string `json:"api_token,omitempty"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Enabled        bool   `json:"enabled"`
}

type CatalogAILimits struct {
	MaxModelsPerCategory int `json:"max_models_per_category"`
}

type SystemSettingsView struct {
	AIRuntime struct {
		BaseURL                string `json:"base_url"`
		Model                  string `json:"model"`
		TimeoutSeconds         int    `json:"timeout_seconds"`
		Enabled                bool   `json:"enabled"`
		GatewayTokenConfigured bool   `json:"gateway_token_configured"`
		GatewayTokenMasked     string `json:"gateway_token_masked"`
		APITokenConfigured     bool   `json:"api_token_configured"`
		APITokenMasked         string `json:"api_token_masked"`
	} `json:"ai_runtime"`
	CatalogAILimits CatalogAILimits `json:"catalog_ai_limits"`
}

type UpdateSystemSettingsRequest struct {
	AIRuntime struct {
		BaseURL           *string `json:"base_url,omitempty"`
		GatewayToken      *string `json:"gateway_token,omitempty"`
		APIToken          *string `json:"api_token,omitempty"`
		Model             *string `json:"model,omitempty"`
		TimeoutSeconds    *int    `json:"timeout_seconds,omitempty"`
		Enabled           *bool   `json:"enabled,omitempty"`
		ClearGatewayToken bool    `json:"clear_gateway_token,omitempty"`
		ClearAPIToken     bool    `json:"clear_api_token,omitempty"`
	} `json:"ai_runtime"`
	CatalogAILimits struct {
		MaxModelsPerCategory *int `json:"max_models_per_category,omitempty"`
	} `json:"catalog_ai_limits"`
}
