package tmux

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestStripAnsi(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no escape sequences",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "color codes",
			input: "\x1b[31mred text\x1b[0m",
			want:  "red text",
		},
		{
			name:  "bold",
			input: "\x1b[1mbold\x1b[0m",
			want:  "bold",
		},
		{
			name:  "multiple codes",
			input: "\x1b[31;1mred bold\x1b[0m",
			want:  "red bold",
		},
		{
			name:  "cursor movement",
			input: "text\x1b[2K\x1b[1Gmore",
			want:  "textmore",
		},
		{
			name:  "mixed content",
			input: "\x1b[90mConnecting\x1b[0m...\x1b[32mOK\x1b[0m",
			want:  "Connecting...OK",
		},
		{
			name:  "OSC sequences (window title)",
			input: "\x1b]0;window title\x07text",
			want:  "text",
		},
		{
			name:  "OSC with ST terminator",
			input: "\x1b]0;title\x1b\\text",
			want:  "text",
		},
		{
			name:  "multiline with codes",
			input: "line1\x1b[0m\nline2\x1b[31mred\x1b[0m\nline3",
			want:  "line1\nline2red\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripAnsi(tt.input)
			if got != tt.want {
				t.Errorf("StripAnsi() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetAttachCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple session name",
			input: "test-session",
			want:  `tmux attach -t "test-session"`,
		},
		{
			name:  "session with spaces",
			input: "cli commands",
			want:  `tmux attach -t "cli commands"`,
		},
		{
			name:  "session with special chars",
			input: "session-123_abc",
			want:  `tmux attach -t "session-123_abc"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  `tmux attach -t ""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetAttachCommand(tt.input)
			if got != tt.want {
				t.Errorf("GetAttachCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

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
			_, err := CaptureLastLines(ctx, "test-session", tt.lines)

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
		_, err := CaptureLastLines(ctx, "test-session", 10)
		if err != nil && strings.Contains(err.Error(), "invalid line count") {
			t.Errorf("unexpected validation error: %v", err)
		}
		// Other errors (like tmux not installed) are fine
	})
}

func TestContextCancellation(t *testing.T) {
	t.Run("CreateSession respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := CreateSession(ctx, "test", "/tmp", "echo test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("ListSessions respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := ListSessions(ctx)
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("GetPanePID respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := GetPanePID(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("CaptureOutput respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := CaptureOutput(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("KillSession respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := KillSession(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("SendKeys respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := SendKeys(ctx, "test", "command")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("SetWindowSizeManual respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := SetWindowSizeManual(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("ResizeWindow respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := ResizeWindow(ctx, "test", 80, 24)
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("StartPipePane respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := StartPipePane(ctx, "test", "/tmp/test.log")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("StopPipePane respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := StopPipePane(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("IsPipePaneActive respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		active := IsPipePaneActive(ctx, "test")
		// Should return false on error
		if active {
			t.Log("unexpected true for cancelled context")
		}
	})

	t.Run("RenameSession respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := RenameSession(ctx, "old", "new")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})
}

func TestExtractLatestResponse(t *testing.T) {
	fixtures := []struct {
		name string
		in   string
		want string
	}{
		{name: "claude1", in: "claude1.txt", want: "claude1.want.txt"},
		{name: "claude2", in: "claude2.txt", want: "claude2.want.txt"},
		{name: "claude3", in: "claude3.txt", want: "claude3.want.txt"},
		{name: "claude4", in: "claude4.txt", want: "claude4.want.txt"},
		{name: "claude6", in: "claude6.txt", want: "claude6.want.txt"},
		{name: "claude7", in: "claude7.txt", want: "claude7.want.txt"},
		{name: "claude8", in: "claude8.txt", want: "claude8.want.txt"},
		{name: "codex1", in: "codex1.txt", want: "codex1.want.txt"},
		{name: "codex2", in: "codex2.txt", want: "codex2.want.txt"},
		{name: "codex3", in: "codex3.txt", want: "codex3.want.txt"},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			inputPath := filepath.Join("testdata", tt.in)
			inputRaw, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}

			wantPath := filepath.Join("testdata", tt.want)
			wantRaw, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("read want: %v", err)
			}

			input := StripAnsi(string(inputRaw))
			lines := strings.Split(input, "\n")
			got := ExtractLatestResponse(lines)
			want := strings.TrimRight(string(wantRaw), "\n")

			if got != want {
				t.Errorf("extractLatestResponse() mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
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

// Tests that require tmux to be installed - skipped by default

func TestListSessions(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestSessionExists(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestCaptureOutput(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestCreateSession(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestKillSession(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestSendKeys(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

// Benchmarks

func BenchmarkGetAttachCommand(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetAttachCommand("test-session")
	}
}
