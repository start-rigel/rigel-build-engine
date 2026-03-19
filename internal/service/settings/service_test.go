package settings

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/config"
)

type memoryRepo struct {
	values map[string]json.RawMessage
}

func (r *memoryRepo) GetSystemSetting(_ context.Context, key string) (json.RawMessage, bool, error) {
	value, ok := r.values[key]
	return value, ok, nil
}

func (r *memoryRepo) UpsertSystemSetting(_ context.Context, key string, value json.RawMessage) error {
	r.values[key] = value
	return nil
}

func TestGetEffectivePrefersDatabase(t *testing.T) {
	repo := &memoryRepo{values: map[string]json.RawMessage{
		keyAIRuntime:      json.RawMessage(`{"base_url":"https://db.example","gateway_token":"db-g","api_token":"db-a","model":"db-model","timeout_seconds":33,"enabled":true}`),
		keyCatalogAILimit: json.RawMessage(`{"max_models_per_category":8}`),
	}}
	svc := New(repo, config.Config{
		AIBaseURL:      "https://env.example",
		AIGatewayToken: "env-g",
		AIToken:        "env-a",
		AIModel:        "env-model",
		AITimeout:      20 * time.Second,
	})
	runtime, limits, err := svc.GetEffective(context.Background())
	if err != nil {
		t.Fatalf("GetEffective() error = %v", err)
	}
	if runtime.BaseURL != "https://db.example" || runtime.Model != "db-model" || runtime.TimeoutSeconds != 33 {
		t.Fatalf("unexpected runtime: %+v", runtime)
	}
	if limits.MaxModelsPerCategory != 8 {
		t.Fatalf("unexpected limit: %+v", limits)
	}
}

func TestUpdateEmptyTokenDoesNotOverwrite(t *testing.T) {
	repo := &memoryRepo{values: map[string]json.RawMessage{
		keyAIRuntime:      json.RawMessage(`{"base_url":"https://db.example","gateway_token":"old-g","api_token":"old-a","model":"db-model","timeout_seconds":25,"enabled":true}`),
		keyCatalogAILimit: json.RawMessage(`{"max_models_per_category":5}`),
	}}
	svc := New(repo, config.Config{})
	blank := ""
	req := UpdateSystemSettingsRequest{}
	req.AIRuntime.GatewayToken = &blank
	req.AIRuntime.APIToken = &blank
	if err := svc.Update(context.Background(), req); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	runtime, _, err := svc.GetEffective(context.Background())
	if err != nil {
		t.Fatalf("GetEffective() error = %v", err)
	}
	if runtime.GatewayToken != "old-g" || runtime.APIToken != "old-a" {
		t.Fatalf("token should remain unchanged, got %+v", runtime)
	}
}

func TestGetEffectiveKeepsDatabaseClearedTokens(t *testing.T) {
	repo := &memoryRepo{values: map[string]json.RawMessage{
		keyAIRuntime:      json.RawMessage(`{"base_url":"https://db.example","gateway_token":"","api_token":"","model":"db-model","timeout_seconds":25,"enabled":true}`),
		keyCatalogAILimit: json.RawMessage(`{"max_models_per_category":5}`),
	}}
	svc := New(repo, config.Config{
		AIBaseURL:      "https://env.example",
		AIGatewayToken: "env-g",
		AIToken:        "env-a",
		AIModel:        "env-model",
		AITimeout:      20 * time.Second,
	})
	runtime, _, err := svc.GetEffective(context.Background())
	if err != nil {
		t.Fatalf("GetEffective() error = %v", err)
	}
	if runtime.GatewayToken != "" || runtime.APIToken != "" {
		t.Fatalf("expected db cleared token to override env, got %+v", runtime)
	}
}
