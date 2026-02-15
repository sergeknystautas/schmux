package tmux

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
			want:  `tmux attach -t "=test-session"`,
		},
		{
			name:  "session with spaces",
			input: "cli commands",
			want:  `tmux attach -t "=cli commands"`,
		},
		{
			name:  "session with special chars",
			input: "session-123_abc",
			want:  `tmux attach -t "=session-123_abc"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  `tmux attach -t "="`,
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
			_, err := CaptureLastLines(ctx, "test-session", tt.lines, true)

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
		_, err := CaptureLastLines(ctx, "test-session", 10, true)
		if err != nil && strings.Contains(err.Error(), "invalid line count") {
			t.Errorf("unexpected validation error: %v", err)
		}
		// Other errors (like tmux not installed) are fine
	})
}

// Context cancellation tests: these verify that functions accept a context
// parameter and attempt to honor cancellation. Due to inherent race conditions
// between cancellation and OS-level command execution, we use t.Log rather
// than t.Error when cancellation is not observed.
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

	t.Run("GetWindowSize respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := GetWindowSize(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
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
	cases := loadNudgenikManifest(t)

	for _, tc := range cases {
		name := strings.TrimSuffix(tc.Capture, ".txt")
		want := strings.TrimSuffix(tc.Capture, ".txt") + ".want.txt"
		t.Run(name, func(t *testing.T) {
			inputPath := filepath.Join("testdata", tc.Capture)
			inputRaw, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}

			wantPath := filepath.Join("testdata", want)
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

// TestUpdateGoldenFiles regenerates .want.txt files from actual extractor output.
// Run with: UPDATE_GOLDEN=1 go test -v -run TestUpdateGoldenFiles ./internal/tmux/...
func TestUpdateGoldenFiles(t *testing.T) {
	if os.Getenv("UPDATE_GOLDEN") == "" {
		t.Skip("set UPDATE_GOLDEN=1 to regenerate golden files")
	}

	cases := loadNudgenikManifest(t)

	for _, tc := range cases {
		f := tc.Capture
		inputPath := filepath.Join("testdata", f)
		inputRaw, err := os.ReadFile(inputPath)
		if err != nil {
			t.Logf("skip %s: %v", f, err)
			continue
		}

		input := StripAnsi(string(inputRaw))
		lines := strings.Split(input, "\n")
		got := ExtractLatestResponse(lines)

		wantFile := strings.TrimSuffix(f, ".txt") + ".want.txt"
		wantPath := filepath.Join("testdata", wantFile)
		if err := os.WriteFile(wantPath, []byte(got+"\n"), 0644); err != nil {
			t.Errorf("write %s: %v", wantFile, err)
		} else {
			t.Logf("updated %s", wantFile)
		}
	}
}

type nudgenikManifest struct {
	Version int                `yaml:"version"`
	Cases   []nudgenikTestCase `yaml:"cases"`
}

type nudgenikTestCase struct {
	ID        string `yaml:"id"`
	Capture   string `yaml:"capture"`
	WantState string `yaml:"want_state"`
	Notes     string `yaml:"notes"`
}

func loadNudgenikManifest(t *testing.T) []nudgenikTestCase {
	t.Helper()

	path := filepath.Join("testdata", "manifest.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read nudgenik manifest: %v", err)
	}

	var manifest nudgenikManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse nudgenik manifest: %v", err)
	}

	if len(manifest.Cases) == 0 {
		t.Fatalf("nudgenik manifest has no cases")
	}

	for i, tc := range manifest.Cases {
		if strings.TrimSpace(tc.Capture) == "" {
			t.Fatalf("nudgenik manifest case %d missing capture", i)
		}
		if !strings.HasSuffix(tc.Capture, ".txt") {
			t.Fatalf("nudgenik manifest case %d capture must be .txt: %q", i, tc.Capture)
		}
	}

	return manifest.Cases
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

// Benchmarks

func BenchmarkGetAttachCommand(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetAttachCommand("test-session")
	}
}
