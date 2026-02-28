package session

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		RunTargets: []config.RunTarget{
			{Name: "test", Type: config.RunTargetTypePromptable, Command: "test"},
		},
	}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	t.Run("initializes all internal state", func(t *testing.T) {
		m := New(cfg, st, statePath, wm, nil)
		if m == nil {
			t.Fatal("New() returned nil")
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
		if m.ensurer == nil {
			t.Error("ensurer should be initialized by New()")
		}
		if m.trackers == nil {
			t.Error("trackers map should be initialized by New()")
		}
		if m.remoteDetectors == nil {
			t.Error("remoteDetectors map should be initialized by New()")
		}
	})

	t.Run("creates default logger when nil is passed", func(t *testing.T) {
		m := New(cfg, st, statePath, wm, nil)
		if m.logger == nil {
			t.Fatal("logger should be non-nil even when nil is passed to New()")
		}
	})

	t.Run("uses provided logger", func(t *testing.T) {
		customLogger := log.NewWithOptions(io.Discard, log.Options{})
		m := New(cfg, st, statePath, wm, customLogger)
		if m.logger != customLogger {
			t.Error("should use the provided logger, not create a new one")
		}
	})
}

func TestGetAttachCommand(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

	_, err := m.GetAttachCommand("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestGetAllSessions(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	// Create fresh state for test isolation
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		_, err := m.GetOutput(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.RenameSession(context.Background(), "nonexistent", "new-name")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestDispose(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

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
	}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

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
		remote           bool
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
				"'hello world'",
			},
			shouldNotContain: []string{
				"--append-system-prompt-file", // Claude uses hooks, not prompt injection
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
				shellutil.Quote("model_instructions_file=" + signalingFilePath),
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
				shellutil.Quote("model_instructions_file=" + signalingFilePath),
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
			},
			shouldNotContain: []string{
				"--append-system-prompt-file", // Claude uses hooks, not prompt injection
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
			},
			shouldNotContain: []string{
				"--append-system-prompt-file", // Claude uses hooks, not prompt injection
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
				shellutil.Quote("model_instructions_file=" + signalingFilePath),
			},
		},
		{
			name: "remote mode claude uses hooks not prompt injection",
			target: ResolvedTarget{
				Name:       "claude",
				Kind:       TargetKindDetected,
				Command:    "claude",
				Promptable: true,
				Env:        map[string]string{},
			},
			prompt:  "fix the bug",
			model:   nil,
			resume:  false,
			remote:  true,
			wantErr: false,
			shouldContain: []string{
				"claude",
				"'fix the bug'",
			},
			shouldNotContain: []string{
				"--append-system-prompt", // Claude uses hooks, not inline prompt injection
				"--append-system-prompt-file",
				signalingFilePath,
			},
		},
		{
			name: "remote mode codex skips file-based injection",
			target: ResolvedTarget{
				Name:       "codex",
				Kind:       TargetKindDetected,
				Command:    "codex",
				Promptable: true,
				Env:        map[string]string{},
			},
			prompt:  "write tests",
			model:   nil,
			resume:  false,
			remote:  true,
			wantErr: false,
			shouldContain: []string{
				"codex",
				"'write tests'",
			},
			shouldNotContain: []string{
				"model_instructions_file",
				signalingFilePath,
			},
		},
		{
			name: "remote mode claude with env vars uses hooks",
			target: ResolvedTarget{
				Name:       "claude-opus",
				Kind:       TargetKindModel,
				Command:    "claude",
				Promptable: true,
				Env: map[string]string{
					"SCHMUX_ENABLED":    "1",
					"SCHMUX_SESSION_ID": "remote-test-123",
				},
			},
			prompt:  "deploy",
			model:   nil,
			resume:  false,
			remote:  true,
			wantErr: false,
			shouldContain: []string{
				"SCHMUX_ENABLED='1'",
				"SCHMUX_SESSION_ID='remote-test-123'",
				"claude",
				"'deploy'",
			},
			shouldNotContain: []string{
				"--append-system-prompt", // Claude uses hooks, not prompt injection
				"--append-system-prompt-file",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCommand(tt.target, tt.prompt, tt.model, tt.resume, tt.remote)
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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

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

func TestSetTerminalCaptureCallback(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	m := New(cfg, st, statePath, wm, nil)

	var capturedSessionID, capturedWorkspaceID, capturedOutput string
	m.SetTerminalCaptureCallback(func(sessionID, workspaceID, output string) {
		capturedSessionID = sessionID
		capturedWorkspaceID = workspaceID
		capturedOutput = output
	})

	// Verify callback is set
	if m.terminalCaptureCallback == nil {
		t.Fatal("terminalCaptureCallback should not be nil")
	}

	// Invoke it
	m.terminalCaptureCallback("s1", "w1", "hello world")
	if capturedSessionID != "s1" || capturedWorkspaceID != "w1" || capturedOutput != "hello world" {
		t.Errorf("callback received wrong values: %q, %q, %q", capturedSessionID, capturedWorkspaceID, capturedOutput)
	}
}

func TestMergeEnvMaps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		base      map[string]string
		overrides map[string]string
		want      map[string]string
	}{
		{
			name:      "both nil returns nil",
			base:      nil,
			overrides: nil,
			want:      nil,
		},
		{
			name:      "nil base with overrides",
			base:      nil,
			overrides: map[string]string{"A": "1"},
			want:      map[string]string{"A": "1"},
		},
		{
			name:      "base with nil overrides",
			base:      map[string]string{"A": "1"},
			overrides: nil,
			want:      map[string]string{"A": "1"},
		},
		{
			name:      "overrides take precedence",
			base:      map[string]string{"A": "old", "B": "keep"},
			overrides: map[string]string{"A": "new", "C": "added"},
			want:      map[string]string{"A": "new", "B": "keep", "C": "added"},
		},
		{
			name:      "empty maps return empty (not nil)",
			base:      map[string]string{},
			overrides: map[string]string{},
			want:      map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeEnvMaps(tt.base, tt.overrides)
			if tt.want == nil {
				if got != nil {
					t.Errorf("mergeEnvMaps() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("mergeEnvMaps() has %d entries, want %d", len(got), len(tt.want))
			}
			for k, wantV := range tt.want {
				if gotV, ok := got[k]; !ok || gotV != wantV {
					t.Errorf("mergeEnvMaps()[%q] = %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

func TestBuildEnvPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "single var",
			env:  map[string]string{"FOO": "bar"},
			want: "FOO='bar'",
		},
		{
			name: "multiple vars sorted alphabetically",
			env:  map[string]string{"Z_VAR": "z", "A_VAR": "a"},
			want: "A_VAR='a' Z_VAR='z'",
		},
		{
			name: "empty map",
			env:  map[string]string{},
			want: "",
		},
		{
			name: "value with spaces is quoted",
			env:  map[string]string{"MSG": "hello world"},
			want: "MSG='hello world'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEnvPrefix(tt.env)
			if got != tt.want {
				t.Errorf("buildEnvPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNicknameExists(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil)

	// Add sessions with known tmux names
	st.AddSession(state.Session{ID: "s1", TmuxSession: "my-session"})
	st.AddSession(state.Session{ID: "s2", TmuxSession: "other-session"})

	tests := []struct {
		name      string
		nickname  string
		excludeID string
		wantID    string
	}{
		{"finds existing by tmux name", "my-session", "", "s1"},
		{"finds other session", "other-session", "", "s2"},
		{"sanitized match (dots to dashes)", "my.session", "", "s1"},
		{"empty nickname returns empty", "", "", ""},
		{"no match returns empty", "nonexistent", "", ""},
		{"excludes specified session", "my-session", "s1", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.nicknameExists(tt.nickname, tt.excludeID)
			if got != tt.wantID {
				t.Errorf("nicknameExists(%q, %q) = %q, want %q", tt.nickname, tt.excludeID, got, tt.wantID)
			}
		})
	}
}

func TestGenerateUniqueNickname(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil)

	t.Run("empty returns empty", func(t *testing.T) {
		got := m.generateUniqueNickname("")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("unique name returned as-is", func(t *testing.T) {
		got := m.generateUniqueNickname("fresh-name")
		if got != "fresh-name" {
			t.Errorf("expected 'fresh-name', got %q", got)
		}
	})

	t.Run("conflict appends number suffix", func(t *testing.T) {
		// Register a session with this tmux name
		st.AddSession(state.Session{ID: "s1", TmuxSession: "taken"})

		got := m.generateUniqueNickname("taken")
		if got != "taken (1)" {
			t.Errorf("expected 'taken (1)', got %q", got)
		}
	})

	t.Run("multiple conflicts increment suffix", func(t *testing.T) {
		// "taken" is already registered from previous subtest.
		// Register "taken (1)" as well.
		st.AddSession(state.Session{ID: "s2", TmuxSession: sanitizeNickname("taken (1)")})

		got := m.generateUniqueNickname("taken")
		if got != "taken (2)" {
			t.Errorf("expected 'taken (2)', got %q", got)
		}
	})
}

func TestAppendPersonaFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		cmd              string
		baseTool         string
		personaFilePath  string
		shouldContain    string
		shouldNotContain string
	}{
		{
			name:            "claude appends system prompt flag",
			cmd:             "claude --dangerously-skip-permissions",
			baseTool:        "claude",
			personaFilePath: "/home/user/.schmux/persona.md",
			shouldContain:   "--append-system-prompt-file",
		},
		{
			name:             "codex returns cmd unchanged",
			cmd:              "codex -c 'instructions'",
			baseTool:         "codex",
			personaFilePath:  "/home/user/.schmux/persona.md",
			shouldNotContain: "--append-system-prompt-file",
		},
		{
			name:             "gemini returns cmd unchanged",
			cmd:              "gemini",
			baseTool:         "gemini",
			personaFilePath:  "/tmp/persona.md",
			shouldNotContain: "--append-system-prompt-file",
		},
		{
			name:            "claude persona path with spaces is quoted",
			cmd:             "claude",
			baseTool:        "claude",
			personaFilePath: "/home/user/my project/persona.md",
			shouldContain:   "'/home/user/my project/persona.md'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendPersonaFlags(tt.cmd, tt.baseTool, tt.personaFilePath)

			if tt.shouldContain != "" && !strings.Contains(got, tt.shouldContain) {
				t.Errorf("appendPersonaFlags() = %q, should contain %q", got, tt.shouldContain)
			}
			if tt.shouldNotContain != "" && strings.Contains(got, tt.shouldNotContain) {
				t.Errorf("appendPersonaFlags() = %q, should NOT contain %q", got, tt.shouldNotContain)
			}

			// All results should start with the original command
			if !strings.HasPrefix(got, tt.cmd) {
				t.Errorf("appendPersonaFlags() = %q, should start with original cmd %q", got, tt.cmd)
			}
		})
	}
}
