//go:build !nomodelregistry

package models

import (
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

func TestLastCheckedSeededFromCache(t *testing.T) {
	dir := t.TempDir()
	// A minimal valid models.dev payload for a supported provider.
	payload := []byte(`{"anthropic":{"name":"Anthropic","models":{"claude-x":{"id":"claude-x","name":"Claude X","tool_call":true,"release_date":"2999-01-01","modalities":{"output":["text"]}}}}}`)
	if err := SaveCache(dir, payload); err != nil {
		t.Fatal(err)
	}
	m := New(nil, nil, dir, log.New(os.Stderr))
	// Seed happens during StartBackgroundFetch's cache load; call the seed path directly.
	fetched, err := CacheFetchedAt(dir)
	if err != nil {
		t.Fatalf("CacheFetchedAt: %v", err)
	}
	if fetched.IsZero() {
		t.Fatal("expected non-zero fetched time from freshly saved cache")
	}
	m.mu.Lock()
	m.lastFetchedAt = fetched
	m.mu.Unlock()
	if got := m.LastChecked(); got == "" {
		t.Fatal("LastChecked returned empty after seeding")
	}
	if _, err := time.Parse(time.RFC3339, m.LastChecked()); err != nil {
		t.Fatalf("LastChecked not RFC3339: %q (%v)", m.LastChecked(), err)
	}
}

func TestLastCheckedEmptyWhenNeverFetched(t *testing.T) {
	m := New(nil, nil, t.TempDir(), log.New(os.Stderr))
	if got := m.LastChecked(); got != "" {
		t.Fatalf("expected empty LastChecked, got %q", got)
	}
}
