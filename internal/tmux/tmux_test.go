package tmux

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testServer is a TmuxServer used by tests that exercise tmux CLI methods.
var testServer = NewTmuxServer("tmux", "default", nil)

func TestCaptureLastLines_Validation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		lines       int
		wantErr     bool
		errContains string
	}{
		{
			name:        "zero lines",
			lines:       0,
			wantErr:     true,
			errContains: "invalid line count",
		},
		{
			name:        "negative lines",
			lines:       -1,
			wantErr:     true,
			errContains: "invalid line count",
		},
		{
			name:        "negative large lines",
			lines:       -100,
			wantErr:     true,
			errContains: "invalid line count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testServer.CaptureLastLines(ctx, "test-session", tt.lines, true)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %d lines", tt.lines)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want containing %q", err, tt.errContains)
				}
			}
		})
	}

	// Positive line counts should pass validation (tmux may not be installed, so exec may fail)
	t.Run("positive line count passes validation", func(t *testing.T) {
		_, err := testServer.CaptureLastLines(ctx, "test-session", 10, true)
		if err != nil && strings.Contains(err.Error(), "invalid line count") {
			t.Errorf("unexpected validation error: %v", err)
		}
		// Other errors (like tmux not installed) are fine
	})
}

// Context cancellation tests: these verify that all tmux functions that accept
// a context propagate cancellation to the underlying exec.CommandContext call.
// We use a deadline in the past to make cancellation deterministic — the context
// is already expired before exec.Command starts, so the error is guaranteed.
func TestContextCancellation(t *testing.T) {
	// Create a context that is already expired
	expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	t.Run("CreateSession rejects cancelled context", func(t *testing.T) {
		err := testServer.CreateSession(expiredCtx, "test", "/tmp", "echo test")
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	})

	t.Run("ListSessions rejects cancelled context", func(t *testing.T) {
		_, err := testServer.ListSessions(expiredCtx)
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	})

	t.Run("GetPanePID rejects cancelled context", func(t *testing.T) {
		_, err := testServer.GetPanePID(expiredCtx, "test")
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	})

	t.Run("CaptureOutput rejects cancelled context", func(t *testing.T) {
		_, err := testServer.CaptureOutput(expiredCtx, "test")
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	})

	t.Run("KillSession rejects cancelled context", func(t *testing.T) {
		err := testServer.KillSession(expiredCtx, "test")
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	})

	t.Run("RenameSession rejects cancelled context", func(t *testing.T) {
		err := testServer.RenameSession(expiredCtx, "old", "new")
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	})
}

func TestExtractLatestResponseCapsContent(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	lines = append(lines, "❯")

	got := ExtractLatestResponse(lines)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != MaxExtractedLines {
		t.Fatalf("expected %d lines, got %d", MaxExtractedLines, len(gotLines))
	}

	expectedStart := 100 - MaxExtractedLines + 1
	if gotLines[0] != "line "+strconv.Itoa(expectedStart) || gotLines[len(gotLines)-1] != "line 100" {
		t.Fatalf("unexpected line range: %q ... %q", gotLines[0], gotLines[len(gotLines)-1])
	}
}

// NOTE: Core tmux operations (CreateSession, KillSession, SendKeys,
// CaptureOutput, ListSessions, SessionExists) require a running tmux
// server and are tested in the E2E test suite (internal/e2e/) which
// runs inside Docker with tmux installed.

// TmuxServer unit tests

func TestTmuxServerCmdPrependsSocket(t *testing.T) {
	srv := NewTmuxServer("tmux", "schmux", nil)
	cmd := srv.cmd(context.Background(), "list-sessions")
	want := []string{"-L", "schmux", "list-sessions"}
	got := cmd.Args[1:] // skip binary name
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmd args = %v, want %v", got, want)
	}
}

func TestTmuxServerBinaryAccessor(t *testing.T) {
	srv := NewTmuxServer("/usr/local/bin/tmux", "schmux", nil)
	if got := srv.Binary(); got != "/usr/local/bin/tmux" {
		t.Errorf("Binary() = %q, want %q", got, "/usr/local/bin/tmux")
	}
}

func TestTmuxServerSocketNameAccessor(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-socket", nil)
	if got := srv.SocketName(); got != "test-socket" {
		t.Errorf("SocketName() = %q, want %q", got, "test-socket")
	}
}

func TestTmuxServerGetAttachCommand(t *testing.T) {
	srv := NewTmuxServer("tmux", "schmux", nil)
	got := srv.GetAttachCommand("my-session")
	want := `tmux -L schmux attach -t "=my-session"`
	if got != want {
		t.Errorf("GetAttachCommand() = %q, want %q", got, want)
	}
}

func TestTmuxServerCreateSessionArgs(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-sock", nil)
	// Use an expired context so the command never actually runs
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	// We can't check cmd args directly via CreateSession since it runs internally,
	// but we can verify that cmd() builds the right args for the new-session command.
	cmd := srv.cmd(ctx, "new-session", "-d", "-s", "sess1", "-c", "/tmp", "echo hi")
	want := []string{"-L", "test-sock", "new-session", "-d", "-s", "sess1", "-c", "/tmp", "echo hi"}
	got := cmd.Args[1:]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CreateSession cmd args = %v, want %v", got, want)
	}
}

func TestTmuxServerKillSessionArgs(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-sock", nil)
	cmd := srv.cmd(context.Background(), "kill-session", "-t", "=my-session")
	want := []string{"-L", "test-sock", "kill-session", "-t", "=my-session"}
	got := cmd.Args[1:]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KillSession cmd args = %v, want %v", got, want)
	}
}

func TestTmuxServerShowEnvironmentArgs(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-sock", nil)
	cmd := srv.cmd(context.Background(), "show-environment", "-g")
	want := []string{"-L", "test-sock", "show-environment", "-g"}
	got := cmd.Args[1:]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ShowEnvironment cmd args = %v, want %v", got, want)
	}
}

func TestTmuxServerSetOptionArgs(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-sock", nil)
	cmd := srv.cmd(context.Background(), "set-option", "-t", "sess1", "history-limit", "10000")
	want := []string{"-L", "test-sock", "set-option", "-t", "sess1", "history-limit", "10000"}
	got := cmd.Args[1:]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SetOption cmd args = %v, want %v", got, want)
	}
}

func TestTmuxServerCaptureLastLinesValidation(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-sock", nil)
	ctx := context.Background()

	_, err := srv.CaptureLastLines(ctx, "test", 0, true)
	if err == nil || !strings.Contains(err.Error(), "invalid line count") {
		t.Errorf("expected 'invalid line count' error for 0 lines, got %v", err)
	}

	_, err = srv.CaptureLastLines(ctx, "test", -5, false)
	if err == nil || !strings.Contains(err.Error(), "invalid line count") {
		t.Errorf("expected 'invalid line count' error for -5 lines, got %v", err)
	}
}

func TestTmuxServerRenameSessionArgs(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-sock", nil)
	cmd := srv.cmd(context.Background(), "rename-session", "-t", "=old-name", "new-name")
	want := []string{"-L", "test-sock", "rename-session", "-t", "=old-name", "new-name"}
	got := cmd.Args[1:]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RenameSession cmd args = %v, want %v", got, want)
	}
}

func TestValidateSessionName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		// Valid names
		{
			name:    "simple alphanumeric",
			input:   "test123",
			wantErr: false,
		},
		{
			name:    "with dash",
			input:   "my-session",
			wantErr: false,
		},
		{
			name:    "with underscore",
			input:   "my_session",
			wantErr: false,
		},
		{
			name:    "mixed valid chars",
			input:   "test-123_session",
			wantErr: false,
		},
		{
			name:    "single char",
			input:   "a",
			wantErr: false,
		},
		{
			name:    "uppercase",
			input:   "TEST-SESSION",
			wantErr: false,
		},

		// Invalid names - command injection attempts
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "semicolon injection",
			input:   "test; rm -rf /",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "pipe is literal inside double quotes",
			input:   "test | cat",
			wantErr: false,
		},
		{
			name:    "backtick injection",
			input:   "test`whoami`",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "dollar paren injection",
			input:   "test$(whoami)",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "ampersand injection",
			input:   "test && rm file",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "newline injection",
			input:   "test\nrm -rf /",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "space character",
			input:   "test session",
			wantErr: false,
		},
		{
			name:    "parens and space",
			input:   "foo (1)",
			wantErr: false,
		},
		{
			name:    "forward slash",
			input:   "feature/dark-mode",
			wantErr: false,
		},
		{
			name:    "backslash",
			input:   "test\\session",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "single quote",
			input:   "test'session",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "double quote",
			input:   "test\"session",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "equals sign (tmux special)",
			input:   "=test",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "colon (tmux special)",
			input:   "test:session",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "dot",
			input:   "test.session",
			wantErr: true,
			errMsg:  "invalid session name",
		},
		{
			name:    "leading dash",
			input:   "-foo",
			wantErr: true,
			errMsg:  "cannot start with",
		},
		{
			name:    "leading space",
			input:   " foo",
			wantErr: true,
			errMsg:  "cannot start with",
		},
		{
			name:    "trailing space",
			input:   "foo ",
			wantErr: true,
			errMsg:  "cannot end with",
		},
		{
			name:    "tab character",
			input:   "foo\tbar",
			wantErr: true,
			errMsg:  "invalid session name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSessionName(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateSessionName(%q) error = %q, want error containing %q", tt.input, err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSessionName(%q) unexpected error: %v", tt.input, err)
				}
			}
		})
	}
}

func TestGetAttachCommandValidation(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-sock", nil)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid name",
			input: "test-session",
			want:  "tmux -L test-sock attach -t \"=test-session\"",
		},
		{
			name:  "injection attempt returns empty",
			input: "test; rm -rf /",
			want:  "",
		},
		{
			name:  "empty name returns empty",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := srv.GetAttachCommand(tt.input)
			if got != tt.want {
				t.Errorf("GetAttachCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
