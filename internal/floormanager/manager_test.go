package floormanager

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func tmuxAvailable() bool {
	return exec.Command("tmux", "-V").Run() == nil
}

func TestManagerWritesInstructionFiles(t *testing.T) {
	tmpDir := t.TempDir()
	m := &Manager{
		workDir:   tmpDir,
		schmuxBin: "/test/bin/schmux",
	}

	if err := m.writeInstructionFiles(); err != nil {
		t.Fatal(err)
	}

	// Check CLAUDE.md exists and has content
	claudeMd, err := os.ReadFile(filepath.Join(tmpDir, "CLAUDE.md"))
	if err != nil {
		t.Fatal("CLAUDE.md not written:", err)
	}
	if len(claudeMd) == 0 {
		t.Error("CLAUDE.md is empty")
	}

	// Check AGENTS.md is identical
	agentsMd, err := os.ReadFile(filepath.Join(tmpDir, "AGENTS.md"))
	if err != nil {
		t.Fatal("AGENTS.md not written:", err)
	}
	if string(claudeMd) != string(agentsMd) {
		t.Error("CLAUDE.md and AGENTS.md should have identical content")
	}

	// Check .claude/settings.json exists
	settings, err := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal("settings.json not written:", err)
	}
	if len(settings) == 0 {
		t.Error("settings.json is empty")
	}

	// Check memory.md is NOT overwritten if it exists
	memPath := filepath.Join(tmpDir, "memory.md")
	if err := os.WriteFile(memPath, []byte("existing memory"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.writeInstructionFiles(); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(memPath)
	if string(content) != "existing memory" {
		t.Error("memory.md was overwritten")
	}
}

func TestManagerInjectionCount(t *testing.T) {
	m := &Manager{}

	m.IncrementInjectionCount(5)
	if m.InjectionCount() != 5 {
		t.Errorf("expected 5, got %d", m.InjectionCount())
	}

	m.IncrementInjectionCount(3)
	if m.InjectionCount() != 8 {
		t.Errorf("expected 8, got %d", m.InjectionCount())
	}

	m.ResetInjectionCount()
	if m.InjectionCount() != 0 {
		t.Errorf("expected 0 after reset, got %d", m.InjectionCount())
	}
}

// newTestFMManager creates a Manager with a session manager that can resolve targets.
func newTestFMManager(t *testing.T, fmTarget string) *Manager {
	t.Helper()
	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.FloorManager = &config.FloorManagerConfig{
		Target: fmTarget,
	}
	st := state.New("", nil)
	statePath := filepath.Join(t.TempDir(), "state.json")
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))

	return &Manager{
		cfg:         cfg,
		sm:          sm,
		logger:      log.NewWithOptions(io.Discard, log.Options{}),
		workDir:     t.TempDir(),
		sessionName: "schmux-fm-test",
		schmuxBin:   "/test/schmux",
		stopCh:      make(chan struct{}),
	}
}

func TestResolveTarget(t *testing.T) {
	t.Run("no target configured", func(t *testing.T) {
		m := newTestFMManager(t, "")

		_, err := m.resolveTarget(context.Background())
		if err == nil {
			t.Fatal("expected error when no target configured")
		}
		if !strings.Contains(err.Error(), "no floor manager target") {
			t.Errorf("expected 'no floor manager target' error, got: %v", err)
		}
	})

	t.Run("unknown target", func(t *testing.T) {
		m := newTestFMManager(t, "nonexistent-tool-xyz")

		_, err := m.resolveTarget(context.Background())
		if err == nil {
			t.Fatal("expected error for unknown target")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %v", err)
		}
	})

	t.Run("builtin tool resolves", func(t *testing.T) {
		m := newTestFMManager(t, "claude")

		resolved, err := m.resolveTarget(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.Command != "claude" {
			t.Errorf("expected command 'claude', got %q", resolved.Command)
		}
		if !resolved.Promptable {
			t.Error("claude should be promptable")
		}
		if resolved.ToolName != "claude" {
			t.Errorf("expected ToolName 'claude', got %q", resolved.ToolName)
		}
	})
}

func TestBuildFMCommand(t *testing.T) {
	t.Run("with prompt", func(t *testing.T) {
		m := newTestFMManager(t, "claude")

		cmd, err := m.buildFMCommand(context.Background(), "manage my agents")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(cmd, "claude") {
			t.Errorf("command should contain 'claude', got %q", cmd)
		}
		if !strings.Contains(cmd, "'manage my agents'") {
			t.Errorf("command should contain quoted prompt, got %q", cmd)
		}
		if !strings.Contains(cmd, "SCHMUX_ENABLED='1'") {
			t.Errorf("command should contain SCHMUX_ENABLED, got %q", cmd)
		}
		if !strings.Contains(cmd, "SCHMUX_SESSION_ID='floor-manager'") {
			t.Errorf("command should contain SCHMUX_SESSION_ID, got %q", cmd)
		}
	})

	t.Run("empty prompt still produces valid command", func(t *testing.T) {
		m := newTestFMManager(t, "claude")

		cmd, err := m.buildFMCommand(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(cmd, "claude") {
			t.Errorf("command should contain 'claude', got %q", cmd)
		}
		// Should NOT contain a quoted empty string as an argument
		if strings.Contains(cmd, "''") {
			t.Errorf("command should not contain empty quoted arg, got %q", cmd)
		}
	})

	t.Run("no target configured returns error", func(t *testing.T) {
		m := newTestFMManager(t, "")

		_, err := m.buildFMCommand(context.Background(), "test")
		if err == nil {
			t.Fatal("expected error when no target configured")
		}
	})
}

func TestBuildFMResumeCommand(t *testing.T) {
	t.Run("claude resume", func(t *testing.T) {
		m := newTestFMManager(t, "claude")

		cmd, err := m.buildFMResumeCommand(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(cmd, "claude") {
			t.Errorf("resume command should contain 'claude', got %q", cmd)
		}
		if !strings.Contains(cmd, "--continue") {
			t.Errorf("resume command should contain '--continue', got %q", cmd)
		}
		if !strings.Contains(cmd, "SCHMUX_ENABLED='1'") {
			t.Errorf("resume command should contain SCHMUX_ENABLED, got %q", cmd)
		}
	})

	t.Run("no target returns error", func(t *testing.T) {
		m := newTestFMManager(t, "")

		_, err := m.buildFMResumeCommand(context.Background())
		if err == nil {
			t.Fatal("expected error when no target configured")
		}
	})
}

func TestFMCommandsAppendRunnerArgs(t *testing.T) {
	dir := t.TempDir()
	schmuxdir.Set(dir)
	t.Cleanup(func() { schmuxdir.Set("") })
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(dir, "cache", "codex-models-zai.json")
	if err := os.WriteFile(catalog, []byte(`{"models":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"),
		[]byte(`{"models":{},"providers":{"zai":{"ANTHROPIC_AUTH_TOKEN":"test-key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	glm := detect.Model{
		ID: "glm-5.3", DisplayName: "GLM-5.3", Provider: "zai",
		Runners: map[string]detect.RunnerSpec{
			"claude": {ModelValue: "glm-5.3", Endpoint: "https://api.z.ai/api/anthropic", RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"}},
			"codex":  {ModelValue: "glm-5.3", Endpoint: "https://api.z.ai/api/v1", RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"}},
		},
	}
	tools := []detect.Tool{{Name: "codex", Command: "codex"}, {Name: "claude", Command: "claude"}}
	logger := log.NewWithOptions(io.Discard, log.Options{})
	mm := models.New(&config.Config{ConfigData: config.ConfigData{Models: &config.ModelsConfig{Enabled: map[string]string{"glm-5.3": "codex"}}}}, tools, dir, logger)
	mm.SetRegistryModels([]detect.Model{glm})

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.FloorManager = &config.FloorManagerConfig{Target: "glm-5.3"}
	st := state.New("", nil)
	statePath := filepath.Join(t.TempDir(), "state.json")
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, nil, logger)
	sm.SetModelManager(mm)

	m := &Manager{
		cfg:         cfg,
		sm:          sm,
		logger:      logger,
		workDir:     t.TempDir(),
		sessionName: "schmux-fm-test",
		schmuxBin:   "/test/schmux",
		stopCh:      make(chan struct{}),
	}

	cmd, err := m.buildFMCommand(context.Background(), "manage")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-m", "glm-5.3", "model_provider=zai", "model_catalog_json=" + catalog} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("FM cmd %q lacks %q", cmd, want)
		}
	}

	rcmd, err := m.buildFMResumeCommand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"resume --last", "model_provider=zai", "model_catalog_json=" + catalog} {
		if !strings.Contains(rcmd, want) {
			t.Fatalf("FM resume cmd %q lacks %q", rcmd, want)
		}
	}
}

func TestResolveSessionName(t *testing.T) {
	t.Run("nil session manager returns ID", func(t *testing.T) {
		m := &Manager{sm: nil}
		got := m.resolveSessionName("sess-123")
		if got != "sess-123" {
			t.Errorf("expected 'sess-123', got %q", got)
		}
	})

	t.Run("session not found returns ID", func(t *testing.T) {
		m := newTestFMManager(t, "claude")
		got := m.resolveSessionName("nonexistent-session")
		if got != "nonexistent-session" {
			t.Errorf("expected 'nonexistent-session', got %q", got)
		}
	})

	t.Run("session with nickname returns nickname", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.WorkspacePath = t.TempDir()
		st := state.New("", nil)
		statePath := filepath.Join(t.TempDir(), "state.json")
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))

		st.AddSession(state.Session{ID: "sess-nick", Nickname: "my-agent"})

		m := &Manager{sm: sm}
		got := m.resolveSessionName("sess-nick")
		if got != "my-agent" {
			t.Errorf("expected 'my-agent', got %q", got)
		}
	})

	t.Run("session with empty nickname returns ID", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.WorkspacePath = t.TempDir()
		st := state.New("", nil)
		statePath := filepath.Join(t.TempDir(), "state.json")
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))

		st.AddSession(state.Session{ID: "sess-no-nick", Nickname: ""})

		m := &Manager{sm: sm}
		got := m.resolveSessionName("sess-no-nick")
		if got != "sess-no-nick" {
			t.Errorf("expected 'sess-no-nick', got %q", got)
		}
	})
}

func TestNew(t *testing.T) {
	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	st := state.New("", nil)
	statePath := filepath.Join(t.TempDir(), "state.json")
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))

	homeDir := t.TempDir()
	schmuxHome := filepath.Join(homeDir, ".schmux")
	schmuxdir.Set(schmuxHome)

	m := New(cfg, sm, nil, homeDir, log.NewWithOptions(io.Discard, log.Options{}))

	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.sessionName != tmuxSessionName {
		t.Errorf("expected session name %q, got %q", tmuxSessionName, m.sessionName)
	}
	expectedWorkDir := filepath.Join(schmuxHome, "floor-manager")
	if m.workDir != expectedWorkDir {
		t.Errorf("expected workDir %q, got %q", expectedWorkDir, m.workDir)
	}
	if m.stopCh == nil {
		t.Error("stopCh should be initialized")
	}
	if m.Running() {
		t.Error("new manager should not be running")
	}
}

func TestRunning_NoSession(t *testing.T) {
	m := &Manager{}

	if m.Running() {
		t.Error("should not be running without tmux session set")
	}
}

func TestRunning_NonexistentTmux(t *testing.T) {
	m := &Manager{}

	m.mu.Lock()
	m.tmuxSession = "schmux-nonexistent-fm-test"
	m.mu.Unlock()

	// Running() checks tmux.SessionExists, which returns false for nonexistent sessions
	if m.Running() {
		t.Error("should not be running when tmux session doesn't exist")
	}
}

func TestTmuxSession(t *testing.T) {
	m := &Manager{}

	if m.TmuxSession() != "" {
		t.Error("TmuxSession should be empty initially")
	}

	m.mu.Lock()
	m.tmuxSession = "test-session"
	m.mu.Unlock()

	if m.TmuxSession() != "test-session" {
		t.Errorf("expected 'test-session', got %q", m.TmuxSession())
	}
}
