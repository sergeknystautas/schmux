package floormanager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

func TestShouldInject(t *testing.T) {
	tests := []struct {
		name     string
		prev     string
		curr     string
		expected bool
	}{
		{"working to error", "working", "error", true},
		{"working to needs_input", "working", "needs_input", true},
		{"working to needs_testing", "working", "needs_testing", true},
		{"working to completed", "working", "completed", true},
		{"working to working", "working", "working", false},
		{"needs_input to working", "needs_input", "working", false},
		{"error to working", "error", "working", false},
		{"empty to working", "", "working", false},
		{"empty to error", "", "error", true},
		{"empty to needs_input", "", "needs_input", true},
		{"completed to error", "completed", "error", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldInject(tt.prev, tt.curr)
			if got != tt.expected {
				t.Errorf("shouldInject(%q, %q) = %v, want %v", tt.prev, tt.curr, got, tt.expected)
			}
		})
	}
}

func TestFormatSignalMessage(t *testing.T) {
	tests := []struct {
		name     string
		nickname string
		prev     string
		state    string
		message  string
		intent   string
		blockers string
		want     string
	}{
		{
			name:     "minimal",
			nickname: "claude-1",
			prev:     "working",
			state:    "completed",
			message:  "Auth module finished",
			want:     `[SIGNAL] claude-1: working -> completed "Auth module finished"`,
		},
		{
			name:     "with intent and blockers",
			nickname: "claude-1",
			prev:     "working",
			state:    "needs_input",
			message:  "Need clarification",
			intent:   "Implementing OAuth2",
			blockers: "Unknown token format",
			want:     `[SIGNAL] claude-1: working -> needs_input "Need clarification" intent="Implementing OAuth2" blocked="Unknown token format"`,
		},
		{
			name:     "with intent only",
			nickname: "claude-1",
			prev:     "working",
			state:    "error",
			message:  "Build failed",
			intent:   "Running tests",
			want:     `[SIGNAL] claude-1: working -> error "Build failed" intent="Running tests"`,
		},
		{
			name:     "empty prev state",
			nickname: "agent-2",
			prev:     "",
			state:    "error",
			message:  "Crashed",
			want:     `[SIGNAL] agent-2: -> error "Crashed"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSignalMessage(tt.nickname, tt.prev, tt.state, tt.message, tt.intent, tt.blockers)
			if got != tt.want {
				t.Errorf("FormatSignalMessage() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

func TestFlushClearsPartialInputBeforeInjecting(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
	sessName := fmt.Sprintf("schmux-fm-inject-test-%d", os.Getpid())
	testServer := tmux.NewTmuxServer("tmux", "default", nil)

	_ = testServer.KillSession(ctx, sessName)
	t.Cleanup(func() {
		_ = testServer.KillSession(ctx, sessName)
	})

	tmpDir := t.TempDir()

	// Create a session running bash (readline supports Ctrl+U)
	if err := testServer.CreateSession(ctx, sessName, tmpDir, "bash --norc --noprofile"); err != nil {
		t.Skip("cannot create tmux session:", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Type partial input to simulate operator typing.
	sendCmd := exec.CommandContext(ctx, testServer.Binary(), "-L", testServer.SocketName(), "send-keys", "-l", "-t", sessName, "hello wor")
	if out, err := sendCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to type partial input: %v: %s", err, string(out))
	}
	time.Sleep(100 * time.Millisecond)

	// Set up a LocalSource + SessionRuntime so the injector can use the runtime
	source := session.NewLocalSource("fm-test", sessName, testServer, nil)
	source.Start()
	runtime := session.NewSessionRuntime("fm-test", source, nil, "", nil, nil, nil)
	runtime.Start()
	t.Cleanup(func() {
		runtime.Stop()
	})

	// Wait for the control mode connection to establish
	deadline := time.Now().Add(3 * time.Second)
	for !source.IsAttached() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !source.IsAttached() {
		t.Fatal("source did not attach in time")
	}

	// Set up injector with pending signal
	m := &Manager{
		tmuxSession: sessName,
		tracker:     runtime,
		logger:      log.Default(),
	}
	inj := NewInjector(m, 0, log.Default())
	inj.pending = []string{`[SIGNAL] agent-1: working -> completed "Task done"`}

	// Flush should clear the partial input, then inject the signal
	inj.flush(ctx)
	time.Sleep(300 * time.Millisecond)

	// Capture pane output
	output, err := testServer.CaptureOutput(ctx, sessName)
	if err != nil {
		t.Fatal("failed to capture output:", err)
	}

	// The signal text should NOT be garbled with the partial input
	if strings.Contains(output, "hello wor[SIGNAL]") {
		t.Error("signal text was garbled with partial operator input")
	}
	// The signal should still have been injected
	if !strings.Contains(output, "[SIGNAL]") {
		t.Error("signal text was not injected")
	}
}
