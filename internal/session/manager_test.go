package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		RunTargets: []config.RunTarget{
			{Name: "test", Type: config.RunTargetTypePromptable, Command: "test"},
		},
	}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)
	if m == nil {
		t.Error("New() returned nil")
	}
	if m.config != cfg {
		t.Error("config not set correctly")
	}
	if m.state != st {
		t.Error("state not set correctly")
	}
	if m.workspace != wm {
		t.Error("workspace manager not set correctly")
	}
}

func TestGetAttachCommand(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-001",
		WorkspaceID: "test-001",
		Target:      "test",
		TmuxSession: "schmux-test-001-abc123",
	}

	st.AddSession(sess)

	cmd, err := m.GetAttachCommand("session-001")
	if err != nil {
		t.Errorf("GetAttachCommand() error = %v", err)
	}

	expected := `tmux attach -t "=schmux-test-001-abc123"`
	if cmd != expected {
		t.Errorf("expected %s, got %s", expected, cmd)
	}
}

func TestGetAttachCommandNotFound(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	_, err := m.GetAttachCommand("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestGetAllSessions(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	// Create fresh state for test isolation
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add test sessions
	sessions := []state.Session{
		{ID: "s1", WorkspaceID: "w1", Target: "a1", TmuxSession: "t1"},
		{ID: "s2", WorkspaceID: "w2", Target: "a2", TmuxSession: "t2"},
	}

	for _, sess := range sessions {
		st.AddSession(sess)
	}

	all := m.GetAllSessions()
	if len(all) != len(sessions) {
		t.Errorf("expected %d sessions, got %d", len(sessions), len(all))
	}
}

func TestGetSession(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-002",
		WorkspaceID: "test-002",
		Target:      "test",
		TmuxSession: "schmux-test-002-def456",
	}

	st.AddSession(sess)

	retrieved, err := m.GetSession("session-002")
	if err != nil {
		t.Errorf("GetSession() error = %v", err)
	}

	if retrieved.ID != sess.ID {
		t.Errorf("expected ID %s, got %s", sess.ID, retrieved.ID)
	}

	_, err = m.GetSession("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestIsRunning(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	t.Run("returns false for nonexistent session", func(t *testing.T) {
		running := m.IsRunning(context.Background(), "nonexistent")
		if running {
			t.Error("expected false for nonexistent session")
		}
	})

	t.Run("returns false for session with no PID and no tmux", func(t *testing.T) {
		sessNoPid := state.Session{
			ID:          "session-nopid",
			WorkspaceID: "test-nopid",
			Target:      "test",
			TmuxSession: "nonexistent-tmux-session",
			Pid:         0,
		}
		st.AddSession(sessNoPid)

		running := m.IsRunning(context.Background(), "session-nopid")
		if running {
			t.Error("expected false for session with no PID and no tmux")
		}
	})
}

func TestGetOutput(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		_, err := m.GetOutput(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "string with single quote",
			input:    "don't",
			expected: "'don'\\''t'",
		},
		{
			name:     "string with multiple single quotes",
			input:    "it's a 'test'",
			expected: "'it'\\''s a '\\''test'\\'''",
		},
		{
			name:     "string with newline",
			input:    "hello\nworld",
			expected: "'hello\nworld'",
		},
		{
			name:     "string with newline and single quote",
			input:    "hello\nit's me",
			expected: "'hello\nit'\\''s me'",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "string with backslash",
			input:    "path\\to\\file",
			expected: "'path\\to\\file'",
		},
		{
			name:     "string with double quotes",
			input:    `say "hello"`,
			expected: `'say "hello"'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.expected {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeNickname(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "replaces dots with dashes",
			input:    "my.session",
			expected: "my-session",
		},
		{
			name:     "replaces colons with dashes",
			input:    "my:session",
			expected: "my-session",
		},
		{
			name:     "replaces both dots and colons",
			input:    "my.session:name",
			expected: "my-session-name",
		},
		{
			name:     "leaves valid characters unchanged",
			input:    "my-session_123",
			expected: "my-session_123",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeNickname(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeNickname(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRenameSession(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.RenameSession(context.Background(), "nonexistent", "new-name")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestDispose(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.Dispose(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestEnsurePipePane(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		Terminal:      &config.TerminalSize{Width: 80, Height: 24, SeedLines: 100},
	}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.EnsureTracker("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestPruneLogFiles(t *testing.T) {
	t.Run("prune with no active sessions", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create test log files
		if err := os.WriteFile(filepath.Join(tmpDir, "orphaned-session.log"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test log: %v", err)
		}

		removed := PruneLogFiles(tmpDir, map[string]bool{})
		if removed != 1 {
			t.Errorf("PruneLogFiles() removed = %d, want 1", removed)
		}

		// File should be gone
		if _, err := os.Stat(filepath.Join(tmpDir, "orphaned-session.log")); err == nil {
			t.Error("orphaned log file still exists (expected removal)")
		}
	})

	t.Run("prune keeps active session logs", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create log files: one active, one orphaned
		if err := os.WriteFile(filepath.Join(tmpDir, "active-session.log"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create active log: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "orphaned-session.log"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create orphaned log: %v", err)
		}

		removed := PruneLogFiles(tmpDir, map[string]bool{"active-session": true})
		if removed != 1 {
			t.Errorf("PruneLogFiles() removed = %d, want 1", removed)
		}

		// Active log should remain
		if _, err := os.Stat(filepath.Join(tmpDir, "active-session.log")); err != nil {
			t.Error("active session log was removed (expected to be kept)")
		}
		// Orphaned log should be gone
		if _, err := os.Stat(filepath.Join(tmpDir, "orphaned-session.log")); err == nil {
			t.Error("orphaned log file still exists (expected removal)")
		}
	})

	t.Run("prune skips non-log files", func(t *testing.T) {
		tmpDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create non-log file: %v", err)
		}

		removed := PruneLogFiles(tmpDir, map[string]bool{})
		if removed != 0 {
			t.Errorf("PruneLogFiles() removed = %d, want 0 (non-log files should be skipped)", removed)
		}
	})
}

func TestBuildCommand(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() failed: %v", err)
	}
	signalingFilePath := filepath.Join(homeDir, ".schmux", "signaling.md")

	tests := []struct {
		name             string
		target           ResolvedTarget
		prompt           string
		model            *detect.Model
		resume           bool
		wantErr          bool
		errContains      string
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name: "claude model with prompt",
			target: ResolvedTarget{
				Name:       "claude-sonnet",
				Kind:       TargetKindModel,
				Command:    "claude",
				Promptable: true,
				Env: map[string]string{
					"ANTHROPIC_MODEL": "claude-sonnet-4-5-20250929",
				},
			},
			prompt:  "hello world",
			model:   nil,
			resume:  false,
			wantErr: false,
			shouldContain: []string{
				"ANTHROPIC_MODEL='claude-sonnet-4-5-20250929'",
				"claude",
				"--append-system-prompt-file",
				shellQuote(signalingFilePath),
				"'hello world'",
			},
		},
		{
			name: "codex model with CLI flag",
			target: ResolvedTarget{
				Name:       "gpt-5.2-codex",
				Kind:       TargetKindModel,
				Command:    "codex",
				Promptable: true,
				Env:        map[string]string{}, // No ANTHROPIC_MODEL for Codex
			},
			prompt: "write a function",
			model: &detect.Model{
				ID:         "gpt-5.2-codex",
				BaseTool:   "codex",
				ModelValue: "gpt-5.2-codex",
				ModelFlag:  "-m",
			},
			resume:  false,
			wantErr: false,
			shouldContain: []string{
				"codex",
				"-m",
				"'gpt-5.2-codex'",
				"-c",
				shellQuote("model_instructions_file=" + signalingFilePath),
				"'write a function'",
			},
			shouldNotContain: []string{
				"ANTHROPIC_MODEL",
			},
		},
		{
			name: "codex model with CLI flag and env vars",
			target: ResolvedTarget{
				Name:       "gpt-5.3-codex",
				Kind:       TargetKindModel,
				Command:    "codex",
				Promptable: true,
				Env: map[string]string{
					"SOME_VAR": "value",
				},
			},
			prompt: "test prompt",
			model: &detect.Model{
				ID:         "gpt-5.3-codex",
				BaseTool:   "codex",
				ModelValue: "gpt-5.3-codex",
				ModelFlag:  "-m",
			},
			resume:  false,
			wantErr: false,
			shouldContain: []string{
				"SOME_VAR='value'",
				"codex",
				"-m",
				"'gpt-5.3-codex'",
				"-c",
				shellQuote("model_instructions_file=" + signalingFilePath),
				"'test prompt'",
			},
			shouldNotContain: []string{
				"ANTHROPIC_MODEL",
			},
		},
		{
			name: "non-promptable target without prompt",
			target: ResolvedTarget{
				Name:       "test-cmd",
				Kind:       TargetKindUser,
				Command:    "ls -la",
				Promptable: false,
				Env:        map[string]string{},
			},
			prompt:  "",
			model:   nil,
			resume:  false,
			wantErr: false,
			shouldContain: []string{
				"ls",
				"-la",
			},
		},
		{
			name: "promptable target without prompt returns error",
			target: ResolvedTarget{
				Name:       "claude",
				Kind:       TargetKindDetected,
				Command:    "claude",
				Promptable: true,
				Env:        map[string]string{},
			},
			prompt:      "",
			model:       nil,
			resume:      false,
			wantErr:     true,
			errContains: "prompt is required",
		},
		{
			name: "non-promptable target with prompt returns error",
			target: ResolvedTarget{
				Name:       "test-cmd",
				Kind:       TargetKindUser,
				Command:    "ls",
				Promptable: false,
				Env:        map[string]string{},
			},
			prompt:      "unexpected prompt",
			model:       nil,
			resume:      false,
			wantErr:     true,
			errContains: "prompt is not allowed",
		},
		{
			name: "resume mode with claude",
			target: ResolvedTarget{
				Name:       "claude",
				Kind:       TargetKindDetected,
				Command:    "claude",
				Promptable: true,
				Env:        map[string]string{},
			},
			prompt:  "",
			model:   nil,
			resume:  true,
			wantErr: false,
			shouldContain: []string{
				"claude",
				"--continue",
				"--append-system-prompt-file",
				shellQuote(signalingFilePath),
			},
		},
		{
			name: "resume mode with claude and model env vars",
			target: ResolvedTarget{
				Name:       "claude-opus",
				Kind:       TargetKindModel,
				Command:    "claude",
				Promptable: true,
				Env: map[string]string{
					"ANTHROPIC_MODEL": "claude-opus-4-5-20251101",
				},
			},
			prompt: "",
			model: &detect.Model{
				ID:         "claude-opus",
				BaseTool:   "claude",
				ModelValue: "claude-opus-4-5-20251101",
			},
			resume:  true,
			wantErr: false,
			shouldContain: []string{
				"ANTHROPIC_MODEL='claude-opus-4-5-20251101'",
				"claude",
				"--continue",
				"--append-system-prompt-file",
				shellQuote(signalingFilePath),
			},
		},
		{
			name: "resume mode with codex",
			target: ResolvedTarget{
				Name:       "codex",
				Kind:       TargetKindDetected,
				Command:    "codex",
				Promptable: true,
				Env:        map[string]string{},
			},
			prompt:  "",
			model:   nil,
			resume:  true,
			wantErr: false,
			shouldContain: []string{
				"codex",
				"resume",
				"--last",
				"-c",
				shellQuote("model_instructions_file=" + signalingFilePath),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCommand(tt.target, tt.prompt, tt.model, tt.resume)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("buildCommand() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("buildCommand() unexpected error: %v", err)
				return
			}

			// Check shouldContain
			for _, substr := range tt.shouldContain {
				if !strings.Contains(got, substr) {
					t.Errorf("buildCommand() = %q, should contain %q", got, substr)
				}
			}

			// Check shouldNotContain
			for _, substr := range tt.shouldNotContain {
				if strings.Contains(got, substr) {
					t.Errorf("buildCommand() = %q, should not contain %q", got, substr)
				}
			}
		})
	}
}

func TestGetTrackerAndEnsureTracker(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	t.Run("GetTracker returns error for missing session", func(t *testing.T) {
		_, err := m.GetTracker("missing")
		if err == nil {
			t.Fatal("expected error for missing session")
		}
	})

	t.Run("EnsureTracker returns error for missing session", func(t *testing.T) {
		err := m.EnsureTracker("missing")
		if err == nil {
			t.Fatal("expected error for missing session")
		}
	})

	t.Run("GetTracker creates and reuses tracker", func(t *testing.T) {
		sess := state.Session{
			ID:          "session-tracker-1",
			WorkspaceID: "workspace-1",
			Target:      "test",
			TmuxSession: "tmux-tracker-1",
		}
		if err := st.AddSession(sess); err != nil {
			t.Fatalf("add session: %v", err)
		}

		tracker1, err := m.GetTracker(sess.ID)
		if err != nil {
			t.Fatalf("GetTracker first call: %v", err)
		}
		tracker2, err := m.GetTracker(sess.ID)
		if err != nil {
			t.Fatalf("GetTracker second call: %v", err)
		}
		if tracker1 != tracker2 {
			t.Fatalf("expected tracker reuse")
		}

		// Explicit cleanup so background goroutine does not leak in tests.
		m.stopTracker(sess.ID)
	})
}
