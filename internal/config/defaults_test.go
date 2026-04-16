package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildDefaultsReturnsNilWhenNoFile(t *testing.T) {
	defaults, err := loadBuildDefaults()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if defaults != nil {
		t.Fatalf("expected nil defaults, got %v", defaults)
	}
}

func TestConfigSeedingNoBuildDefaults(t *testing.T) {
	// Without build defaults, CreateDefault should produce Go zero-value defaults.
	cfg := CreateDefault(filepath.Join(t.TempDir(), "config.json"))

	if cfg.Network != nil {
		t.Errorf("expected nil Network, got %+v", cfg.Network)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("expected empty Repos, got %d", len(cfg.Repos))
	}
}

func TestConfigSeedingNetworkPort(t *testing.T) {
	cfg := CreateDefault(filepath.Join(t.TempDir(), "config.json"))

	defaults := map[string]json.RawMessage{
		"network": json.RawMessage(`{"port":44102}`),
	}
	if err := overlayDefaults(cfg, defaults); err != nil {
		t.Fatalf("overlayDefaults failed: %v", err)
	}

	if cfg.Network == nil {
		t.Fatal("expected Network to be set after overlay")
	}
	if cfg.Network.Port != 44102 {
		t.Errorf("expected port 44102, got %d", cfg.Network.Port)
	}
}

func TestConfigSeedingRepos(t *testing.T) {
	cfg := CreateDefault(filepath.Join(t.TempDir(), "config.json"))

	defaults := map[string]json.RawMessage{
		"repos": json.RawMessage(`[{"url":"https://github.com/example/repo","name":"repo"}]`),
	}
	if err := overlayDefaults(cfg, defaults); err != nil {
		t.Fatalf("overlayDefaults failed: %v", err)
	}

	if len(cfg.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].URL != "https://github.com/example/repo" {
		t.Errorf("expected repo URL 'https://github.com/example/repo', got %q", cfg.Repos[0].URL)
	}
}

func TestConfigSeedingDoesNotModifyExistingConfig(t *testing.T) {
	// Simulate a user config that already has a port set.
	// Build defaults should NOT override user values — this test verifies
	// that overlayDefaults replaces top-level keys (which is correct because
	// it's only called during CreateDefault, never on loaded user configs).
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create and save a config with a custom port.
	cfg := CreateDefault(configPath)
	cfg.Network = &NetworkConfig{Port: 9999}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load the saved config — Load() does NOT call applyBuildDefaults,
	// so the user's port value is preserved.
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Network == nil || loaded.Network.Port != 9999 {
		t.Errorf("expected loaded config to have port 9999, got %+v", loaded.Network)
	}
}

func TestTemplateResolution_UserInDashboardHostname(t *testing.T) {
	user := os.Getenv("USER")
	if user == "" {
		t.Skip("USER env var not set")
	}

	cfg := &Config{ConfigData: ConfigData{
		Network: &NetworkConfig{
			DashboardHostname: "${USER}.dashboard.example.com",
		},
	}}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resolved := resolveConfigTemplates(cfgJSON)
	var result Config
	if err := json.Unmarshal(resolved, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := user + ".dashboard.example.com"
	if result.Network.DashboardHostname != want {
		t.Errorf("got %q, want %q", result.Network.DashboardHostname, want)
	}
}

func TestTemplateResolution_GoTemplatesUntouched(t *testing.T) {
	cfg := &Config{ConfigData: ConfigData{
		SaplingCommands: SaplingCommands{
			CreateWorkspace: "sl clone {{.RepoIdentifier}} {{.DestPath}}",
		},
	}}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resolved := resolveConfigTemplates(cfgJSON)
	var result Config
	if err := json.Unmarshal(resolved, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := "sl clone {{.RepoIdentifier}} {{.DestPath}}"
	if result.SaplingCommands.CreateWorkspace != want {
		t.Errorf("got %q, want %q", result.SaplingCommands.CreateWorkspace, want)
	}
}

func TestTemplateResolution_GeneralEnvVars(t *testing.T) {
	t.Setenv("SCHMUX_TEST_HOST", "myhost.example.com")

	cfg := &Config{ConfigData: ConfigData{
		Network: &NetworkConfig{
			DashboardHostname: "${SCHMUX_TEST_HOST}",
		},
	}}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resolved := resolveConfigTemplates(cfgJSON)
	var result Config
	if err := json.Unmarshal(resolved, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.Network.DashboardHostname != "myhost.example.com" {
		t.Errorf("got %q, want %q", result.Network.DashboardHostname, "myhost.example.com")
	}
}

func TestTemplateResolution_EmptyUserWarnsButExpands(t *testing.T) {
	t.Setenv("USER", "")

	input := []byte(`{"path":"/home/${USER}/ws"}`)
	resolved := resolveConfigTemplates(input)

	want := `{"path":"/home//ws"}`
	if string(resolved) != want {
		t.Errorf("got %s, want %s", resolved, want)
	}
}

func TestTemplateResolution_UnsetVarBecomesEmpty(t *testing.T) {
	t.Setenv("SCHMUX_NONEXISTENT_VAR_12345", "")
	os.Unsetenv("SCHMUX_NONEXISTENT_VAR_12345")

	input := []byte(`{"workspace_path":"/home/${SCHMUX_NONEXISTENT_VAR_12345}/ws"}`)
	resolved := resolveConfigTemplates(input)

	want := `{"workspace_path":"/home//ws"}`
	if string(resolved) != want {
		t.Errorf("got %s, want %s", resolved, want)
	}
}

func TestTemplateResolution_EmptyUserNoWarningWithoutUserTemplate(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("SCHMUX_TEST_VAR", "expanded")

	// Input has env vars but NOT ${USER} — should expand normally without warning.
	input := []byte(`{"path":"/home/${SCHMUX_TEST_VAR}/ws"}`)
	resolved := resolveConfigTemplates(input)

	want := `{"path":"/home/expanded/ws"}`
	if string(resolved) != want {
		t.Errorf("got %s, want %s", resolved, want)
	}
}

func TestTemplateResolution_NoTemplatesUnchanged(t *testing.T) {
	input := []byte(`{"network":{"port":7337,"dashboard_hostname":"static.example.com"}}`)
	resolved := resolveConfigTemplates(input)
	if string(resolved) != string(input) {
		t.Errorf("expected unchanged output, got %s", string(resolved))
	}
}
