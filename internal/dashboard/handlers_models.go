package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	catalog, err := s.models.GetCatalog()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read models: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"models": catalog.Models}); err != nil {
		s.logger.Error("failed to encode response", "handler", "models", "err", err)
	}
}

func buildTLS(cfg *config.Config) *contracts.TLS {
	certPath := cfg.GetTLSCertPath()
	keyPath := cfg.GetTLSKeyPath()
	if certPath == "" && keyPath == "" {
		return nil
	}
	return &contracts.TLS{
		CertPath: certPath,
		KeyPath:  keyPath,
	}
}

// handleModelConfigured handles GET /api/models/{name}/configured
func (s *Server) handleModelConfigured(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "model name required", http.StatusBadRequest)
		return
	}

	model, ok := s.models.FindModel(name)
	if !ok {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	configured, err := s.models.IsConfigured(model)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read secrets: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"configured": configured})
}

// handleModelSecretsPost handles POST /api/models/{name}/secrets
func (s *Server) handleModelSecretsPost(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "model name required", http.StatusBadRequest)
		return
	}

	model, ok := s.models.FindModel(name)
	if !ok {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	type SecretsRequest struct {
		Secrets map[string]string `json:"secrets"`
	}
	var req SecretsRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if err := s.models.ValidateSecrets(model, req.Secrets); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := config.SaveModelSecrets(model.ID, model.Provider, req.Secrets); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save secrets: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleModelSecretsDelete handles DELETE /api/models/{name}/secrets
func (s *Server) handleModelSecretsDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "model name required", http.StatusBadRequest)
		return
	}

	model, ok := s.models.FindModel(name)
	if !ok {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	if s.models.IsTargetInUse(model.ID) {
		http.Error(w, "model is in use by nudgenik or quick launch", http.StatusBadRequest)
		return
	}
	if model.Provider != "" && model.Provider != "anthropic" {
		if err := config.DeleteProviderSecrets(model.Provider); err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete secrets: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		if err := config.DeleteModelSecrets(model.ID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete secrets: %v", err), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
