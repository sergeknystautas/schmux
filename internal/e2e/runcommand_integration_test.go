//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
)

// TestRunCommandRealTmux runs RunCommand against a real tmux in control mode.
// This tests the full pipeline: new-window, capture-pane, sentinel parsing
// without the daemon, isolating RunCommand behavior with real tmux.
func TestRunCommandRealTmux(t *testing.T) {
	t.Parallel()
	socketName := fmt.Sprintf("schmux-integ-%d", time.Now().UnixNano()%100000)

	// Start tmux in control mode with a unique socket.
	// -A attaches to existing session or creates new (keeps control mode alive).
	// Do NOT use -d — that detaches immediately and tmux exits.
	// TERM=dumb prevents tmux from wrapping output in DCS escape sequences.
	cmd := exec.Command("tmux", "-L", socketName, "-CC", "new-session", "-A", "-s", "test")
	cmd.Env = append(os.Environ(), "TERM=dumb")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start tmux: %v", err)
	}
	defer func() {
		exec.Command("tmux", "-L", socketName, "kill-server").Run()
		cmd.Process.Kill()
		cmd.Wait()
		ptmx.Close()
	}()

	// Create a tee reader so we can log what the parser sees
	teeR, teeW := io.Pipe()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				t.Logf("TMUX_RAW (%d bytes): %q", n, string(buf[:n]))
				teeW.Write(buf[:n])
			}
			if err != nil {
				teeW.CloseWithError(err)
				return
			}
		}
	}()

	// Create parser (reads from tee) and client (writes to ptmx)
	parser := controlmode.NewParser(teeR, nil, "test")
	client := controlmode.NewClient(ptmx, parser, nil)

	go parser.Run()
	client.Start()
	defer client.Close()

	// Wait for control mode to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case <-parser.ControlModeReady():
		t.Log("control mode ready")
	case <-ctx.Done():
		t.Fatal("timeout waiting for control mode")
	}

	// Verify basic Execute works
	resp, _, err := client.Execute(ctx, "display-message -p 'hello'")
	if err != nil {
		t.Fatalf("basic Execute failed: %v", err)
	}
	t.Logf("display-message response: %q", resp)

	// Test RunCommand with a simple command
	t.Run("SimpleEcho", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		out, err := client.RunCommand(ctx, "/tmp", "echo hello-world")
		if err != nil {
			t.Fatalf("RunCommand failed: %v", err)
		}
		if !strings.Contains(out, "hello-world") {
			t.Errorf("expected 'hello-world' in output, got: %q", out)
		}
		t.Logf("RunCommand output: %q", out)
	})

	// Test RunCommand with multiple sequential calls
	t.Run("SequentialCalls", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			out, err := client.RunCommand(ctx, "/tmp", fmt.Sprintf("echo call-%d", i))
			cancel()
			if err != nil {
				t.Fatalf("RunCommand #%d failed: %v", i, err)
			}
			expected := fmt.Sprintf("call-%d", i)
			if !strings.Contains(out, expected) {
				t.Errorf("RunCommand #%d: expected %q in output, got: %q", i, expected, out)
			}
		}
	})

	// Test RunCommand with git (the actual use case for VCS status)
	t.Run("GitDiff", func(t *testing.T) {
		repoDir := t.TempDir()
		RunCmd(t, repoDir, "git", "init", "-b", "main")
		RunCmd(t, repoDir, "git", "config", "user.email", "test@test.local")
		RunCmd(t, repoDir, "git", "config", "user.name", "Test")
		if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		RunCmd(t, repoDir, "git", "add", ".")
		RunCmd(t, repoDir, "git", "commit", "-m", "init")
		if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello\nmodified\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		out, err := client.RunCommand(ctx, repoDir, "git diff HEAD --numstat")
		if err != nil {
			t.Fatalf("git diff RunCommand failed: %v", err)
		}
		t.Logf("git diff output: %q", out)
		if !strings.Contains(out, "file.txt") {
			t.Errorf("expected 'file.txt' in git diff output, got: %q", out)
		}
	})
}
