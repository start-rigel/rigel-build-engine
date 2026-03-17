package postgres

import "testing"

func TestDecodeJSONMap(t *testing.T) {
	var payload map[string]any
	err := decodeJSONMap([]byte(`{"category":"GPU","seed":true}`), &payload)
	if err != nil {
		t.Fatalf("decodeJSONMap() error = %v", err)
	}
	if payload["category"] != "GPU" {
		t.Fatalf("expected category GPU, got %#v", payload["category"])
	}
}
