package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type VariantSecrets map[string]map[string]string

type SecretsFile struct {
	Variants VariantSecrets `json:"variants,omitempty"`
	Auth     AuthSecrets    `json:"auth,omitempty"`
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
			return &SecretsFile{Variants: VariantSecrets{}}, nil
		}
		return nil, fmt.Errorf("failed to read secrets file: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse secrets file: %w", err)
	}

	if _, ok := raw["variants"]; ok || raw["auth"] != nil {
		var secrets SecretsFile
		if err := json.Unmarshal(data, &secrets); err != nil {
			return nil, fmt.Errorf("failed to parse secrets file: %w", err)
		}
		if secrets.Variants == nil {
			secrets.Variants = VariantSecrets{}
		}
		return &secrets, nil
	}

	var legacy VariantSecrets
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("failed to parse secrets file: %w", err)
	}
	if legacy == nil {
		legacy = VariantSecrets{}
	}
	return &SecretsFile{Variants: legacy}, nil
}

func SaveSecretsFile(secrets *SecretsFile) error {
	path, err := secretsPath()
	if err != nil {
		return err
	}

	if secrets == nil {
		secrets = &SecretsFile{}
	}
	if secrets.Variants == nil {
		secrets.Variants = VariantSecrets{}
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

// SaveVariantSecrets saves secrets for a specific variant.
func SaveVariantSecrets(variantName string, secrets map[string]string) error {
	if variantName == "" {
		return fmt.Errorf("variant name is required")
	}

	existing, err := LoadSecretsFile()
	if err != nil {
		return err
	}
	if existing.Variants == nil {
		existing.Variants = VariantSecrets{}
	}

	existing.Variants[variantName] = secrets
	return SaveSecretsFile(existing)
}

// DeleteVariantSecrets removes secrets for a specific variant.
func DeleteVariantSecrets(variantName string) error {
	if variantName == "" {
		return fmt.Errorf("variant name is required")
	}

	existing, err := LoadSecretsFile()
	if err != nil {
		return err
	}
	if existing.Variants == nil {
		return nil
	}

	if _, ok := existing.Variants[variantName]; !ok {
		return nil
	}
	delete(existing.Variants, variantName)
	return SaveSecretsFile(existing)
}

// GetVariantSecrets returns secrets for a variant.
func GetVariantSecrets(variantName string) (map[string]string, error) {
	secrets, err := LoadSecretsFile()
	if err != nil {
		return nil, err
	}
	if secrets == nil || secrets.Variants == nil {
		return map[string]string{}, nil
	}
	return secrets.Variants[variantName], nil
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
