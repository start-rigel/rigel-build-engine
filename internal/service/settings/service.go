package settings

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/config"
)

const (
	keyAIRuntime      = "ai_runtime"
	keyCatalogAILimit = "catalog_ai_limits"
)

type Repository interface {
	GetSystemSetting(ctx context.Context, key string) (json.RawMessage, bool, error)
	UpsertSystemSetting(ctx context.Context, key string, value json.RawMessage) error
}

type Service struct {
	repo        Repository
	defaultFrom config.Config
}

func New(repo Repository, cfg config.Config) *Service {
	return &Service{repo: repo, defaultFrom: cfg}
}

func (s *Service) GetView(ctx context.Context) (SystemSettingsView, error) {
	runtime, limits, err := s.GetEffective(ctx)
	if err != nil {
		return SystemSettingsView{}, err
	}
	var view SystemSettingsView
	view.AIRuntime.BaseURL = runtime.BaseURL
	view.AIRuntime.Model = runtime.Model
	view.AIRuntime.TimeoutSeconds = runtime.TimeoutSeconds
	view.AIRuntime.Enabled = runtime.Enabled
	view.AIRuntime.GatewayTokenConfigured = strings.TrimSpace(runtime.GatewayToken) != ""
	view.AIRuntime.GatewayTokenMasked = maskToken(runtime.GatewayToken)
	view.AIRuntime.APITokenConfigured = strings.TrimSpace(runtime.APIToken) != ""
	view.AIRuntime.APITokenMasked = maskToken(runtime.APIToken)
	view.CatalogAILimits = limits
	return view, nil
}

func (s *Service) GetEffective(ctx context.Context) (AIRuntime, CatalogAILimits, error) {
	runtime := AIRuntime{
		BaseURL:        strings.TrimSpace(s.defaultFrom.AIBaseURL),
		GatewayToken:   strings.TrimSpace(s.defaultFrom.AIGatewayToken),
		APIToken:       strings.TrimSpace(s.defaultFrom.AIToken),
		Model:          fallbackString(strings.TrimSpace(s.defaultFrom.AIModel), "openai/gpt-5.4-nano"),
		TimeoutSeconds: normalizeTimeout(int(s.defaultFrom.AITimeout.Seconds())),
		Enabled:        true,
	}
	limits := CatalogAILimits{MaxModelsPerCategory: 5}

	if raw, ok, err := s.repo.GetSystemSetting(ctx, keyAIRuntime); err != nil {
		return AIRuntime{}, CatalogAILimits{}, err
	} else if ok && len(raw) > 0 {
		var stored AIRuntime
		if err := json.Unmarshal(raw, &stored); err == nil {
			if strings.TrimSpace(stored.BaseURL) != "" {
				runtime.BaseURL = strings.TrimSpace(stored.BaseURL)
			}
			if strings.TrimSpace(stored.GatewayToken) != "" {
				runtime.GatewayToken = strings.TrimSpace(stored.GatewayToken)
			}
			if strings.TrimSpace(stored.APIToken) != "" {
				runtime.APIToken = strings.TrimSpace(stored.APIToken)
			}
			if strings.TrimSpace(stored.Model) != "" {
				runtime.Model = strings.TrimSpace(stored.Model)
			}
			if stored.TimeoutSeconds > 0 {
				runtime.TimeoutSeconds = normalizeTimeout(stored.TimeoutSeconds)
			}
			runtime.Enabled = stored.Enabled
		}
	}
	if raw, ok, err := s.repo.GetSystemSetting(ctx, keyCatalogAILimit); err != nil {
		return AIRuntime{}, CatalogAILimits{}, err
	} else if ok && len(raw) > 0 {
		var stored CatalogAILimits
		if err := json.Unmarshal(raw, &stored); err == nil && stored.MaxModelsPerCategory > 0 {
			limits.MaxModelsPerCategory = normalizeLimit(stored.MaxModelsPerCategory)
		}
	}
	return runtime, limits, nil
}

func (s *Service) Update(ctx context.Context, req UpdateSystemSettingsRequest) error {
	currentRuntime, currentLimits, err := s.GetEffective(ctx)
	if err != nil {
		return err
	}
	updatedRuntime := currentRuntime
	updatedLimits := currentLimits

	if req.AIRuntime.BaseURL != nil {
		trimmed := strings.TrimSpace(*req.AIRuntime.BaseURL)
		if trimmed != "" {
			updatedRuntime.BaseURL = trimmed
		}
	}
	if req.AIRuntime.Model != nil {
		trimmed := strings.TrimSpace(*req.AIRuntime.Model)
		if trimmed != "" {
			updatedRuntime.Model = trimmed
		}
	}
	if req.AIRuntime.TimeoutSeconds != nil && *req.AIRuntime.TimeoutSeconds > 0 {
		updatedRuntime.TimeoutSeconds = normalizeTimeout(*req.AIRuntime.TimeoutSeconds)
	}
	if req.AIRuntime.Enabled != nil {
		updatedRuntime.Enabled = *req.AIRuntime.Enabled
	}
	if req.AIRuntime.ClearGatewayToken {
		updatedRuntime.GatewayToken = ""
	} else if req.AIRuntime.GatewayToken != nil {
		trimmed := strings.TrimSpace(*req.AIRuntime.GatewayToken)
		if trimmed != "" {
			updatedRuntime.GatewayToken = trimmed
		}
	}
	if req.AIRuntime.ClearAPIToken {
		updatedRuntime.APIToken = ""
	} else if req.AIRuntime.APIToken != nil {
		trimmed := strings.TrimSpace(*req.AIRuntime.APIToken)
		if trimmed != "" {
			updatedRuntime.APIToken = trimmed
		}
	}
	if req.CatalogAILimits.MaxModelsPerCategory != nil && *req.CatalogAILimits.MaxModelsPerCategory > 0 {
		updatedLimits.MaxModelsPerCategory = normalizeLimit(*req.CatalogAILimits.MaxModelsPerCategory)
	}

	runtimeData, _ := json.Marshal(updatedRuntime)
	if err := s.repo.UpsertSystemSetting(ctx, keyAIRuntime, runtimeData); err != nil {
		return err
	}
	limitData, _ := json.Marshal(updatedLimits)
	if err := s.repo.UpsertSystemSetting(ctx, keyCatalogAILimit, limitData); err != nil {
		return err
	}
	return nil
}

func (s *Service) AIEnabled(runtime AIRuntime) bool {
	if !runtime.Enabled {
		return false
	}
	return strings.TrimSpace(runtime.BaseURL) != "" && strings.TrimSpace(runtime.GatewayToken) != "" && strings.TrimSpace(runtime.APIToken) != ""
}

func (s *Service) Timeout(runtime AIRuntime) time.Duration {
	return time.Duration(normalizeTimeout(runtime.TimeoutSeconds)) * time.Second
}

func normalizeLimit(v int) int {
	if v <= 0 {
		return 5
	}
	if v > 20 {
		return 20
	}
	return v
}

func normalizeTimeout(v int) int {
	if v <= 0 {
		return 25
	}
	if v > 120 {
		return 120
	}
	return v
}

func fallbackString(v, f string) string {
	if strings.TrimSpace(v) == "" {
		return f
	}
	return v
}

func maskToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
}
