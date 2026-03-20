package config

import (
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("RIGEL_SERVICE_NAME", "")
	t.Setenv("RIGEL_HTTP_PORT", "")
	t.Setenv("RIGEL_HTTP_READ_TIMEOUT", "")
	t.Setenv("RIGEL_HTTP_WRITE_TIMEOUT", "")
	t.Setenv("RIGEL_HTTP_IDLE_TIMEOUT", "")
	t.Setenv("RIGEL_POSTGRES_DSN", "postgres://rigel:rigel@postgres:5432/rigel?sslmode=disable")
	t.Setenv("RIGEL_INTERNAL_SERVICE_TOKEN", "test-token")
	t.Setenv("RIGEL_BUILD_ENGINE_ADMIN_TOKEN", "rigel_admin_token_for_test_123456")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BuildEngineMode != "local" {
		t.Fatalf("expected local mode, got %q", cfg.BuildEngineMode)
	}
}

func TestLoadRejectsWeakAdminToken(t *testing.T) {
	t.Setenv("RIGEL_HTTP_PORT", "")
	t.Setenv("RIGEL_POSTGRES_DSN", "postgres://rigel:rigel@postgres:5432/rigel?sslmode=disable")
	t.Setenv("RIGEL_BUILD_ENGINE_ADMIN_TOKEN", "short")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for weak admin token")
	}
	if !strings.Contains(err.Error(), "at least 24 characters") {
		t.Fatalf("unexpected error: %v", err)
	}
}
