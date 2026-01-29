package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/detect"
)

type ModelSecrets map[string]map[string]string

type SecretsFile struct {
	Models   ModelSecrets `json:"models,omitempty"`
	Variants ModelSecrets `json:"variants,omitempty"` // deprecated, migrated to models
	Auth     AuthSecrets  `json:"auth,omitempty"`
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".schmux", "secrets.json"), nil
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
		if secrets.Variants != nil && len(secrets.Variants) > 0 {
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
		if secrets.Variants != nil && len(secrets.Variants) > 0 {
			for k, v := range secrets.Variants {
				secrets.Models[k] = v
			}
			secrets.Variants = nil // Clear deprecated field
			// Best-effort save to persist migration
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
	return &SecretsFile{Models: legacy}, nil
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

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write secrets: %w", err)
	}
	return nil
}

// SaveModelSecrets saves secrets for a specific model.
func SaveModelSecrets(modelName string, secrets map[string]string) error {
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
	return SaveSecretsFile(existing)
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
	if secrets == nil || secrets.Models == nil {
		return map[string]string{}, nil
	}
	for _, model := range detect.GetBuiltinModels() {
		if model.Provider != provider {
			continue
		}
		if secrets.Models[model.ID] != nil {
			return secrets.Models[model.ID], nil
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
func DeleteProviderSecrets(provider string) error {
	if provider == "" {
		return nil
	}
	existing, err := LoadSecretsFile()
	if err != nil {
		return err
	}
	if existing.Models == nil {
		return nil
	}
	for _, model := range detect.GetBuiltinModels() {
		if model.Provider != provider {
			continue
		}
		delete(existing.Models, model.ID)
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
