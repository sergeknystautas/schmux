package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestNewChatStorageCleaner_BuildsRootAndCallbacks(t *testing.T) {
	cfg := &config.Config{}
	schmuxDir := t.TempDir()
	schmuxdir.Set(schmuxDir)
	t.Cleanup(func() { schmuxdir.Set("") })

	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	if err := st.AddSession(state.Session{ID: "s1", WorkspaceID: "ws-1", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{ID: "s2", WorkspaceID: "ws-2", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}

	cleaner, activeIDs, root, maxMB := newChatStorageCleaner(cfg, st, schmuxDir)
	if cleaner == nil {
		t.Fatal("expected cleaner, got nil")
	}
	expectedRoot := schmuxDir + "/chat"
	if root != expectedRoot {
		t.Errorf("root = %q, want %q", root, expectedRoot)
	}
	if _, ok := activeIDs["s1"]; !ok {
		t.Error("active set missing s1")
	}
	if _, ok := activeIDs["s2"]; !ok {
		t.Error("active set missing s2")
	}
	if got := cleaner.RunOnce(context.Background()); got.LastCleanup == nil {
		t.Error("RunOnce must populate LastCleanup even on no-op pass")
	}
	// MaxMB closure reads through to default 4096.
	if maxMB != 4096 {
		t.Errorf("default maxMB = %d, want 4096", maxMB)
	}
}

func TestNewChatStorageCleaner_ActiveSetIncludesAllSessions(t *testing.T) {
	cfg := &config.Config{}
	schmuxDir := t.TempDir()
	schmuxdir.Set(schmuxDir)
	t.Cleanup(func() { schmuxdir.Set("") })

	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)
	// All session IDs in state go into the active set. The cleaner is
	// only called against the chat subtree, so a non-chat session whose
	// ID happens to collide with a chat session directory would still
	// be safe — the chat session directory is the one we protect.
	if err := st.AddSession(state.Session{ID: "term1", WorkspaceID: "ws-1", Kind: ""}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{ID: "chat1", WorkspaceID: "ws-1", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}

	_, activeIDs, _, _ := newChatStorageCleaner(cfg, st, schmuxDir)
	if _, ok := activeIDs["term1"]; !ok {
		t.Error("terminal session must appear in active set (cleaner protects by ID)")
	}
	if _, ok := activeIDs["chat1"]; !ok {
		t.Error("chat session must appear in active set")
	}
}

func TestNewChatStorageCleaner_LoopHonorsCancellation(t *testing.T) {
	cfg := &config.Config{}
	schmuxDir := t.TempDir()
	schmuxdir.Set(schmuxDir)
	t.Cleanup(func() { schmuxdir.Set("") })

	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cleaner, _, _, _ := newChatStorageCleaner(cfg, st, schmuxDir)
	// A short-tick loop must return promptly on a pre-cancelled context.
	done := make(chan struct{})
	go func() {
		runChatStorageLoop(ctx, cleaner, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit on cancelled context")
	}
}
