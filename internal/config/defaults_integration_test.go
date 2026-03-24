package config

import (
	"encoding/json"
	"os"
	"testing"
)

// TestIntegrationBuildDefaultsFullPipeline exercises the complete config seeding
// flow: Go defaults → build defaults overlay → ${USER} template resolution.
// This simulates what CreateDefault does when build_defaults.json is embedded.
func TestIntegrationBuildDefaultsFullPipeline(t *testing.T) {
	user := os.Getenv("USER")
	if user == "" {
		t.Skip("USER env var not set")
	}

	// Step 1: Start with Go defaults (same as CreateDefault)
	cfg := CreateDefault(t.TempDir() + "/config.json")

	// Step 2: Simulate build defaults overlay (as if build_defaults.json were embedded)
	buildDefaults := map[string]json.RawMessage{
		"network": json.RawMessage(`{
			"port": 44102,
			"dashboard_hostname": "${USER}.sb.example.net"
		}`),
		"source_code_management": json.RawMessage(`"git-worktree"`),
		"repos": json.RawMessage(`[{
			"name": "myrepo",
			"url": "myrepo",
			"bare_path": "myrepo",
			"vcs": "sapling"
		}]`),
		"sapling_commands": json.RawMessage(`{
			"create_workspace": "fbclone {{.RepoIdentifier}} {{.DestPath}}"
		}`),
	}

	if err := overlayDefaults(cfg, buildDefaults); err != nil {
		t.Fatalf("overlayDefaults: %v", err)
	}

	// Step 3: Resolve templates (same as CreateDefault does after overlay)
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal for template resolution: %v", err)
	}
	resolved := resolveConfigTemplates(cfgJSON)
	if err := json.Unmarshal(resolved, cfg); err != nil {
		t.Fatalf("unmarshal after template resolution: %v", err)
	}

	// Verify: port set from build defaults
	if cfg.Network == nil {
		t.Fatal("expected Network to be set")
	}
	if cfg.Network.Port != 44102 {
		t.Errorf("port: got %d, want 44102", cfg.Network.Port)
	}

	// Verify: dashboard_hostname has ${USER} resolved
	wantHostname := user + ".sb.example.net"
	if cfg.Network.DashboardHostname != wantHostname {
		t.Errorf("dashboard_hostname: got %q, want %q", cfg.Network.DashboardHostname, wantHostname)
	}

	// Verify: GetDashboardURL composes correctly
	wantURL := "http://" + wantHostname + ":44102"
	if got := cfg.GetDashboardURL(); got != wantURL {
		t.Errorf("GetDashboardURL: got %q, want %q", got, wantURL)
	}

	// Verify: repos set from build defaults
	if len(cfg.Repos) != 1 {
		t.Fatalf("repos: got %d, want 1", len(cfg.Repos))
	}
	if cfg.Repos[0].Name != "myrepo" {
		t.Errorf("repo name: got %q, want %q", cfg.Repos[0].Name, "myrepo")
	}

	// Verify: Go template syntax in sapling_commands is NOT resolved
	if cfg.SaplingCommands.CreateWorkspace != "fbclone {{.RepoIdentifier}} {{.DestPath}}" {
		t.Errorf("sapling create_workspace was incorrectly modified: %q", cfg.SaplingCommands.CreateWorkspace)
	}

	// Verify: SCM set from build defaults
	if cfg.SourceCodeManagement != "git-worktree" {
		t.Errorf("source_code_management: got %q, want %q", cfg.SourceCodeManagement, "git-worktree")
	}
}

// TestIntegrationNoBuildDefaultsPreservesGoDefaults verifies that when no
// build defaults are provided, the pipeline produces clean Go defaults.
func TestIntegrationNoBuildDefaultsPreservesGoDefaults(t *testing.T) {
	cfg := CreateDefault(t.TempDir() + "/config.json")

	// With no embedded build_defaults.json, CreateDefault returns Go defaults.
	if cfg.Network != nil {
		t.Errorf("expected nil Network, got %+v", cfg.Network)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("expected empty Repos, got %d", len(cfg.Repos))
	}
	// Default port getter returns 7337
	if cfg.GetPort() != 7337 {
		t.Errorf("default port: got %d, want 7337", cfg.GetPort())
	}
}
