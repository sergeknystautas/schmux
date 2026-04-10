package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

type ModelSecrets map[string]map[string]string

type SecretsFile struct {
	Models    ModelSecrets                 `json:"models,omitempty"`
	Variants  ModelSecrets                 `json:"variants,omitempty"` // deprecated, migrated to models
	Providers map[string]map[string]string `json:"providers,omitempty"`
	Auth      AuthSecrets                  `json:"auth,omitempty"`
}

type AuthSecrets struct {
	GitHub        *GitHubSecrets `json:"github,omitempty"`
	SessionSecret string         `json:"session_secret,omitempty"`
}

type GitHubSecrets struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

func secretsPath() (string, error) {
	d := schmuxdir.Get()
	if d == "" {
		return "", fmt.Errorf("failed to resolve schmux directory")
	}
	return filepath.Join(d, "secrets.json"), nil
}

// LoadSecretsFile loads the secrets file or returns an empty structure if it doesn't exist.
func LoadSecretsFile() (*SecretsFile, error) {
	path, err := secretsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SecretsFile{Models: ModelSecrets{}}, nil
		}
		return nil, fmt.Errorf("failed to read secrets file: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse secrets file: %w", err)
	}

	if _, ok := raw["models"]; ok || raw["auth"] != nil {
		var secrets SecretsFile
		if err := json.Unmarshal(data, &secrets); err != nil {
			return nil, fmt.Errorf("failed to parse secrets file: %w", err)
		}
		if secrets.Models == nil {
			secrets.Models = ModelSecrets{}
		}
		// Migrate variants to models if present
		if len(secrets.Variants) > 0 {
			if secrets.Models == nil {
				secrets.Models = ModelSecrets{}
			}
			for k, v := range secrets.Variants {
				if _, exists := secrets.Models[k]; !exists {
					secrets.Models[k] = v
				}
			}
			secrets.Variants = nil // Clear deprecated field
			// Best-effort save to persist migration
			_ = SaveSecretsFile(&secrets)
		}
		if migrateSecretKeys(&secrets) || migrateToProviderKeyed(&secrets) {
			_ = SaveSecretsFile(&secrets)
		}
		return &secrets, nil
	}

	if _, ok := raw["variants"]; ok {
		var secrets SecretsFile
		if err := json.Unmarshal(data, &secrets); err != nil {
			return nil, fmt.Errorf("failed to parse secrets file: %w", err)
		}
		if secrets.Models == nil {
			secrets.Models = ModelSecrets{}
		}
		// Migrate variants to models
		if len(secrets.Variants) > 0 {
			for k, v := range secrets.Variants {
				secrets.Models[k] = v
			}
			secrets.Variants = nil // Clear deprecated field
			// Best-effort save to persist migration
			_ = SaveSecretsFile(&secrets)
		}
		if migrateSecretKeys(&secrets) || migrateToProviderKeyed(&secrets) {
			_ = SaveSecretsFile(&secrets)
		}
		return &secrets, nil
	}

	var legacy ModelSecrets
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("failed to parse secrets file: %w", err)
	}
	if legacy == nil {
		legacy = ModelSecrets{}
	}
	secrets := &SecretsFile{Models: legacy}
	if migrateSecretKeys(secrets) || migrateToProviderKeyed(secrets) {
		_ = SaveSecretsFile(secrets)
	}
	return secrets, nil
}

func SaveSecretsFile(secrets *SecretsFile) error {
	path, err := secretsPath()
	if err != nil {
		return err
	}

	if secrets == nil {
		secrets = &SecretsFile{}
	}
	if secrets.Models == nil {
		secrets.Models = ModelSecrets{}
	}

	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal secrets: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	if err := fileutil.AtomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write secrets: %w", err)
	}
	return nil
}

// SaveModelSecrets saves secrets for a specific model.
func SaveModelSecrets(modelName string, provider string, secrets map[string]string) error {
	if modelName == "" {
		return fmt.Errorf("model name is required")
	}

	existing, err := LoadSecretsFile()
	if err != nil {
		return err
	}
	if existing.Models == nil {
		existing.Models = ModelSecrets{}
	}

	existing.Models[modelName] = secrets

	// Also update providers map
	if provider == "" {
		provider = getProviderForModel(modelName)
	}
	if provider != "" {
		if existing.Providers == nil {
			existing.Providers = make(map[string]map[string]string)
		}
		existing.Providers[provider] = secrets
	}

	return SaveSecretsFile(existing)
}

// legacyModelProviders maps old model IDs to providers for callers of
// the deleted GetBuiltinModels(). Only IDs that existed in the old
// builtinModels list, plus legacy aliases and models.dev mixed-case IDs.
var legacyModelProviders = map[string]string{
	// Anthropic native
	"claude-opus-4-6": "anthropic", "claude-sonnet-4-6": "anthropic", "claude-haiku-4-5": "anthropic",
	"claude-opus-4-5": "anthropic", "claude-opus-4-1": "anthropic", "claude-opus-4": "anthropic",
	"claude-sonnet-4-5": "anthropic", "claude-sonnet-4": "anthropic",
	// Third-party via claude
	"kimi-thinking": "moonshot", "kimi-k2.5": "moonshot",
	"glm-4.7": "zai", "glm-4.5-air": "zai", "glm-5": "zai", "glm-5-turbo": "zai",
	"minimax-m2.1": "minimax", "minimax-2.5": "minimax", "minimax-2.7": "minimax",
	"qwen3-coder-plus": "dashscope",
	// OpenAI/Codex
	"gpt-5.4": "openai", "gpt-5.3-codex": "openai", "gpt-5.2": "openai",
	"gpt-5.2-codex": "openai", "gpt-5.1-codex-max": "openai",
	"gpt-5.1-codex": "openai", "gpt-5.1-codex-mini": "openai", "gpt-5-codex": "openai",
	// Google/Gemini
	"gemini-3.1-pro-preview": "google", "gemini-3-flash-preview": "google",
	"gemini-2.5-pro": "google", "gemini-2.5-flash": "google",
	"gemini-2.5-flash-lite": "google", "gemini-2.0-flash": "google",
	// OpenCode
	"opencode-zen": "opencode-zen",
	// Legacy aliases
	"claude-opus": "anthropic", "claude-sonnet": "anthropic", "claude-haiku": "anthropic",
	"opus": "anthropic", "sonnet": "anthropic", "haiku": "anthropic",
	"minimax": "minimax",
	// models.dev IDs (targets of ID migration)
	"MiniMax-M2.1": "minimax", "MiniMax-M2.5": "minimax", "MiniMax-M2.7": "minimax",
	"kimi-k2-thinking": "moonshot",
	// Dated Anthropic IDs (targets of legacy migrations)
	"claude-opus-4-5-20251101": "anthropic", "claude-opus-4-1-20250805": "anthropic",
	"claude-sonnet-4-5-20250929": "anthropic", "claude-opus-4-20250514": "anthropic",
	"claude-sonnet-4-20250514": "anthropic", "claude-haiku-4-5-20251001": "anthropic",
	// Old default_* model IDs
	"default_claude": "anthropic", "default_codex": "openai",
	"default_gemini": "google", "default_opencode": "opencode",
}

// getProviderForModel returns the provider for a given model ID using the
// static legacy map. Used only by SaveModelSecrets when no provider is passed.
func getProviderForModel(modelID string) string {
	return legacyModelProviders[modelID]
}

// DeleteModelSecrets removes secrets for a specific model.
func DeleteModelSecrets(modelName string) error {
	if modelName == "" {
		return fmt.Errorf("model name is required")
	}

	existing, err := LoadSecretsFile()
	if err != nil {
		return err
	}
	if existing.Models == nil {
		return nil
	}

	if _, ok := existing.Models[modelName]; !ok {
		return nil
	}
	delete(existing.Models, modelName)
	return SaveSecretsFile(existing)
}

// GetModelSecrets returns secrets for a model.
func GetModelSecrets(modelName string) (map[string]string, error) {
	secrets, err := LoadSecretsFile()
	if err != nil {
		return nil, err
	}
	if secrets == nil || secrets.Models == nil {
		return map[string]string{}, nil
	}
	return secrets.Models[modelName], nil
}

// GetProviderSecrets returns the first matching secrets map for a provider.
// Provider IDs should match detect.Model.Provider.
func GetProviderSecrets(provider string) (map[string]string, error) {
	if provider == "" {
		return map[string]string{}, nil
	}
	secrets, err := LoadSecretsFile()
	if err != nil {
		return nil, err
	}
	// First try provider-keyed lookup (new format)
	if secrets != nil && secrets.Providers != nil {
		if providerSecrets, ok := secrets.Providers[provider]; ok {
			return providerSecrets, nil
		}
	}
	// Fall back to model-keyed lookup (legacy format)
	if secrets == nil || secrets.Models == nil {
		return map[string]string{}, nil
	}
	for modelID, modelProvider := range legacyModelProviders {
		if modelProvider != provider {
			continue
		}
		if secrets.Models[modelID] != nil {
			return secrets.Models[modelID], nil
		}
	}
	return map[string]string{}, nil
}

// GetEffectiveModelSecrets merges provider secrets with model-specific secrets.
// Model-specific secrets take precedence.
func GetEffectiveModelSecrets(model detect.Model) (map[string]string, error) {
	providerSecrets, err := GetProviderSecrets(model.Provider)
	if err != nil {
		return nil, err
	}
	modelSecrets, err := GetModelSecrets(model.ID)
	if err != nil {
		return nil, err
	}
	if len(providerSecrets) == 0 && len(modelSecrets) == 0 {
		return map[string]string{}, nil
	}
	merged := make(map[string]string, len(providerSecrets)+len(modelSecrets))
	for k, v := range providerSecrets {
		merged[k] = v
	}
	for k, v := range modelSecrets {
		merged[k] = v
	}
	return merged, nil
}

// DeleteProviderSecrets removes secrets for all models owned by the provider.
// It removes from both the legacy Models map and the new Providers map.
func DeleteProviderSecrets(provider string) error {
	if provider == "" {
		return nil
	}
	existing, err := LoadSecretsFile()
	if err != nil {
		return err
	}
	// Delete from Providers map (new format)
	if existing.Providers != nil {
		delete(existing.Providers, provider)
	}
	// Delete from Models map (legacy format)
	if existing.Models != nil {
		for modelID, modelProvider := range legacyModelProviders {
			if modelProvider != provider {
				continue
			}
			delete(existing.Models, modelID)
		}
	}
	return SaveSecretsFile(existing)
}

// GetAuthSecrets returns auth secrets.
func GetAuthSecrets() (AuthSecrets, error) {
	secrets, err := LoadSecretsFile()
	if err != nil {
		return AuthSecrets{}, err
	}
	return secrets.Auth, nil
}

// SaveGitHubAuthSecrets saves GitHub auth client credentials.
func SaveGitHubAuthSecrets(clientID, clientSecret string) error {
	secrets, err := LoadSecretsFile()
	if err != nil {
		return err
	}
	if secrets.Auth.GitHub == nil {
		secrets.Auth.GitHub = &GitHubSecrets{}
	}
	secrets.Auth.GitHub.ClientID = clientID
	secrets.Auth.GitHub.ClientSecret = clientSecret
	return SaveSecretsFile(secrets)
}

// EnsureSessionSecret returns the session secret, creating one if missing.
func EnsureSessionSecret() (string, error) {
	secrets, err := LoadSecretsFile()
	if err != nil {
		return "", err
	}
	if secrets.Auth.SessionSecret != "" {
		return secrets.Auth.SessionSecret, nil
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate session secret: %w", err)
	}
	secrets.Auth.SessionSecret = base64.RawStdEncoding.EncodeToString(buf)
	if err := SaveSecretsFile(secrets); err != nil {
		return "", err
	}
	return secrets.Auth.SessionSecret, nil
}

// GetSessionSecret returns the session secret if present.
func GetSessionSecret() (string, error) {
	secrets, err := LoadSecretsFile()
	if err != nil {
		return "", err
	}
	return secrets.Auth.SessionSecret, nil
}

// migrateSecretKeys renames old model ID keys to vendor-defined IDs.
// Returns true if any keys were migrated.
func migrateSecretKeys(secrets *SecretsFile) bool {
	if secrets == nil || secrets.Models == nil {
		return false
	}
	changed := false
	for oldID, newID := range detect.LegacyIDMigrations() {
		if oldID == newID {
			continue
		}
		if s, ok := secrets.Models[oldID]; ok {
			if _, exists := secrets.Models[newID]; !exists {
				secrets.Models[newID] = s
			}
			delete(secrets.Models, oldID)
			changed = true
		}
	}
	return changed
}

// migrateToProviderKeyed converts model-keyed secrets to provider-keyed format.
// This groups secrets by provider, so "moonshot" provider secrets apply to all moonshot models.
func migrateToProviderKeyed(secrets *SecretsFile) bool {
	if secrets == nil || secrets.Models == nil || len(secrets.Models) == 0 {
		return false
	}
	if secrets.Providers == nil {
		secrets.Providers = make(map[string]map[string]string)
	}

	providerToModels := make(map[string][]string)
	for modelID, provider := range legacyModelProviders {
		providerToModels[provider] = append(providerToModels[provider], modelID)
	}

	changed := false
	// For each model secret, find its provider and add to providers map
	for modelID, modelSecrets := range secrets.Models {
		// Find provider for this model
		var provider string
		for p, models := range providerToModels {
			for _, m := range models {
				if m == modelID {
					provider = p
					break
				}
			}
			if provider != "" {
				break
			}
		}
		if provider == "" {
			continue // Can't determine provider, skip
		}
		// Add to providers map (provider secrets take precedence)
		if _, exists := secrets.Providers[provider]; !exists {
			secrets.Providers[provider] = modelSecrets
			changed = true
		}
	}
	return changed
}
