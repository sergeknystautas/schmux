package models

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/sergeknystautas/schmux/internal/detect"
)

// UserModel is a user-defined model entry.
type UserModel struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Runner          string   `json:"runner"`
	Endpoint        string   `json:"endpoint,omitempty"`
	RequiredSecrets []string `json:"required_secrets,omitempty"`
}

type userModelsFile struct {
	Models []UserModel `json:"models"`
}

// LoadUserModels loads user-defined models from a JSON file.
// Returns empty slice if file doesn't exist.
func LoadUserModels(path string) ([]UserModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var f userModelsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse user models: %w", err)
	}
	return f.Models, nil
}

// SaveUserModels writes user-defined models to a JSON file.
func SaveUserModels(path string, models []UserModel) error {
	f := userModelsFile{Models: models}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ValidateUserModels checks user models for validity.
func ValidateUserModels(models []UserModel, detectedTools []string) error {
	toolSet := make(map[string]bool)
	for _, t := range detectedTools {
		toolSet[t] = true
	}

	seen := make(map[string]bool)
	for _, m := range models {
		if m.ID == "" {
			return fmt.Errorf("model ID is required")
		}
		if strings.HasPrefix(m.ID, "default_") {
			return fmt.Errorf("model ID %q uses reserved prefix 'default_'", m.ID)
		}
		if seen[m.ID] {
			return fmt.Errorf("duplicate model ID: %q", m.ID)
		}
		seen[m.ID] = true

		if m.Runner == "" {
			return fmt.Errorf("model %q: runner is required", m.ID)
		}
		if !toolSet[m.Runner] {
			return fmt.Errorf("model %q: unknown runner %q (available: %v)", m.ID, m.Runner, detectedTools)
		}
		if m.Endpoint != "" {
			if _, err := url.ParseRequestURI(m.Endpoint); err != nil {
				return fmt.Errorf("model %q: invalid endpoint URL: %w", m.ID, err)
			}
		}
	}
	return nil
}

// UserModelsToDetect converts user models to detect.Model entries.
func UserModelsToDetect(models []UserModel) []detect.Model {
	var result []detect.Model
	for _, um := range models {
		provider := um.Provider
		if provider == "" {
			provider = "custom"
		}
		displayName := um.DisplayName
		if displayName == "" {
			displayName = um.ID
		}

		runners := map[string]detect.RunnerSpec{
			um.Runner: {
				ModelValue:      um.ID,
				Endpoint:        um.Endpoint,
				RequiredSecrets: um.RequiredSecrets,
			},
			"opencode": {
				ModelValue: provider + "/" + um.ID,
			},
		}

		result = append(result, detect.Model{
			ID:          um.ID,
			DisplayName: displayName,
			Provider:    provider,
			Category:    "third-party",
			Runners:     runners,
		})
	}
	return result
}
