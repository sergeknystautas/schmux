package models

import (
	"os"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestIsTargetInUse_CountsAllChainEntries(t *testing.T) {
	cfg := &config.Config{}
	cfg.Nudgenik = &config.NudgenikConfig{Targets: []string{"MiniMax-M3::api"}}
	cfg.BranchSuggest = &config.BranchSuggestConfig{Targets: []string{"GLM-5.3", "GLM-5.3::api"}}
	// Constructor pattern from manager_lastchecked_test.go:20. No build tag
	// needed — New and IsTargetInUse are untagged in manager.go.
	m := New(cfg, nil, t.TempDir(), log.New(os.Stderr))

	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"primary with api suffix", "MiniMax-M3", true},
		{"bare fallback entry", "GLM-5.3", true},
		{"unrelated model", "kimi-for-coding", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := m.IsTargetInUse(tc.target); got != tc.want {
			t.Errorf("%s: IsTargetInUse(%q) = %v, want %v", tc.name, tc.target, got, tc.want)
		}
	}
}
