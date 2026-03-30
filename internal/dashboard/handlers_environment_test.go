package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestCompareEnvironments(t *testing.T) {
	system := map[string]string{
		"PATH":    "/usr/local/bin:/usr/bin",
		"GOPATH":  "/home/user/go",
		"NVM_DIR": "/home/user/.nvm",
	}
	tmuxEnv := map[string]string{
		"PATH":       "/usr/bin",
		"GOPATH":     "/home/user/go",
		"LEGACY_VAR": "old",
	}
	vars := compareEnvironments(system, tmuxEnv)

	byKey := make(map[string]contracts.EnvironmentVar)
	for _, v := range vars {
		byKey[v.Key] = v
	}

	if byKey["PATH"].Status != "differs" {
		t.Errorf("PATH: expected differs, got %s", byKey["PATH"].Status)
	}
	if byKey["GOPATH"].Status != "in_sync" {
		t.Errorf("GOPATH: expected in_sync, got %s", byKey["GOPATH"].Status)
	}
	if byKey["NVM_DIR"].Status != "system_only" {
		t.Errorf("NVM_DIR: expected system_only, got %s", byKey["NVM_DIR"].Status)
	}
	if byKey["LEGACY_VAR"].Status != "tmux_only" {
		t.Errorf("LEGACY_VAR: expected tmux_only, got %s", byKey["LEGACY_VAR"].Status)
	}
}
