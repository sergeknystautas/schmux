package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserModels(t *testing.T) {
	dir := t.TempDir()
	modelsJSON := `{
		"models": [
			{
				"id": "my-model",
				"display_name": "My Model",
				"provider": "internal",
				"runner": "claude",
				"endpoint": "https://llm.internal.corp/anthropic",
				"required_secrets": ["ANTHROPIC_AUTH_TOKEN"]
			}
		]
	}`
	os.WriteFile(filepath.Join(dir, "models.json"), []byte(modelsJSON), 0644)

	models, err := LoadUserModels(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ID != "my-model" {
		t.Errorf("wrong ID: %q", models[0].ID)
	}
}

func TestLoadUserModels_Missing(t *testing.T) {
	models, err := LoadUserModels("/nonexistent/models.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Error("expected empty list for missing file")
	}
}

func TestValidateUserModels_ReservedPrefix(t *testing.T) {
	models := []UserModel{{ID: "default_claude", Runner: "claude"}}
	err := ValidateUserModels(models, []string{"claude"})
	if err == nil {
		t.Error("expected error for reserved prefix")
	}
}

func TestValidateUserModels_InvalidRunner(t *testing.T) {
	models := []UserModel{{ID: "my-model", Runner: "nonexistent"}}
	err := ValidateUserModels(models, []string{"claude", "codex"})
	if err == nil {
		t.Error("expected error for invalid runner")
	}
}

func TestValidateUserModels_DuplicateID(t *testing.T) {
	models := []UserModel{
		{ID: "my-model", Runner: "claude"},
		{ID: "my-model", Runner: "codex"},
	}
	err := ValidateUserModels(models, []string{"claude", "codex"})
	if err == nil {
		t.Error("expected error for duplicate ID")
	}
}

func TestSaveUserModels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")

	models := []UserModel{{
		ID:          "test-model",
		DisplayName: "Test",
		Provider:    "custom",
		Runner:      "claude",
	}}

	if err := SaveUserModels(path, models); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadUserModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != "test-model" {
		t.Errorf("round-trip failed: %+v", loaded)
	}
}

func TestUserModelsToDetect(t *testing.T) {
	userModels := []UserModel{
		{
			ID:              "my-model",
			DisplayName:     "My Model",
			Provider:        "custom",
			Runner:          "claude",
			Endpoint:        "https://llm.internal.corp/anthropic",
			RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		},
	}

	detectModels := UserModelsToDetect(userModels)
	if len(detectModels) != 1 {
		t.Fatalf("expected 1 model, got %d", len(detectModels))
	}

	m := detectModels[0]
	if m.ID != "my-model" {
		t.Errorf("wrong ID: %q", m.ID)
	}
	if m.DisplayName != "My Model" {
		t.Errorf("wrong display name: %q", m.DisplayName)
	}
	if m.Provider != "custom" {
		t.Errorf("wrong provider: %q", m.Provider)
	}
	if m.Category != "third-party" {
		t.Errorf("wrong category: %q", m.Category)
	}

	claudeRunner, ok := m.RunnerFor("claude")
	if !ok {
		t.Fatal("missing claude runner")
	}
	if claudeRunner.ModelValue != "my-model" {
		t.Errorf("wrong ModelValue: %q", claudeRunner.ModelValue)
	}
	if claudeRunner.Endpoint != "https://llm.internal.corp/anthropic" {
		t.Errorf("wrong endpoint: %q", claudeRunner.Endpoint)
	}
	if len(claudeRunner.RequiredSecrets) != 1 || claudeRunner.RequiredSecrets[0] != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("wrong required secrets: %v", claudeRunner.RequiredSecrets)
	}

	opencodeRunner, ok := m.RunnerFor("opencode")
	if !ok {
		t.Fatal("missing opencode runner")
	}
	if opencodeRunner.ModelValue != "custom/my-model" {
		t.Errorf("wrong opencode ModelValue: %q", opencodeRunner.ModelValue)
	}
}
