package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	resp, err := buildAvailableModels(s.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read models: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"models": resp}); err != nil {
		s.logger.Error("failed to encode response", "handler", "models", "err", err)
	}
}

func buildAvailableModels(cfg *config.Config) ([]contracts.Model, error) {
	allModels := detect.GetBuiltinModels()
	detectedTools := config.DetectedToolsFromConfig(cfg)
	detected := make(map[string]bool, len(detectedTools))
	for _, t := range detectedTools {
		detected[t.Name] = true
	}

	resp := make([]contracts.Model, 0, len(allModels))
	for _, model := range allModels {
		runners := make(map[string]contracts.RunnerInfo, len(model.Runners))
		anyConfigured := false
		anyAvailable := false

		for toolName, spec := range model.Runners {
			available := detected[toolName]
			configured := true // no secrets needed = configured
			if len(spec.RequiredSecrets) > 0 {
				configured = false
				secrets, err := config.GetEffectiveModelSecrets(model)
				if err == nil {
					configured = true
					for _, key := range spec.RequiredSecrets {
						if strings.TrimSpace(secrets[key]) == "" {
							configured = false
							break
						}
					}
				}
			}
			runners[toolName] = contracts.RunnerInfo{
				Available:       available,
				Configured:      configured,
				RequiredSecrets: spec.RequiredSecrets,
			}
			if available && configured {
				anyConfigured = true
			}
			if available {
				anyAvailable = true
			}
		}

		// Only include models that have at least one available runner
		if !anyAvailable {
			continue
		}

		preferredTool := cfg.PreferredTool(model.ID)

		resp = append(resp, contracts.Model{
			ID:            model.ID,
			DisplayName:   model.DisplayName,
			Provider:      model.Provider,
			Category:      model.Category,
			UsageURL:      model.UsageURL,
			Configured:    anyConfigured,
			Runners:       runners,
			PreferredTool: preferredTool,
		})
	}
	return resp, nil
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

	model, ok := detect.FindModel(name)
	if !ok {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	configured, err := modelConfigured(model)
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

	model, ok := detect.FindModel(name)
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
	if err := validateModelSecrets(model, req.Secrets); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := config.SaveModelSecrets(model.ID, req.Secrets); err != nil {
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

	model, ok := detect.FindModel(name)
	if !ok {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	if targetInUseByNudgenikOrQuickLaunch(s.config, model.ID) {
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

func targetInUseByNudgenikOrQuickLaunch(cfg *config.Config, targetName string) bool {
	if cfg == nil || targetName == "" {
		return false
	}

	// Normalize to canonical model ID if targetName is a model or alias
	canonicalName := targetName
	if model, ok := detect.FindModel(targetName); ok {
		canonicalName = model.ID
	}

	if cfg.GetNudgenikTarget() == canonicalName {
		return true
	}
	for _, preset := range cfg.GetQuickLaunch() {
		if preset.Target == canonicalName {
			return true
		}
		// Also check if preset.Target is an alias that resolves to this model
		if model, ok := detect.FindModel(preset.Target); ok && model.ID == canonicalName {
			return true
		}
	}
	return false
}

func modelConfigured(model detect.Model) (bool, error) {
	for _, spec := range model.Runners {
		if len(spec.RequiredSecrets) == 0 {
			return true, nil // No secrets needed for at least one runner
		}
	}
	// Check if any runner has its secrets configured
	secrets, err := config.GetEffectiveModelSecrets(model)
	if err != nil {
		return false, err
	}
	for _, spec := range model.Runners {
		allPresent := true
		for _, key := range spec.RequiredSecrets {
			if strings.TrimSpace(secrets[key]) == "" {
				allPresent = false
				break
			}
		}
		if allPresent {
			return true, nil
		}
	}
	return false, nil
}

func validateModelSecrets(model detect.Model, secrets map[string]string) error {
	// Find required secrets from any runner that needs them
	for _, spec := range model.Runners {
		for _, key := range spec.RequiredSecrets {
			val := strings.TrimSpace(secrets[key])
			if val == "" {
				return fmt.Errorf("missing required secret %s", key)
			}
		}
	}
	return nil
}
