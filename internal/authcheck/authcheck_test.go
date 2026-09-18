package authcheck

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// stubBin writes an executable script with the given body under dir with
// the given name and prepends dir to PATH.
func stubBin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
}

func withStubbedPATH(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
	t.Cleanup(func() { os.Setenv("PATH", orig) })
}

func TestRunClaude(t *testing.T) {
	dir := t.TempDir()
	withStubbedPATH(t, dir)

	stubBin(t, dir, "claude", `echo '{"loggedIn": true, "authMethod": "oauth_token"}'`)
	if res, _ := Run(context.Background(), chat.ProtocolClaude); res != LoggedIn {
		t.Error("loggedIn true should be LoggedIn")
	}

	stubBin(t, dir, "claude", `echo '{"loggedIn": false}'`)
	if res, _ := Run(context.Background(), chat.ProtocolClaude); res != LoggedOut {
		t.Error("loggedIn false should be LoggedOut")
	}

	stubBin(t, dir, "claude", `echo 'not json at all'`)
	if res, _ := Run(context.Background(), chat.ProtocolClaude); res != NoAnswer {
		t.Error("unparseable output should be NoAnswer")
	}

	stubBin(t, dir, "claude", `sleep 5`)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if res, _ := Run(ctx, chat.ProtocolClaude); res != NoAnswer {
		t.Error("timeout should be NoAnswer")
	}
}

func TestInvalidateClaudeUsesLogout(t *testing.T) {
	dir := t.TempDir()
	withStubbedPATH(t, dir)
	calls := filepath.Join(dir, "calls")
	t.Setenv("AUTHCHECK_CALLS", calls)
	stubBin(t, dir, "claude", `printf '%s' "$*" > "$AUTHCHECK_CALLS"`)

	if _, err := InvalidateClaude(context.Background()); err != nil {
		t.Fatalf("InvalidateClaude: %v", err)
	}
	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("read invocation: %v", err)
	}
	if string(got) != "auth logout" {
		t.Errorf("claude args = %q, want %q", got, "auth logout")
	}
}

func TestRunCodex(t *testing.T) {
	dir := t.TempDir()
	withStubbedPATH(t, dir)

	stubBin(t, dir, "codex", `echo 'Logged in using ChatGPT'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedIn {
		t.Error("ChatGPT marker should be LoggedIn")
	}

	stubBin(t, dir, "codex", `echo 'Logged in using an API key - sk-test'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedIn {
		t.Errorf("API key login = %v, want LoggedIn", res)
	}
	stubBin(t, dir, "codex", `echo 'Logged in using access token'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedIn {
		t.Errorf("access token login = %v, want LoggedIn", res)
	}
	stubBin(t, dir, "codex", "echo 'Logged in using ChatGPT'\nexit 1")
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != NoAnswer {
		t.Errorf("failed login command = %v, want NoAnswer", res)
	}

	// Only an explicit logged-out answer establishes absence of credentials.
	stubBin(t, dir, "codex", `echo 'Not logged in'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedOut {
		t.Error("explicit Not logged in should be LoggedOut")
	}
	for _, output := range []string{"warning: config could not be loaded", "unexpected status", "error: permission denied"} {
		stubBin(t, dir, "codex", "echo '"+output+"'\nexit 1")
		if res, raw := Run(context.Background(), chat.ProtocolCodex); res != NoAnswer {
			t.Errorf("output %q = %v, want NoAnswer", raw, res)
		}
	}

	stubBin(t, dir, "codex", `exit 1`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != NoAnswer {
		t.Error("no output should be NoAnswer")
	}
}

func TestRunUnknownProtocol(t *testing.T) {
	if res, _ := Run(context.Background(), "opencode"); res != NoAnswer {
		t.Error("unknown protocol should be NoAnswer")
	}
}
