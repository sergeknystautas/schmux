package directhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildAnthropicSystemPrompt_IncludesSchema(t *testing.T) {
	schema := `{"type":"object","properties":{"branch":{"type":"string"}}}`
	sys := buildAnthropicSystemPrompt(schema)
	if !strings.Contains(sys, schema) {
		t.Fatal("system prompt must embed schema verbatim")
	}
	if !strings.Contains(sys, "Respond with a single JSON object") {
		t.Fatal("system prompt must instruct JSON-only output")
	}
}

func TestBuildAnthropicRequest_ShapesBody(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4-6", "user prompt", "sys prompt", 4096)
	if req.Model != "claude-sonnet-4-6" {
		t.Errorf("model: got %q", req.Model)
	}
	if req.System != "sys prompt" {
		t.Errorf("system: got %q", req.System)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("messages: got %d, want 1", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || req.Messages[0].Content != "user prompt" {
		t.Errorf("message: got %+v", req.Messages[0])
	}
	if req.MaxTokens != 4096 {
		t.Errorf("max_tokens: got %d", req.MaxTokens)
	}
}

func TestExtractAnthropicText_JoinsTextBlocks(t *testing.T) {
	resp := anthropicResponse{
		Content: []anthropicBlock{
			{Type: "text", Text: "{\"branch\":"},
			{Type: "tool_use", Text: "ignored"},
			{Type: "text", Text: "\"feat/foo\"}"},
		},
	}
	got := extractAnthropicText(resp)
	want := `{"branch":"feat/foo"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCallAnthropic_OAuthTokenUsesBearerAndBetaHeader(t *testing.T) {
	var gotAuth, gotBeta, gotAPIKey, gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"{}"}]}`)
	}))
	defer server.Close()

	raw, err := callAnthropic(context.Background(), anthropicCallParams{
		Endpoint: server.URL,
		Token:    "sk-ant-oat-abc123",
		Request:  buildAnthropicRequest("m", "p", "s", 0),
	})
	if err != nil {
		t.Fatalf("callAnthropic: %v", err)
	}
	if raw != "{}" {
		t.Errorf("raw: got %q, want {}", raw)
	}
	if gotAuth != "Bearer sk-ant-oat-abc123" {
		t.Errorf("Authorization: got %q", gotAuth)
	}
	if gotBeta != anthropicOAuthBeta {
		t.Errorf("anthropic-beta: got %q", gotBeta)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key must be empty for OAuth, got %q", gotAPIKey)
	}
	if gotVersion == "" {
		t.Error("anthropic-version header must be set")
	}
}

func TestCallAnthropic_APIKeyUsesXApiKeyNoBeta(t *testing.T) {
	var gotAuth, gotBeta, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAPIKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	_, err := callAnthropic(context.Background(), anthropicCallParams{
		Endpoint: server.URL,
		Token:    "sk-ant-api03-notoauth",
		Request:  buildAnthropicRequest("m", "p", "s", 0),
	})
	if err != nil {
		t.Fatalf("callAnthropic: %v", err)
	}
	if gotAPIKey != "sk-ant-api03-notoauth" {
		t.Errorf("x-api-key: got %q", gotAPIKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization must be empty for api-key, got %q", gotAuth)
	}
	if gotBeta != "" {
		t.Errorf("anthropic-beta must be empty for api-key, got %q", gotBeta)
	}
}

func TestCallAnthropic_Non2xxReturnsErrHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	defer server.Close()

	_, err := callAnthropic(context.Background(), anthropicCallParams{
		Endpoint: server.URL,
		Token:    "sk-ant-oat-x",
		Request:  buildAnthropicRequest("m", "p", "s", 0),
	})
	if !errors.Is(err, ErrHTTP) {
		t.Fatalf("expected ErrHTTP, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code, got %v", err)
	}
}

func TestCallAnthropic_RequestBodyShape(t *testing.T) {
	var gotBody anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"x"}]}`)
	}))
	defer server.Close()

	req := buildAnthropicRequest("claude-sonnet-4-6", "why is the sky blue?", "be terse", 1024)
	_, _ = callAnthropic(context.Background(), anthropicCallParams{
		Endpoint: server.URL,
		Token:    "sk-ant-oat-x",
		Request:  req,
	})
	if gotBody.Model != "claude-sonnet-4-6" || gotBody.System != "be terse" || gotBody.MaxTokens != 1024 {
		t.Errorf("body: got %+v", gotBody)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Content != "why is the sky blue?" {
		t.Errorf("messages: got %+v", gotBody.Messages)
	}
}
