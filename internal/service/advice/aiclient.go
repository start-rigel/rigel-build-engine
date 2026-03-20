package advice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/service/settings"
)

type HTTPChatClient struct {
	httpClient *http.Client
}

func NewHTTPChatClient() *HTTPChatClient {
	return &HTTPChatClient{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *HTTPChatClient) ChatCompletion(ctx context.Context, runtime settings.AIRuntime, prompt string, timeout time.Duration) (string, error) {
	payload := map[string]any{
		"model": runtime.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a PC build planning assistant. Output JSON only."},
			{"role": "user", "content": prompt},
		},
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	body, _ := json.Marshal(payload)
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, runtime.BaseURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new ai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("cf-aig-authorization", "Bearer "+runtime.GatewayToken)
	req.Header.Set("Authorization", "Bearer "+runtime.APIToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do ai request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("decode ai response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty ai choices")
	}
	switch content := parsed.Choices[0].Message.Content.(type) {
	case string:
		return content, nil
	case []any:
		var parts []string
		for _, part := range content {
			item, ok := part.(map[string]any)
			if !ok {
				continue
			}
			text, ok := item["text"].(string)
			if ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), nil
		}
		raw, _ := json.Marshal(content)
		return string(raw), nil
	default:
		raw, _ := json.Marshal(content)
		return string(raw), nil
	}
}
