package mdedit

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
)

func TestHubSharesDocumentPerPathAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.md")
	p2 := filepath.Join(dir, "b.md")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewHub(log.NewWithOptions(io.Discard, log.Options{}))
	ok := func() error { return nil }
	a, b, c := newFakeConn(), newFakeConn(), newFakeConn()
	d1, err := hub.Subscribe(p1, ok, a)
	if err != nil {
		t.Fatal(err)
	}
	d1b, err := hub.Subscribe(p1, ok, b)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := hub.Subscribe(p2, ok, c)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d1b {
		t.Fatal("same path produced two documents")
	}
	if d1 == d2 {
		t.Fatal("different paths share a document")
	}
	if n := hub.Count(); n != 2 {
		t.Fatalf("documents = %d, want 2", n)
	}
	hub.Unsubscribe(p1, a)
	if n := hub.Count(); n != 2 {
		t.Fatalf("document removed with a subscriber left")
	}
	hub.Unsubscribe(p1, b)
	if n := hub.Count(); n != 1 {
		t.Fatalf("documents = %d after last unsubscribe, want 1", n)
	}
	hub.Close()
	if n := hub.Count(); n != 0 {
		t.Fatalf("documents = %d after Close, want 0", n)
	}
}

func TestHubSubscribeRefusalLeavesNoDocument(t *testing.T) {
	hub := NewHub(log.NewWithOptions(io.Discard, log.Options{}))
	_, err := hub.Subscribe(filepath.Join(t.TempDir(), "missing.md"), func() error { return nil }, newFakeConn())
	if err == nil {
		t.Fatal("expected refusal for a missing file")
	}
	if n := hub.Count(); n != 0 {
		t.Fatalf("documents = %d after refusal, want 0", n)
	}
}
