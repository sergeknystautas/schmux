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

func TestRunCodex(t *testing.T) {
	dir := t.TempDir()
	withStubbedPATH(t, dir)

	stubBin(t, dir, "codex", `echo 'Logged in using ChatGPT'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedIn {
		t.Error("ChatGPT marker should be LoggedIn")
	}

	// Logged-out shape is unobserved in the wild: any non-empty completed
	// output without the marker reads as LoggedOut (the command's whole
	// job is to answer); empty output is NoAnswer — never guess.
	stubBin(t, dir, "codex", `echo 'Not logged in'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedOut {
		t.Error("answered-without-marker should be LoggedOut")
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
