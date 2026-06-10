package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

func TestApplyBuildMonitor_ConvertsNameKeysToSlug(t *testing.T) {
	cfg := &config.Config{}
	req := &contracts.BuildMonitorConfig{
		Enabled: true,
		Repos: map[string]contracts.BuildMonitorRepoConfig{
			"My Repo": {Enabled: true, GitHubLogin: "octocat"},
		},
	}
	applyBuildMonitor(cfg, req)
	if _, ok := cfg.BuildMonitor.Repos["my-repo"]; !ok {
		t.Fatalf("expected slug key 'my-repo', got keys %v", cfg.BuildMonitor.Repos)
	}
	if cfg.BuildMonitor.Repos["my-repo"].GitHubLogin != "octocat" {
		t.Fatalf("expected GitHubLogin octocat, got %q", cfg.BuildMonitor.Repos["my-repo"].GitHubLogin)
	}
}

func TestApplyBuildMonitor_NilInput(t *testing.T) {
	cfg := &config.Config{}
	applyBuildMonitor(cfg, (*contracts.BuildMonitorConfig)(nil))
	if cfg.BuildMonitor != nil {
		t.Fatal("expected nil BuildMonitor after nil input")
	}
}
