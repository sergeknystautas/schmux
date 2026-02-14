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

func TestMarkEntriesByText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Write two raw entries
	content := `{"ts":"2026-02-13T14:32:00Z","ws":"ws-abc","agent":"claude-code","type":"operational","text":"fact one"}
{"ts":"2026-02-13T14:33:00Z","ws":"ws-abc","agent":"claude-code","type":"codebase","text":"fact two"}
{"ts":"2026-02-13T14:34:00Z","ws":"ws-def","agent":"codex","type":"operational","text":"fact three"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Mark "fact one" and "fact three" as proposed
	err := MarkEntriesByText(path, "proposed", []string{"fact one", "fact three"}, "prop-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only "fact two" should remain as raw
	raw, err := ReadEntries(path, FilterRaw())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 raw entry, got %d", len(raw))
	}
	if raw[0].Text != "fact two" {
		t.Errorf("expected 'fact two', got %s", raw[0].Text)
	}

	// All entries (including state changes) should be readable
	all, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 3 original entries + 2 state-change records = 5
	if len(all) != 5 {
		t.Fatalf("expected 5 total entries, got %d", len(all))
	}

	// Verify state-change records have correct proposal ID
	stateChanges := 0
	for _, e := range all {
		if e.StateChange == "proposed" {
			stateChanges++
			if e.ProposalID != "prop-test" {
				t.Errorf("expected proposal_id 'prop-test', got %s", e.ProposalID)
			}
		}
	}
	if stateChanges != 2 {
		t.Errorf("expected 2 state changes, got %d", stateChanges)
	}
}

func TestMarkEntriesByText_NoMatchingEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	content := `{"ts":"2026-02-13T14:32:00Z","ws":"ws-abc","agent":"claude-code","type":"operational","text":"fact one"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Mark non-existent text — should not error
	err := MarkEntriesByText(path, "proposed", []string{"nonexistent fact"}, "prop-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "fact one" should remain raw (no state change added)
	raw, err := ReadEntries(path, FilterRaw())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 raw entry, got %d", len(raw))
	}
}

func TestMarkEntriesByText_SkipsAlreadyChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Entry with existing state change
	content := `{"ts":"2026-02-13T14:32:00Z","ws":"ws-abc","agent":"claude-code","type":"operational","text":"fact one"}
{"ts":"2026-02-13T15:00:00Z","state_change":"proposed","entry_ts":"2026-02-13T14:32:00Z","proposal_id":"prop-old"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Try to mark "fact one" as applied — should still append since MarkEntriesByText
	// only skips state-change records themselves, not already-changed entries
	err := MarkEntriesByText(path, "applied", []string{"fact one"}, "prop-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Original entry + old state change + new state change = 3
	if len(all) != 3 {
		t.Fatalf("expected 3 total entries, got %d", len(all))
	}
}
