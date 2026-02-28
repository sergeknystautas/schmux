package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// setupSecretsHome creates a temp HOME with ~/.schmux/ and returns cleanup via t.Setenv.
func setupSecretsHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	schmuxDir := filepath.Join(tmpHome, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0700); err != nil {
		t.Fatalf("mkdir .schmux: %v", err)
	}
	return schmuxDir
}

func writeSecrets(t *testing.T, dir string, data interface{}) {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal secrets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), b, 0600); err != nil {
		t.Fatalf("write secrets.json: %v", err)
	}
}

func TestLoadSecretsFile_FileNotFound(t *testing.T) {
	dir := setupSecretsHome(t)
	// Do not create secrets.json
	_ = dir

	secrets, err := LoadSecretsFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secrets == nil {
		t.Fatal("expected non-nil secrets")
	}
	if secrets.Models == nil {
		t.Error("expected Models to be initialized, got nil")
	}
	if len(secrets.Models) != 0 {
		t.Errorf("expected empty Models, got %d entries", len(secrets.Models))
	}
}

func TestLoadSecretsFile_ModelsFormat(t *testing.T) {
	dir := setupSecretsHome(t)
	writeSecrets(t, dir, map[string]interface{}{
		"models": map[string]interface{}{
			"claude": map[string]string{"api_key": "sk-test"},
		},
		"auth": map[string]interface{}{
			"session_secret": "mysecret",
		},
	})

	secrets, err := LoadSecretsFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secrets.Models["claude"]["api_key"] != "sk-test" {
		t.Errorf("expected api_key=sk-test, got %q", secrets.Models["claude"]["api_key"])
	}
	if secrets.Auth.SessionSecret != "mysecret" {
		t.Errorf("expected session_secret=mysecret, got %q", secrets.Auth.SessionSecret)
	}
}

func TestLoadSecretsFile_VariantsOnlyFormat_MigratesToModels(t *testing.T) {
	dir := setupSecretsHome(t)
	writeSecrets(t, dir, map[string]interface{}{
		"variants": map[string]interface{}{
			"codex": map[string]string{"api_key": "sk-codex"},
		},
	})

	secrets, err := LoadSecretsFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Variants should be migrated to models
	if secrets.Models["codex"]["api_key"] != "sk-codex" {
		t.Errorf("expected codex api_key migrated to models, got %v", secrets.Models)
	}
	if secrets.Variants != nil {
		t.Error("expected Variants to be cleared after migration")
	}
}

func TestLoadSecretsFile_ModelsWithVariants_NoOverwrite(t *testing.T) {
	dir := setupSecretsHome(t)
	writeSecrets(t, dir, map[string]interface{}{
		"models": map[string]interface{}{
			"claude": map[string]string{"api_key": "sk-model"},
		},
		"variants": map[string]interface{}{
			"claude": map[string]string{"api_key": "sk-variant-should-not-win"},
			"codex":  map[string]string{"api_key": "sk-codex"},
		},
	})

	secrets, err := LoadSecretsFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Model key should NOT be overwritten by variant
	if secrets.Models["claude"]["api_key"] != "sk-model" {
		t.Errorf("expected model key preserved, got %q", secrets.Models["claude"]["api_key"])
	}
	// Variant-only key should be migrated
	if secrets.Models["codex"]["api_key"] != "sk-codex" {
		t.Errorf("expected codex migrated from variants, got %v", secrets.Models["codex"])
	}
	if secrets.Variants != nil {
		t.Error("expected Variants cleared after migration")
	}
}

func TestLoadSecretsFile_LegacyFlatFormat(t *testing.T) {
	dir := setupSecretsHome(t)
	// Legacy format: bare ModelSecrets (map of map)
	writeSecrets(t, dir, map[string]interface{}{
		"claude": map[string]string{"api_key": "sk-legacy"},
	})

	secrets, err := LoadSecretsFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secrets.Models["claude"]["api_key"] != "sk-legacy" {
		t.Errorf("expected legacy flat format parsed as Models, got %v", secrets.Models)
	}
}

func TestLoadSecretsFile_CorruptJSON(t *testing.T) {
	dir := setupSecretsHome(t)
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte("{broken"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadSecretsFile()
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
}

func TestLoadSecretsFile_EmptyModelsInitialized(t *testing.T) {
	dir := setupSecretsHome(t)
	// Models key present but empty
	writeSecrets(t, dir, map[string]interface{}{
		"models": map[string]interface{}{},
	})

	secrets, err := LoadSecretsFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secrets.Models == nil {
		t.Error("expected Models initialized even when empty in file")
	}
}

func TestLoadSecretsFile_AuthWithGitHub(t *testing.T) {
	dir := setupSecretsHome(t)
	writeSecrets(t, dir, map[string]interface{}{
		"auth": map[string]interface{}{
			"github": map[string]string{
				"client_id":     "gh-id",
				"client_secret": "gh-secret",
			},
		},
	})

	secrets, err := LoadSecretsFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secrets.Auth.GitHub == nil {
		t.Fatal("expected GitHub auth to be parsed")
	}
	if secrets.Auth.GitHub.ClientID != "gh-id" {
		t.Errorf("expected client_id=gh-id, got %q", secrets.Auth.GitHub.ClientID)
	}
	if secrets.Auth.GitHub.ClientSecret != "gh-secret" {
		t.Errorf("expected client_secret=gh-secret, got %q", secrets.Auth.GitHub.ClientSecret)
	}
}
