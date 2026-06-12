package config

import "testing"

func TestGetBuildMonitorEnabled_DefaultFalse(t *testing.T) {
	c := &Config{}
	if c.GetBuildMonitorEnabled() {
		t.Fatal("expected false when BuildMonitor is nil")
	}
	c.ConfigData.BuildMonitor = &BuildMonitorConfig{Enabled: true}
	if !c.GetBuildMonitorEnabled() {
		t.Fatal("expected true when enabled")
	}
}

func TestGetBuildMonitorRepoEnabled_AbsentIsFalse(t *testing.T) {
	c := &Config{} // nil map
	if c.GetBuildMonitorRepoEnabled("foo") {
		t.Fatal("nil map must be false")
	}
	c.ConfigData.BuildMonitor = &BuildMonitorConfig{Repos: map[string]BuildMonitorRepoConfig{
		"foo": {Enabled: true, GitHubLogin: "octocat"},
	}}
	if !c.GetBuildMonitorRepoEnabled("foo") {
		t.Fatal("present+enabled must be true")
	}
	if c.GetBuildMonitorRepoEnabled("bar") {
		t.Fatal("absent key must be false")
	}
}

func TestGetBuildMonitorInterval(t *testing.T) {
	tests := []struct {
		name string
		cfg  *BuildMonitorConfig
		want int
	}{
		{name: "nil config defaults to 5", cfg: nil, want: 5},
		{name: "zero defaults to 5", cfg: &BuildMonitorConfig{}, want: 5},
		{name: "negative defaults to 5", cfg: &BuildMonitorConfig{Interval: -1}, want: 5},
		{name: "explicit value", cfg: &BuildMonitorConfig{Interval: 15}, want: 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{ConfigData: ConfigData{BuildMonitor: tt.cfg}}
			if got := c.GetBuildMonitorInterval(); got != tt.want {
				t.Errorf("GetBuildMonitorInterval() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetBuildMonitorRepo_CarriesLogin(t *testing.T) {
	c := &Config{ConfigData: ConfigData{BuildMonitor: &BuildMonitorConfig{Repos: map[string]BuildMonitorRepoConfig{
		"foo": {GitHubLogin: "octocat"},
	}}}}
	got, ok := c.GetBuildMonitorRepo("foo")
	if !ok || got.GitHubLogin != "octocat" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestGetBuildMonitorTarget(t *testing.T) {
	c := &Config{}
	if got := c.GetBuildMonitorTarget(); got != "" {
		t.Errorf("nil section: got %q", got)
	}
	c.ConfigData.BuildMonitor = &BuildMonitorConfig{Target: "  claude  "}
	if got := c.GetBuildMonitorTarget(); got != "claude" {
		t.Errorf("got %q, want trimmed claude", got)
	}
}

func TestGetBuildMonitorAutoWorkspace(t *testing.T) {
	c := &Config{}
	if c.GetBuildMonitorAutoWorkspace() {
		t.Error("nil section: want false")
	}
	c.ConfigData.BuildMonitor = &BuildMonitorConfig{AutoWorkspaceOnFirstFailure: true}
	if !c.GetBuildMonitorAutoWorkspace() {
		t.Error("want true")
	}
}
