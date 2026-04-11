package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/workspace"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// newTestManager creates a Manager with minimal config, ephemeral state, and a discard logger.
// Returns the manager and state for further test setup (e.g., adding sessions).
func newTestManager(t *testing.T) (*Manager, *state.State) {
	t.Helper()
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("", nil)
	statePath := filepath.Join(t.TempDir(), "state.json")
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)
	return m, st
}

func TestNew(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		RunTargets: []config.RunTarget{
			{Name: "test", Command: "test"},
		},
	}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	t.Run("initializes all internal state", func(t *testing.T) {
		m := New(cfg, st, statePath, wm, nil, nil)
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
		m := New(cfg, st, statePath, wm, nil, nil)
		if m.logger == nil {
			t.Fatal("logger should be non-nil even when nil is passed to New()")
		}
	})

	t.Run("uses provided logger", func(t *testing.T) {
		customLogger := log.NewWithOptions(io.Discard, log.Options{})
		m := New(cfg, st, statePath, wm, nil, customLogger)
		if m.logger != customLogger {
			t.Error("should use the provided logger, not create a new one")
		}
	})
}

func TestGetAttachCommand(t *testing.T) {
	m, st := newTestManager(t)

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
	m, _ := newTestManager(t)

	_, err := m.GetAttachCommand("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestGetAllSessions(t *testing.T) {
	m, st := newTestManager(t)

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
	m, st := newTestManager(t)

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
	m, st := newTestManager(t)

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
	m, _ := newTestManager(t)

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
		{
			name:     "preserves space and parens",
			input:    "foo (1)",
			expected: "foo (1)",
		},
		{
			name:     "preserves slash",
			input:    "feature/dark-mode",
			expected: "feature/dark-mode",
		},
		{
			name:     "replaces shell metachars",
			input:    "a;b&c'd",
			expected: "a-b-c-d",
		},
		{
			name:     "replaces dollar and backtick",
			input:    "a$b`c",
			expected: "a-b-c",
		},
		{
			name:     "replaces quotes and backslash",
			input:    `a"b\c`,
			expected: "a-b-c",
		},
		{
			name:     "preserves runs of dashes from repeated replacements",
			input:    "a...b",
			expected: "a---b",
		},
		{
			name:     "trims leading replacement dash",
			input:    "=foo",
			expected: "foo",
		},
		{
			name:     "trims leading dash",
			input:    "-foo",
			expected: "foo",
		},
		{
			name:     "trims leading and trailing spaces",
			input:    "  hello  ",
			expected: "hello",
		},
		{
			name:     "fully invalid becomes empty",
			input:    "...",
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
	m, _ := newTestManager(t)

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

	m := New(cfg, st, statePath, wm, nil, nil)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.Dispose(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestEnsurePipePane(t *testing.T) {
	m, _ := newTestManager(t)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.EnsureTracker("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
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
				Name:       "claude-sonnet-4-6",
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
				ToolName:   "codex",
			},
			prompt: "write a function",
			model: &detect.Model{
				ID: "gpt-5.2-codex",
				Runners: map[string]detect.RunnerSpec{
					"codex":    {ModelValue: "gpt-5.2-codex"},
					"opencode": {ModelValue: "openai/gpt-5.2-codex"},
				},
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
				ToolName: "codex",
			},
			prompt: "test prompt",
			model: &detect.Model{
				ID: "gpt-5.3-codex",
				Runners: map[string]detect.RunnerSpec{
					"codex":    {ModelValue: "gpt-5.3-codex"},
					"opencode": {ModelValue: "openai/gpt-5.3-codex"},
				},
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
			name: "promptable target without prompt succeeds",
			target: ResolvedTarget{
				Name:       "claude",
				Kind:       TargetKindDetected,
				Command:    "claude",
				Promptable: true,
				Env:        map[string]string{},
			},
			prompt:  "",
			model:   nil,
			resume:  false,
			wantErr: false,
			shouldContain: []string{
				"claude",
			},
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
				Name:       "claude-opus-4-6",
				Kind:       TargetKindModel,
				Command:    "claude",
				Promptable: true,
				Env: map[string]string{
					"ANTHROPIC_MODEL": "claude-opus-4-5-20251101",
				},
				ToolName: "claude",
			},
			prompt: "",
			model: &detect.Model{
				ID: "claude-opus-4-6",
				Runners: map[string]detect.RunnerSpec{
					"claude": {ModelValue: "claude-opus-4-5-20251101"},
				},
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
				Name:       "claude-opus-4-6",
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
	m, st := newTestManager(t)

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
	m, _ := newTestManager(t)

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
	m, st := newTestManager(t)

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

	t.Run("excludes specified session from live tmux check", func(t *testing.T) {
		server := tmux.NewTmuxServer("tmux", fmt.Sprintf("nick-exclude-%d", time.Now().UnixNano()), nil)
		if err := server.Check(); err != nil {
			t.Skip("tmux not available")
		}

		ctx := context.Background()
		tmuxName := "same-name"
		_ = server.KillSession(ctx, tmuxName)
		t.Cleanup(func() {
			_ = server.KillSession(ctx, tmuxName)
		})
		if err := server.CreateSession(ctx, tmuxName, t.TempDir(), "sleep 600"); err != nil {
			t.Skipf("cannot create tmux session: %v", err)
		}

		m.server = server
		st.AddSession(state.Session{ID: "same", TmuxSession: tmuxName})

		got := m.nicknameExists("same-name", "same")
		if got != "" {
			t.Errorf("nicknameExists should ignore the excluded session, got %q", got)
		}
	})
}

func TestGenerateUniqueNickname(t *testing.T) {
	m, st := newTestManager(t)

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

	t.Run("sanitized trailing punctuation keeps stable suffixing", func(t *testing.T) {
		st.AddSession(state.Session{ID: "s3", TmuxSession: sanitizeNickname("review:")})

		got := m.generateUniqueNickname("review:")
		if got != "review (1)" {
			t.Errorf("expected 'review (1)', got %q", got)
		}
	})

	t.Run("live tmux session is treated as taken even when state missed it", func(t *testing.T) {
		server := tmux.NewTmuxServer("tmux", fmt.Sprintf("nick-test-%d", time.Now().UnixNano()), nil)
		if err := server.Check(); err != nil {
			t.Skip("tmux not available")
		}

		ctx := context.Background()
		tmuxName := "zsh (1)"
		_ = server.KillSession(ctx, tmuxName)
		t.Cleanup(func() {
			_ = server.KillSession(ctx, tmuxName)
		})

		if err := server.CreateSession(ctx, tmuxName, t.TempDir(), "sleep 600"); err != nil {
			t.Skipf("cannot create tmux session: %v", err)
		}

		m.server = server
		st.AddSession(state.Session{ID: "s3", TmuxSession: "zsh"})

		got := m.generateUniqueNickname("zsh")
		if got != "zsh (2)" {
			t.Errorf("expected 'zsh (2)', got %q", got)
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
		{
			name:             "opencode returns cmd unchanged (uses SpawnEnv)",
			cmd:              "opencode",
			baseTool:         "opencode",
			personaFilePath:  "/tmp/persona.md",
			shouldNotContain: "persona",
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

func TestRemotePersonaWriteCommand(t *testing.T) {
	t.Parallel()

	personaPrompt := "## Persona: Architect\n\n### Instructions\nYou're a senior engineer.\n\nCore principles:\n- Simple > complex\n- It depends on what"
	filePath := ".sl/schmux/system-prompt-session123.md"

	cmd := remotePersonaWriteCommand(personaPrompt, filePath)

	// Command must be single-line (safe for tmux control mode Execute)
	if strings.Contains(cmd, "\n") {
		t.Errorf("command contains newlines, would corrupt tmux control mode:\n%s", cmd)
	}

	// Command must write to the correct path
	if !strings.Contains(cmd, filePath) {
		t.Errorf("command should reference file path %q, got: %s", filePath, cmd)
	}

	// Base64 content must round-trip correctly
	// Extract the base64 portion: command is "printf '%s' '<base64>' | base64 -d > <path>"
	b64Start := strings.Index(cmd, "printf '%s' '") + len("printf '%s' '")
	b64End := strings.Index(cmd[b64Start:], "'")
	if b64Start < 0 || b64End < 0 {
		t.Fatalf("could not extract base64 content from command: %s", cmd)
	}
	encoded := cmd[b64Start : b64Start+b64End]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode failed: %v (encoded: %q)", err, encoded)
	}
	if string(decoded) != personaPrompt {
		t.Errorf("round-trip failed:\n  got:  %q\n  want: %q", string(decoded), personaPrompt)
	}
}

func TestMarkSessionDisposing(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)

	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
	st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Target: "claude", TmuxSession: "test", Status: "stopped"})

	prevStatus, err := m.MarkSessionDisposing("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if prevStatus != "stopped" {
		t.Errorf("expected previous status 'stopped', got %q", prevStatus)
	}

	sess, found := st.GetSession("sess-1")
	if !found {
		t.Fatal("session not found")
	}
	if sess.Status != state.SessionStatusDisposing {
		t.Errorf("expected disposing, got %q", sess.Status)
	}
}

func TestMarkSessionDisposingIdempotent(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)

	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
	st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Target: "claude", TmuxSession: "test", Status: state.SessionStatusDisposing})

	prevStatus, err := m.MarkSessionDisposing("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if prevStatus != state.SessionStatusDisposing {
		t.Errorf("expected disposing (idempotent), got %q", prevStatus)
	}
}

func TestMarkSessionDisposingNotFound(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)

	_, err := m.MarkSessionDisposing("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestResolveTarget(t *testing.T) {
	t.Run("user run target", func(t *testing.T) {
		cfg := &config.Config{
			WorkspacePath: "/tmp/workspaces",
			RunTargets: []config.RunTarget{
				{Name: "lint", Command: "golangci-lint run"},
			},
		}
		st := state.New("", nil)
		statePath := filepath.Join(t.TempDir(), "state.json")
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		m := New(cfg, st, statePath, wm, nil, nil)

		resolved, err := m.ResolveTarget(context.Background(), "lint")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.Kind != TargetKindUser {
			t.Errorf("expected TargetKindUser, got %v", resolved.Kind)
		}
		if resolved.Command != "golangci-lint run" {
			t.Errorf("expected 'golangci-lint run', got %q", resolved.Command)
		}
		if resolved.Promptable {
			t.Error("user run targets should not be promptable")
		}
		if resolved.Name != "lint" {
			t.Errorf("expected name 'lint', got %q", resolved.Name)
		}
	})

	t.Run("builtin tool name fallback", func(t *testing.T) {
		cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
		st := state.New("", nil)
		statePath := filepath.Join(t.TempDir(), "state.json")
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		m := New(cfg, st, statePath, wm, nil, nil)

		resolved, err := m.ResolveTarget(context.Background(), "claude")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.Kind != TargetKindModel {
			t.Errorf("expected TargetKindModel, got %v", resolved.Kind)
		}
		if resolved.Command != "claude" {
			t.Errorf("expected command 'claude', got %q", resolved.Command)
		}
		if !resolved.Promptable {
			t.Error("builtin tools should be promptable")
		}
		if resolved.ToolName != "claude" {
			t.Errorf("expected ToolName 'claude', got %q", resolved.ToolName)
		}
	})

	t.Run("not found", func(t *testing.T) {
		m, _ := newTestManager(t)

		_, err := m.ResolveTarget(context.Background(), "nonexistent-target-xyz")
		if err == nil {
			t.Fatal("expected error for unknown target")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected error containing 'not found', got: %v", err)
		}
	})

	t.Run("user target takes precedence over builtin name", func(t *testing.T) {
		// If user defines a run target named "claude", it should match
		// as a user target (not builtin), because config is checked first
		// after models.
		cfg := &config.Config{
			WorkspacePath: "/tmp/workspaces",
			RunTargets: []config.RunTarget{
				{Name: "claude", Command: "my-custom-claude-wrapper"},
			},
		}
		st := state.New("", nil)
		statePath := filepath.Join(t.TempDir(), "state.json")
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		m := New(cfg, st, statePath, wm, nil, nil)

		resolved, err := m.ResolveTarget(context.Background(), "claude")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.Kind != TargetKindUser {
			t.Errorf("expected TargetKindUser (user target override), got %v", resolved.Kind)
		}
		if resolved.Command != "my-custom-claude-wrapper" {
			t.Errorf("expected custom command, got %q", resolved.Command)
		}
	})

}

func TestResolveWorkspace(t *testing.T) {
	t.Run("by workspace ID found", func(t *testing.T) {
		m, st := newTestManager(t)
		wsPath := t.TempDir()
		st.AddWorkspace(state.Workspace{
			ID:     "ws-resolve-1",
			Repo:   "https://github.com/test/repo.git",
			Branch: "main",
			Path:   wsPath,
		})

		ws, err := m.resolveWorkspace(context.Background(), SpawnOptions{
			WorkspaceID: "ws-resolve-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.ID != "ws-resolve-1" {
			t.Errorf("expected workspace ID 'ws-resolve-1', got %q", ws.ID)
		}
		if ws.Path != wsPath {
			t.Errorf("expected path %q, got %q", wsPath, ws.Path)
		}
	})

	t.Run("by workspace ID not found", func(t *testing.T) {
		m, _ := newTestManager(t)

		_, err := m.resolveWorkspace(context.Background(), SpawnOptions{
			WorkspaceID: "nonexistent-ws",
		})
		if err == nil {
			t.Fatal("expected error for nonexistent workspace")
		}
		if !strings.Contains(err.Error(), "workspace not found") {
			t.Errorf("expected 'workspace not found' error, got: %v", err)
		}
	})
}

func TestDispose_StoppedSession(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)

	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-d1", Repo: "https://example.com/r.git", Branch: "main", Path: wsPath})
	st.AddSession(state.Session{
		ID:          "sess-d1",
		WorkspaceID: "ws-d1",
		Target:      "claude",
		TmuxSession: "schmux-nonexistent-session-xyz",
		Status:      "stopped",
	})

	err := m.Dispose(context.Background(), "sess-d1")
	if err != nil {
		t.Fatalf("Dispose() error: %v", err)
	}

	// Session should be removed from state
	_, found := st.GetSession("sess-d1")
	if found {
		t.Error("session should have been removed from state after dispose")
	}

	// Workspace should still exist (workspaces persist after session disposal)
	_, wsFound := st.GetWorkspace("ws-d1")
	if !wsFound {
		t.Error("workspace should not be removed when session is disposed")
	}
}

func TestDispose_CleansUpTracker(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)

	st.AddWorkspace(state.Workspace{ID: "ws-d2", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
	st.AddSession(state.Session{
		ID:          "sess-d2",
		WorkspaceID: "ws-d2",
		Target:      "claude",
		TmuxSession: "schmux-nonexistent-session-d2",
		Status:      "stopped",
	})

	// Create a tracker for this session
	tracker, err := m.GetTracker("sess-d2")
	if err != nil {
		t.Fatalf("GetTracker: %v", err)
	}
	if tracker == nil {
		t.Fatal("tracker should not be nil")
	}

	// Verify tracker exists before dispose
	m.mu.RLock()
	_, trackerExists := m.trackers["sess-d2"]
	m.mu.RUnlock()
	if !trackerExists {
		t.Fatal("tracker should exist before dispose")
	}

	// Dispose should clean up the tracker
	err = m.Dispose(context.Background(), "sess-d2")
	if err != nil {
		t.Fatalf("Dispose() error: %v", err)
	}

	m.mu.RLock()
	_, trackerExists = m.trackers["sess-d2"]
	m.mu.RUnlock()
	if trackerExists {
		t.Error("tracker should have been removed after dispose")
	}
}

func TestDispose_LastSessionNotifiesCompound(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)

	st.AddWorkspace(state.Workspace{ID: "ws-d3", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
	st.AddSession(state.Session{
		ID:          "sess-d3",
		WorkspaceID: "ws-d3",
		Target:      "claude",
		TmuxSession: "schmux-nonexistent-d3",
		Status:      "stopped",
	})

	var callbackWorkspaceID string
	var callbackIsActive bool
	callbackCalled := false
	m.SetCompoundCallback(func(wsID string, active bool) {
		callbackCalled = true
		callbackWorkspaceID = wsID
		callbackIsActive = active
	})

	err := m.Dispose(context.Background(), "sess-d3")
	if err != nil {
		t.Fatalf("Dispose() error: %v", err)
	}

	if !callbackCalled {
		t.Error("compound callback should have been called for last session in workspace")
	}
	if callbackWorkspaceID != "ws-d3" {
		t.Errorf("expected workspace ID 'ws-d3', got %q", callbackWorkspaceID)
	}
	if callbackIsActive {
		t.Error("expected active=false when last session is disposed")
	}
}

func TestDispose_NotLastSessionSkipsCompound(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)

	st.AddWorkspace(state.Workspace{ID: "ws-d4", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
	// Two sessions in the same workspace
	st.AddSession(state.Session{
		ID:          "sess-d4a",
		WorkspaceID: "ws-d4",
		Target:      "claude",
		TmuxSession: "schmux-nonexistent-d4a",
		Status:      "stopped",
	})
	st.AddSession(state.Session{
		ID:          "sess-d4b",
		WorkspaceID: "ws-d4",
		Target:      "codex",
		TmuxSession: "schmux-nonexistent-d4b",
		Status:      "running",
	})

	callbackCalled := false
	m.SetCompoundCallback(func(wsID string, active bool) {
		callbackCalled = true
	})

	// Dispose only the first session — second still exists
	err := m.Dispose(context.Background(), "sess-d4a")
	if err != nil {
		t.Fatalf("Dispose() error: %v", err)
	}

	if callbackCalled {
		t.Error("compound callback should NOT be called when other sessions remain in workspace")
	}
}

func TestRevertSessionStatus(t *testing.T) {
	t.Run("restores original status", func(t *testing.T) {
		cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
		statePath := t.TempDir() + "/state.json"
		st := state.New(statePath, nil)
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		m := New(cfg, st, statePath, wm, nil, nil)

		st.AddWorkspace(state.Workspace{ID: "ws-r1", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
		st.AddSession(state.Session{ID: "sess-r1", WorkspaceID: "ws-r1", Target: "claude", TmuxSession: "test-r1", Status: "running"})

		// Mark as disposing
		prevStatus, err := m.MarkSessionDisposing("sess-r1")
		if err != nil {
			t.Fatal(err)
		}
		if prevStatus != "running" {
			t.Fatalf("expected previous status 'running', got %q", prevStatus)
		}

		// Revert
		m.RevertSessionStatus("sess-r1", prevStatus)

		sess, found := st.GetSession("sess-r1")
		if !found {
			t.Fatal("session should still exist")
		}
		if sess.Status != "running" {
			t.Errorf("expected status 'running' after revert, got %q", sess.Status)
		}
	})

	t.Run("nonexistent session does not affect other sessions", func(t *testing.T) {
		m, st := newTestManager(t)
		st.AddSession(state.Session{ID: "sess-real", Status: "stopped"})

		m.RevertSessionStatus("nonexistent", "running")

		// The real session must be unchanged
		sess, found := st.GetSession("sess-real")
		if !found {
			t.Fatal("existing session should still exist")
		}
		if sess.Status != "stopped" {
			t.Errorf("existing session status should be unchanged, got %q", sess.Status)
		}
	})
}

func TestStop_CleansUpAllTrackers(t *testing.T) {
	m, st := newTestManager(t)

	// Add two sessions and create trackers for them
	st.AddSession(state.Session{ID: "s-stop1", TmuxSession: "t1"})
	st.AddSession(state.Session{ID: "s-stop2", TmuxSession: "t2"})

	tracker1, err := m.GetTracker("s-stop1")
	if err != nil {
		t.Fatalf("GetTracker s-stop1: %v", err)
	}
	tracker2, err := m.GetTracker("s-stop2")
	if err != nil {
		t.Fatalf("GetTracker s-stop2: %v", err)
	}
	if tracker1 == nil || tracker2 == nil {
		t.Fatal("trackers should not be nil")
	}

	// Stop should clean up all trackers
	m.Stop()

	m.mu.RLock()
	remaining := len(m.trackers)
	m.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("expected 0 trackers after Stop, got %d", remaining)
	}
}

// --- ensureEventsWindow tests ---

// mockEventsConn records calls to CreateSession, FindSessionByName, and KillSession.
type mockEventsConn struct {
	existingWindow *controlmode.WindowInfo // returned by FindSessionByName
	createWindowID string
	createPaneID   string
	calls          []string // records call order: "find", "kill", "create"
	killedWindowID string
}

func (m *mockEventsConn) FindSessionByName(_ context.Context, _ string) (*controlmode.WindowInfo, error) {
	m.calls = append(m.calls, "find")
	return m.existingWindow, nil
}

func (m *mockEventsConn) KillSession(_ context.Context, windowID string) error {
	m.calls = append(m.calls, "kill")
	m.killedWindowID = windowID
	return nil
}

func (m *mockEventsConn) CreateSession(_ context.Context, _, _, _ string) (string, string, error) {
	m.calls = append(m.calls, "create")
	return m.createWindowID, m.createPaneID, nil
}

func TestEnsureEventsWindow_KillsStaleWindow(t *testing.T) {
	mock := &mockEventsConn{
		existingWindow: &controlmode.WindowInfo{
			WindowID:   "@5",
			WindowName: "schmux-events-abc12345",
			PaneID:     "%10",
		},
		createWindowID: "@9",
		createPaneID:   "%15",
	}

	windowID, paneID, err := ensureEventsWindow(context.Background(), mock, "schmux-events-abc12345", "/tmp/ws")
	if err != nil {
		t.Fatal(err)
	}
	if windowID != "@9" || paneID != "%15" {
		t.Errorf("got window=%s pane=%s, want @9 %%15", windowID, paneID)
	}

	// Must kill stale window before creating the new one
	if len(mock.calls) < 3 {
		t.Fatalf("expected at least 3 calls (find, kill, create), got %v", mock.calls)
	}
	if mock.calls[0] != "find" {
		t.Errorf("first call should be find, got %s", mock.calls[0])
	}
	if mock.calls[1] != "kill" {
		t.Errorf("second call should be kill, got %s", mock.calls[1])
	}
	if mock.killedWindowID != "@5" {
		t.Errorf("killed window %s, want @5", mock.killedWindowID)
	}
	if mock.calls[2] != "create" {
		t.Errorf("third call should be create, got %s", mock.calls[2])
	}
}

func TestEnsureEventsWindow_NoStaleWindow(t *testing.T) {
	mock := &mockEventsConn{
		existingWindow: nil, // no stale window
		createWindowID: "@3",
		createPaneID:   "%7",
	}

	windowID, paneID, err := ensureEventsWindow(context.Background(), mock, "schmux-events-abc12345", "/tmp/ws")
	if err != nil {
		t.Fatal(err)
	}
	if windowID != "@3" || paneID != "%7" {
		t.Errorf("got window=%s pane=%s, want @3 %%7", windowID, paneID)
	}

	// Should NOT call kill when no stale window exists
	for _, call := range mock.calls {
		if call == "kill" {
			t.Fatal("KillSession should not be called when no stale window exists")
		}
	}
}

func TestQueuedSessionTimeout(t *testing.T) {
	m, st := newTestManager(t)
	m.queueTimeout = 100 * time.Millisecond

	// Add a session in "provisioning" status
	sess := state.Session{
		ID:        "timeout-test",
		Target:    "claude",
		Status:    "provisioning",
		CreatedAt: time.Now(),
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("failed to add session: %v", err)
	}

	// Simulate the queue-wait goroutine with a channel that never sends
	resultCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		qTimeout := m.queueTimeout
		timer := time.NewTimer(qTimeout)
		defer timer.Stop()

		select {
		case <-resultCh:
			t.Error("resultCh should not receive")
		case <-timer.C:
			st.UpdateSessionFunc("timeout-test", func(s *state.Session) {
				s.Status = "failed"
			})
			st.SaveBatched()
		}
	}()

	<-done

	// Verify session was marked as failed
	found, ok := st.GetSession("timeout-test")
	if !ok {
		t.Fatal("session not found in state")
	}
	if found.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", found.Status)
	}
}

func TestSpawn_NoTmux(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		RunTargets: []config.RunTarget{
			{Name: "test-tool", Command: "echo hello"},
		},
	}
	st := state.New("", nil)
	statePath := filepath.Join(t.TempDir(), "state.json")
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))

	// nil server = tmux not available (e.g., remote-only setup)
	m := New(cfg, st, statePath, wm, nil, nil)

	_, err := m.Spawn(context.Background(), SpawnOptions{
		TargetName: "test-tool",
	})
	if err == nil {
		t.Fatal("expected error when tmux is not available")
	}
	if !strings.Contains(err.Error(), "tmux is required") {
		t.Errorf("expected tmux install instructions in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "brew install tmux") {
		t.Errorf("expected macOS install hint, got: %v", err)
	}
	if !strings.Contains(err.Error(), "apt install tmux") {
		t.Errorf("expected Linux install hint, got: %v", err)
	}
}
