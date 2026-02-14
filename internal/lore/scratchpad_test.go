package lore

import (
	"fmt"
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

func TestPruneEntries_AppliedOlderThanMaxAge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Create an entry that was applied 60 days ago
	oldTS := time.Now().Add(-60 * 24 * time.Hour).UTC().Format(time.RFC3339)
	oldStateTS := time.Now().Add(-59 * 24 * time.Hour).UTC().Format(time.RFC3339)

	content := fmt.Sprintf(`{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"operational","text":"old applied fact"}
{"ts":"%s","state_change":"applied","entry_ts":"%s","proposal_id":"prop-1"}
{"ts":"2026-02-13T14:33:00Z","ws":"ws-abc","agent":"claude-code","type":"codebase","text":"recent fact"}
`, oldTS, oldStateTS, oldTS)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 2 {
		t.Errorf("expected 2 pruned (entry + state change), got %d", pruned)
	}

	// Only "recent fact" should remain
	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 remaining entry, got %d", len(entries))
	}
	if entries[0].Text != "recent fact" {
		t.Errorf("expected 'recent fact', got %s", entries[0].Text)
	}
}

func TestPruneEntries_DismissedOlderThanMaxAge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	oldTS := time.Now().Add(-45 * 24 * time.Hour).UTC().Format(time.RFC3339)
	oldStateTS := time.Now().Add(-44 * 24 * time.Hour).UTC().Format(time.RFC3339)

	content := fmt.Sprintf(`{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"codebase","text":"dismissed fact"}
{"ts":"%s","state_change":"dismissed","entry_ts":"%s","proposal_id":"prop-2"}
`, oldTS, oldStateTS, oldTS)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 2 {
		t.Errorf("expected 2 pruned, got %d", pruned)
	}

	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 remaining entries, got %d", len(entries))
	}
}

func TestPruneEntries_RawEntriesNeverPruned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Raw entry (no state change) even if old — should NOT be pruned
	oldTS := time.Now().Add(-90 * 24 * time.Hour).UTC().Format(time.RFC3339)
	content := fmt.Sprintf(`{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"operational","text":"old raw fact"}
`, oldTS)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned (raw entries never pruned), got %d", pruned)
	}

	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestPruneEntries_ProposedEntriesNeverPruned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Proposed entry even if old — should NOT be pruned
	oldTS := time.Now().Add(-90 * 24 * time.Hour).UTC().Format(time.RFC3339)
	oldStateTS := time.Now().Add(-89 * 24 * time.Hour).UTC().Format(time.RFC3339)

	content := fmt.Sprintf(`{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"operational","text":"old proposed fact"}
{"ts":"%s","state_change":"proposed","entry_ts":"%s","proposal_id":"prop-3"}
`, oldTS, oldStateTS, oldTS)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned (proposed entries never pruned), got %d", pruned)
	}

	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestPruneEntries_RecentAppliedNotPruned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Applied entry from 5 days ago — should NOT be pruned with 30-day maxAge
	recentTS := time.Now().Add(-5 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recentStateTS := time.Now().Add(-4 * 24 * time.Hour).UTC().Format(time.RFC3339)

	content := fmt.Sprintf(`{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"operational","text":"recent applied fact"}
{"ts":"%s","state_change":"applied","entry_ts":"%s","proposal_id":"prop-4"}
`, recentTS, recentStateTS, recentTS)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned (recent entries not pruned), got %d", pruned)
	}

	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestPruneEntries_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}
}

func TestPruneEntries_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.jsonl")

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}
}

func TestPruneEntries_NoPrunableEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Only raw entries and proposed entries — nothing to prune
	content := `{"ts":"2026-02-13T14:32:00Z","ws":"ws-abc","agent":"claude-code","type":"operational","text":"fact one"}
{"ts":"2026-02-13T14:33:00Z","ws":"ws-abc","agent":"claude-code","type":"codebase","text":"fact two"}
{"ts":"2026-02-13T15:00:00Z","state_change":"proposed","entry_ts":"2026-02-13T14:32:00Z","proposal_id":"prop-5"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Read file before prune for comparison
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}

	// File should be unchanged (not rewritten)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("file should not have been rewritten when nothing was pruned")
	}
}

func TestPruneEntries_MixedEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lore.jsonl")

	// Mix of old applied (prune), old proposed (keep), raw (keep), recent dismissed (keep)
	oldTS1 := time.Now().Add(-60 * 24 * time.Hour).UTC().Format(time.RFC3339)
	oldStateTS1 := time.Now().Add(-59 * 24 * time.Hour).UTC().Format(time.RFC3339)
	oldTS2 := time.Now().Add(-50 * 24 * time.Hour).UTC().Format(time.RFC3339)
	oldStateTS2 := time.Now().Add(-49 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recentTS := time.Now().Add(-5 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recentStateTS := time.Now().Add(-4 * 24 * time.Hour).UTC().Format(time.RFC3339)

	content := fmt.Sprintf(`{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"operational","text":"old applied"}
{"ts":"%s","state_change":"applied","entry_ts":"%s","proposal_id":"prop-a"}
{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"codebase","text":"old proposed"}
{"ts":"%s","state_change":"proposed","entry_ts":"%s","proposal_id":"prop-b"}
{"ts":"2026-02-13T14:33:00Z","ws":"ws-abc","agent":"claude-code","type":"codebase","text":"raw entry"}
{"ts":"%s","ws":"ws-abc","agent":"claude-code","type":"operational","text":"recent dismissed"}
{"ts":"%s","state_change":"dismissed","entry_ts":"%s","proposal_id":"prop-c"}
`,
		oldTS1, oldStateTS1, oldTS1,
		oldTS2, oldStateTS2, oldTS2,
		recentTS, recentStateTS, recentTS,
	)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneEntries(path, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should prune: old applied entry + its state change = 2
	if pruned != 2 {
		t.Errorf("expected 2 pruned, got %d", pruned)
	}

	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Remaining: old proposed entry + its state change + raw entry + recent dismissed + its state change = 5
	if len(entries) != 5 {
		t.Fatalf("expected 5 remaining entries, got %d", len(entries))
	}

	// Verify the correct entries remain
	texts := make(map[string]bool)
	for _, e := range entries {
		if e.Text != "" {
			texts[e.Text] = true
		}
	}
	if texts["old applied"] {
		t.Error("old applied entry should have been pruned")
	}
	if !texts["old proposed"] {
		t.Error("old proposed entry should have been kept")
	}
	if !texts["raw entry"] {
		t.Error("raw entry should have been kept")
	}
	if !texts["recent dismissed"] {
		t.Error("recent dismissed entry should have been kept")
	}
}
