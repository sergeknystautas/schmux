package directhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type openaiRequest struct {
	Model     string          `json:"model"`
	Messages  []openaiMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message openaiMessage `json:"message"`
	} `json:"choices"`
}

type openaiCallParams struct {
	Endpoint string
	Request  openaiRequest
}

func buildOpenAIRequest(modelID, userPrompt, systemPrompt string, maxTokens int) openaiRequest {
	return openaiRequest{
		Model:     modelID,
		MaxTokens: maxTokens,
		Messages: []openaiMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
}

func callOpenAI(ctx context.Context, p openaiCallParams) (string, error) {
	body, err := json.Marshal(p.Request)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
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
	var parsed openaiResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode: %w (body=%s)", err, string(respBody))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai response has no choices (body=%s)", string(respBody))
	}
	return parsed.Choices[0].Message.Content, nil
}
