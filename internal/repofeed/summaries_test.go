//go:build !norepofeed

package repofeed

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSummaryCache_GetSetDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summaries.json")

	sc := &SummaryCache{
		entries: make(map[string]*SummaryEntry),
		path:    path,
	}

	// Initially empty
	if got := sc.Get("ws-001"); got != nil {
		t.Fatal("expected nil for missing workspace")
	}

	// Set and get
	entry := &SummaryEntry{
		Summary:        "Fixing auth timeout",
		PromptsHash:    "abc123",
		LastSummarized: time.Now(),
	}
	sc.Set("ws-001", entry)

	got := sc.Get("ws-001")
	if got == nil {
		t.Fatal("expected non-nil after Set")
	}
	if got.Summary != "Fixing auth timeout" {
		t.Errorf("summary = %q, want 'Fixing auth timeout'", got.Summary)
	}
	if got.PromptsHash != "abc123" {
		t.Errorf("hash = %q, want 'abc123'", got.PromptsHash)
	}

}

func TestSummaryCache_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summaries.json")

	// Write
	sc1 := &SummaryCache{
		entries: make(map[string]*SummaryEntry),
		path:    path,
	}
	sc1.Set("ws-001", &SummaryEntry{
		Summary:        "Adding dark mode",
		PromptsHash:    "def456",
		LastSummarized: time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC),
	})

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	// Load from disk
	sc2 := &SummaryCache{
		entries: make(map[string]*SummaryEntry),
		path:    path,
	}
	sc2.load()

	got := sc2.Get("ws-001")
	if got == nil {
		t.Fatal("expected entry after reload")
	}
	if got.Summary != "Adding dark mode" {
		t.Errorf("summary = %q after reload", got.Summary)
	}
}
