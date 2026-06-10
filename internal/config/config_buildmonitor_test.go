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

func TestGetBuildMonitorRepo_CarriesLogin(t *testing.T) {
	c := &Config{ConfigData: ConfigData{BuildMonitor: &BuildMonitorConfig{Repos: map[string]BuildMonitorRepoConfig{
		"foo": {GitHubLogin: "octocat"},
	}}}}
	got, ok := c.GetBuildMonitorRepo("foo")
	if !ok || got.GitHubLogin != "octocat" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}
