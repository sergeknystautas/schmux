package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type VariantSecrets map[string]map[string]string

func secretsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".schmux", "secrets.json"), nil
}

// LoadSecrets loads the secrets file or returns an empty map if it doesn't exist.
func LoadSecrets() (VariantSecrets, error) {
	path, err := secretsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return VariantSecrets{}, nil
		}
		return nil, fmt.Errorf("failed to read secrets file: %w", err)
	}

	var secrets VariantSecrets
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("failed to parse secrets file: %w", err)
	}
	if secrets == nil {
		secrets = VariantSecrets{}
	}
	return secrets, nil
}

// SaveVariantSecrets saves secrets for a specific variant.
func SaveVariantSecrets(variantName string, secrets map[string]string) error {
	if variantName == "" {
		return fmt.Errorf("variant name is required")
	}
	path, err := secretsPath()
	if err != nil {
		return err
	}

	existing, err := LoadSecrets()
	if err != nil {
		return err
	}
	if existing == nil {
		existing = VariantSecrets{}
	}

	existing[variantName] = secrets

	data, err := json.MarshalIndent(existing, "", "  ")
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

// DeleteVariantSecrets removes secrets for a specific variant.
func DeleteVariantSecrets(variantName string) error {
	if variantName == "" {
		return fmt.Errorf("variant name is required")
	}
	path, err := secretsPath()
	if err != nil {
		return err
	}

	existing, err := LoadSecrets()
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}

	if _, ok := existing[variantName]; !ok {
		return nil
	}
	delete(existing, variantName)

	data, err := json.MarshalIndent(existing, "", "  ")
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

// GetVariantSecrets returns secrets for a variant.
func GetVariantSecrets(variantName string) (map[string]string, error) {
	secrets, err := LoadSecrets()
	if err != nil {
		return nil, err
	}
	if secrets == nil {
		return map[string]string{}, nil
	}
	return secrets[variantName], nil
}
