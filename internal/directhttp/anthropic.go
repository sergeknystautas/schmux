package directhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []anthropicBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

const anthropicDefaultMaxTokens = 4096
const anthropicAPIVersion = "2023-06-01"
const anthropicOAuthBeta = "oauth-2025-04-20"

type anthropicCallParams struct {
	Endpoint string
	Token    string
	Request  anthropicRequest
}

func buildAnthropicSystemPrompt(jsonSchema string) string {
	return fmt.Sprintf(
		"You are a structured-output assistant. Respond with a single JSON "+
			"object that matches this JSON Schema. Output the JSON object "+
			"only — no markdown, no prose, no code fences.\n\nSCHEMA:\n%s",
		jsonSchema,
	)
}

func buildAnthropicRequest(modelID, userPrompt, systemPrompt string, maxTokens int) anthropicRequest {
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	return anthropicRequest{
		Model:     modelID,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: userPrompt}},
	}
}

func extractAnthropicText(resp anthropicResponse) string {
	var out string
	for _, b := range resp.Content {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

func callAnthropic(ctx context.Context, p anthropicCallParams) (string, error) {
	body, err := json.Marshal(p.Request)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	if strings.HasPrefix(p.Token, "sk-ant-oat") {
		httpReq.Header.Set("Authorization", "Bearer "+p.Token)
		httpReq.Header.Set("anthropic-beta", anthropicOAuthBeta)
	} else {
		httpReq.Header.Set("x-api-key", p.Token)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(respBody)
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return "", fmt.Errorf("%w: %d %s: %s", ErrHTTP, resp.StatusCode, resp.Status, snippet)
	}
	var parsed anthropicResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode response: %w (body=%s)", err, string(respBody))
	}
	return extractAnthropicText(parsed), nil
}
