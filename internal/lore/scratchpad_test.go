package lore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseEntry(t *testing.T) {
	line := `{"ts":"2026-02-13T14:32:00Z","ws":"ws-abc","agent":"claude-code","type":"operational","text":"use go run ./cmd/build-dashboard"}`
	entry, err := ParseEntry(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Workspace != "ws-abc" {
		t.Errorf("expected ws-abc, got %s", entry.Workspace)
	}
	if entry.Agent != "claude-code" {
		t.Errorf("expected claude-code, got %s", entry.Agent)
	}
	if entry.Type != "operational" {
		t.Errorf("expected operational, got %s", entry.Type)
	}
	if entry.Text != "use go run ./cmd/build-dashboard" {
		t.Errorf("unexpected text: %s", entry.Text)
	}
}

func TestParseStateChange(t *testing.T) {
	line := `{"ts":"2026-02-13T15:00:00Z","state_change":"proposed","entry_ts":"2026-02-13T14:32:00Z","proposal_id":"prop-123"}`
	entry, err := ParseEntry(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.StateChange != "proposed" {
		t.Errorf("expected proposed, got %s", entry.StateChange)
	}
	if entry.ProposalID != "prop-123" {
		t.Errorf("expected prop-123, got %s", entry.ProposalID)
	}
}

func TestReadEntries_FiltersRaw(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")
	content := `{"ts":"2026-02-13T14:32:00Z","ws":"ws-abc","agent":"claude-code","type":"operational","text":"fact one"}
{"ts":"2026-02-13T14:33:00Z","ws":"ws-abc","agent":"claude-code","type":"codebase","text":"fact two"}
{"ts":"2026-02-13T15:00:00Z","state_change":"proposed","entry_ts":"2026-02-13T14:32:00Z","proposal_id":"prop-123"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadEntries(path, FilterRaw())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "fact one" has a state_change to proposed, so only "fact two" is raw
	if len(entries) != 1 {
		t.Fatalf("expected 1 raw entry, got %d", len(entries))
	}
	if entries[0].Text != "fact two" {
		t.Errorf("expected 'fact two', got %s", entries[0].Text)
	}
}

func TestAppendEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	entry := Entry{
		Timestamp: time.Date(2026, 2, 13, 14, 32, 0, 0, time.UTC),
		Workspace: "ws-abc",
		Agent:     "claude-code",
		Type:      "operational",
		Text:      "test fact",
	}
	if err := AppendEntry(path, entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d; content: %s", len(entries), string(data))
	}
	if entries[0].Text != "test fact" {
		t.Errorf("expected 'test fact', got %s", entries[0].Text)
	}
}
