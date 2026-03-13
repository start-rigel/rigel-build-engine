package postgres

import (
	"testing"

	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

func TestDecodeCompatibilityFindings(t *testing.T) {
	findings, err := decodeCompatibilityFindings([]any{
		map[string]any{
			"rule":     "cpu_mb_platform",
			"severity": "blocked",
			"message":  "platform must match",
			"passed":   true,
		},
	})
	if err != nil {
		t.Fatalf("decodeCompatibilityFindings() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0] != (buildservice.CompatibilityFinding{
		Rule:     "cpu_mb_platform",
		Severity: "blocked",
		Message:  "platform must match",
		Passed:   true,
	}) {
		t.Fatalf("unexpected finding: %+v", findings[0])
	}
}
