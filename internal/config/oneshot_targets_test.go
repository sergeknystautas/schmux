package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMigratesTargetStringToTargetsList(t *testing.T) {
	path := writeTempConfig(t, `{
		"nudgenik": {"target": "MiniMax-M3::api", "viewed_buffer_ms": 100},
		"branch_suggest": {"target": "GLM-5.3::api"}
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.GetNudgenikTargets(); len(got) != 1 || got[0] != "MiniMax-M3::api" {
		t.Errorf("GetNudgenikTargets() = %v, want [MiniMax-M3::api]", got)
	}
	if got := cfg.GetNudgenikViewedBufferMs(); got != 100 {
		t.Errorf("viewed_buffer_ms lost in migration: %d", got)
	}
	if got := cfg.GetBranchSuggestTargets(); len(got) != 1 || got[0] != "GLM-5.3::api" {
		t.Errorf("GetBranchSuggestTargets() = %v, want [GLM-5.3::api]", got)
	}
}

func TestLoadBlankTargetStaysDisabled(t *testing.T) {
	// A blank legacy value must NOT become targets: [""] — that would fail
	// entry validation and block startup (spec: Configuration).
	path := writeTempConfig(t, `{"branch_suggest": {"target": "   "}}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.GetBranchSuggestTargets(); len(got) != 0 {
		t.Errorf("GetBranchSuggestTargets() = %v, want empty", got)
	}
}

func TestLoadPreservesExplicitTargetsList(t *testing.T) {
	path := writeTempConfig(t, `{"branch_suggest": {"targets": ["A::api", "B::api"]}}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.GetBranchSuggestTargets()
	if len(got) != 2 || got[0] != "A::api" || got[1] != "B::api" {
		t.Errorf("GetBranchSuggestTargets() = %v, want [A::api B::api]", got)
	}
}

func TestLoadLegacyFileStableAcrossLoads(t *testing.T) {
	// Loading the same legacy file twice yields the same one-entry chain —
	// whether or not the first Load rewrote the file, no duplication.
	path := writeTempConfig(t, `{"branch_suggest": {"target": "A::api"}}`)
	for i := 0; i < 2; i++ {
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load #%d: %v", i+1, err)
		}
		if got := cfg.GetBranchSuggestTargets(); len(got) != 1 || got[0] != "A::api" {
			t.Fatalf("Load #%d: GetBranchSuggestTargets() = %v, want [A::api]", i+1, got)
		}
	}
}

func TestGetTargetsTrimsAndDropsBlanks(t *testing.T) {
	cfg := &Config{ConfigData: ConfigData{BranchSuggest: &BranchSuggestConfig{
		Targets: []string{"  A::api ", "", "B::api"},
	}}}
	got := cfg.GetBranchSuggestTargets()
	if len(got) != 2 || got[0] != "A::api" || got[1] != "B::api" {
		t.Errorf("GetBranchSuggestTargets() = %v, want [A::api B::api]", got)
	}
	// Singular getter returns the primary.
	if got := cfg.GetBranchSuggestTarget(); got != "A::api" {
		t.Errorf("GetBranchSuggestTarget() = %q, want A::api", got)
	}
	// Nil-safety.
	var nilCfg *Config
	if got := nilCfg.GetBranchSuggestTargets(); len(got) != 0 {
		t.Errorf("nil config should return empty, got %v", got)
	}
}

func TestMigrateLegacyModelIDsOverTargetsList(t *testing.T) {
	cfg := &Config{ConfigData: ConfigData{
		Nudgenik:      &NudgenikConfig{Targets: []string{"minimax-m2.1"}},
		BranchSuggest: &BranchSuggestConfig{Targets: []string{"minimax-m2.1", "opus"}},
	}}
	cfg.migrateModelIDs()
	if got := cfg.Nudgenik.Targets; len(got) != 1 || got[0] != "MiniMax-M2.1" {
		t.Errorf("Nudgenik.Targets = %v, want [MiniMax-M2.1]", got)
	}
	wantSecond := "claude-opus-4-6"
	gotBS := cfg.BranchSuggest.Targets
	if len(gotBS) != 2 || gotBS[0] != "MiniMax-M2.1" || gotBS[1] != wantSecond {
		t.Errorf("BranchSuggest.Targets = %v, want [MiniMax-M2.1 claude-opus-4-6]", gotBS)
	}
}

func TestValidateRejectsBlankTargetEntries(t *testing.T) {
	if err := validateNudgenikConfig(&NudgenikConfig{Targets: []string{"A::api", " "}}); err == nil {
		t.Error("blank nudgenik entry should fail validation")
	}
	if err := validateBranchSuggestConfig(&BranchSuggestConfig{Targets: []string{"A::api"}}); err != nil {
		t.Errorf("valid branch suggest targets should pass, got %v", err)
	}
	if err := validateBranchSuggestConfig(&BranchSuggestConfig{Targets: []string{""}}); err == nil {
		t.Error("blank branch suggest entry should fail validation")
	}
}
