package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/directhttp"
)

// handleOneshotUpdates processes Anthropic token and Ollama endpoint updates.
// Called from handleConfigUpdate before validation.
func (h *ConfigHandlers) handleOneshotUpdates(w http.ResponseWriter, req contracts.ConfigUpdateRequest) bool {
	if req.AnthropicOAuthToken != nil {
		if err := config.SaveAnthropicOAuthToken(strings.TrimSpace(*req.AnthropicOAuthToken)); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to save Anthropic token: %v", err), http.StatusInternalServerError)
			return false
		}
	}

	if req.Ollama != nil && req.Ollama.Endpoint != nil {
		newEndpoint := strings.TrimSpace(*req.Ollama.Endpoint)
		if err := h.config.SetOllamaEndpoint(newEndpoint); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to save Ollama endpoint: %v", err), http.StatusInternalServerError)
			return false
		}
		if newEndpoint != "" {
			if ollamaModels, err := directhttp.ProbeOllama(newEndpoint, 2*time.Second); err == nil {
				directhttp.SetOllamaModels(ollamaModels)
			} else {
				directhttp.SetOllamaModels(nil)
			}
		} else {
			directhttp.SetOllamaModels(nil)
		}
	}

	return true
}
