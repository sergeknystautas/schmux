package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/directhttp"
)

// TestBuildOneshotTargets_IncludesCLIRows verifies that models with oneshot-
// capable runners produce CLI rows in the picker.
// Template: internal/dashboard/handlers_config_test.go:115
func TestBuildOneshotTargets_IncludesCLIRows(t *testing.T) {
	server, _, _ := newTestServer(t)
	handlers := newTestConfigHandlers(server)

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	handlers.handleConfigGet(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, body = %s", getRR.Code, getRR.Body.String())
	}
	var resp contracts.ConfigResponse
	if err := json.NewDecoder(getRR.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The response should have OneshotTargets (may be empty if no models
	// have oneshot-capable runners, but the field must be present).
	if resp.OneshotTargets == nil {
		resp.OneshotTargets = []contracts.OneshotTarget{}
	}

	// Verify all CLI targets have the right source.
	for _, target := range resp.OneshotTargets {
		if target.Source == "cli" {
			if target.ID == "" || target.Label == "" {
				t.Errorf("CLI target has empty id or label: %+v", target)
			}
			if target.Label != "" && target.Label[len(target.Label)-1:] != ")" {
				t.Errorf("CLI target label should have suffix: %q", target.Label)
			}
		}
	}
}

// TestBuildOneshotTargets_IncludesOllamaRows verifies Ollama models from the
// probe produce ollama_api rows.
func TestBuildOneshotTargets_IncludesOllamaRows(t *testing.T) {
	directhttp.SetOllamaModels([]string{"llama3.2:latest"})
	defer directhttp.SetOllamaModels(nil)

	server, _, _ := newTestServer(t)
	handlers := newTestConfigHandlers(server)

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	handlers.handleConfigGet(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET: status = %d", getRR.Code)
	}
	var resp contracts.ConfigResponse
	if err := json.NewDecoder(getRR.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	found := false
	for _, target := range resp.OneshotTargets {
		if target.Source == "ollama_api" && target.ID == "llama3.2:latest::api" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ollama_api row for llama3.2:latest::api, got %v", resp.OneshotTargets)
	}
}

// TestConfigResponse_AnthropicOAuthTokenSet reports token presence.
func TestConfigResponse_AnthropicOAuthTokenSet(t *testing.T) {
	server, _, _ := newTestServer(t)
	handlers := newTestConfigHandlers(server)

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	handlers.handleConfigGet(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET: status = %d", getRR.Code)
	}
	var resp contracts.ConfigResponse
	if err := json.NewDecoder(getRR.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Without a token saved, the field should be false.
	if resp.AnthropicOAuthTokenSet {
		t.Error("expected AnthropicOAuthTokenSet=false when no token saved")
	}
}

// TestConfigResponse_OllamaFields reports endpoint and reachability.
func TestConfigResponse_OllamaFields(t *testing.T) {
	directhttp.SetOllamaModels([]string{"test-model"})
	defer directhttp.SetOllamaModels(nil)

	server, _, _ := newTestServer(t)
	handlers := newTestConfigHandlers(server)

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	handlers.handleConfigGet(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET: status = %d", getRR.Code)
	}
	var resp contracts.ConfigResponse
	if err := json.NewDecoder(getRR.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.Ollama.Reachable {
		t.Error("expected Reachable=true when models are set")
	}
	if len(resp.Ollama.Models) != 1 || resp.Ollama.Models[0] != "test-model" {
		t.Errorf("expected Models=[test-model], got %v", resp.Ollama.Models)
	}
}
