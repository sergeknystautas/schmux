package lore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/events"
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


func TestLoreStateDir(t *testing.T) {
	dir, err := LoreStateDir("test-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %s", dir)
	}
	if !strings.Contains(dir, filepath.Join(".schmux", "lore", "test-repo")) {
		t.Errorf("expected path containing .schmux/lore/test-repo, got %s", dir)
	}
	// Directory should exist
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("should be a directory")
	}
	// Clean up
	os.RemoveAll(dir)
}

func TestLoreStatePath(t *testing.T) {
	path, err := LoreStatePath("test-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("test-repo", "state.jsonl")) {
		t.Errorf("expected path ending with test-repo/state.jsonl, got %s", path)
	}
	// Clean up parent dir
	os.RemoveAll(filepath.Dir(path))
}


func TestParseFailureEntry(t *testing.T) {
	line := `{"ts":"2026-02-18T10:30:00Z","ws":"ws-1","agent":"claude-code","type":"failure","tool":"Bash","input_summary":"npm run build","error_summary":"Missing script","category":"wrong_command"}`
	entry, err := ParseEntry(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Type != "failure" {
		t.Errorf("expected type=failure, got %s", entry.Type)
	}
	if entry.Tool != "Bash" {
		t.Errorf("expected tool=Bash, got %s", entry.Tool)
	}
	if entry.InputSummary != "npm run build" {
		t.Errorf("expected input_summary='npm run build', got %s", entry.InputSummary)
	}
	if entry.ErrorSummary != "Missing script" {
		t.Errorf("expected error_summary='Missing script', got %s", entry.ErrorSummary)
	}
	if entry.Category != "wrong_command" {
		t.Errorf("expected category=wrong_command, got %s", entry.Category)
	}
}

func TestEntryKey_ReflectionEntry(t *testing.T) {
	e := Entry{Type: "reflection", Text: "use go run ./cmd/build-dashboard"}
	key := e.EntryKey()
	if key != "use go run ./cmd/build-dashboard" {
		t.Errorf("expected text as key, got %q", key)
	}
}

func TestEntryKey_FailureEntry(t *testing.T) {
	e := Entry{Type: "failure", Tool: "Bash", InputSummary: "npm run build", ErrorSummary: "Missing script"}
	key := e.EntryKey()
	if key != "Bash: npm run build" {
		t.Errorf("expected 'Bash: npm run build', got %q", key)
	}
}

func TestEntryKey_FailureEntryNoTool(t *testing.T) {
	e := Entry{Type: "failure", InputSummary: "something"}
	key := e.EntryKey()
	if key != "something" {
		t.Errorf("expected 'something', got %q", key)
	}
}


func TestFilterByParams(t *testing.T) {
	t.Parallel()
	baseTime := time.Date(2026, 2, 13, 14, 0, 0, 0, time.UTC)

	entries := []Entry{
		{Timestamp: baseTime, Agent: "claude-code", Type: "operational", Text: "fact one"},
		{Timestamp: baseTime.Add(time.Minute), Agent: "codex", Type: "codebase", Text: "fact two"},
		{Timestamp: baseTime.Add(2 * time.Minute), Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "npm build"},
		{Timestamp: baseTime.Add(time.Hour), StateChange: "proposed", EntryTS: baseTime.Format(time.RFC3339)},
	}

	tests := []struct {
		name      string
		state     string
		agent     string
		entryType string
		limit     int
		wantCount int
		wantTexts []string
	}{
		{
			name:      "no filters returns all non-state-change entries",
			wantCount: 3,
		},
		{
			name:      "filter by agent",
			agent:     "claude-code",
			wantCount: 2,
		},
		{
			name:      "filter by type",
			entryType: "codebase",
			wantCount: 1,
			wantTexts: []string{"fact two"},
		},
		{
			name:      "filter by state=raw excludes proposed entries",
			state:     "raw",
			wantCount: 2, // fact two (raw) + failure (raw), fact one is proposed
		},
		{
			name:      "filter by state=proposed",
			state:     "proposed",
			wantCount: 1,
			wantTexts: []string{"fact one"},
		},
		{
			name:      "limit restricts results",
			limit:     1,
			wantCount: 1,
		},
		{
			name:      "combined filters",
			agent:     "claude-code",
			state:     "raw",
			wantCount: 1, // only the failure entry (claude-code + raw)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := FilterByParams(tt.state, tt.agent, tt.entryType, tt.limit)
			result := filter(entries)
			if len(result) != tt.wantCount {
				texts := make([]string, len(result))
				for i, e := range result {
					texts[i] = e.EntryKey()
				}
				t.Fatalf("got %d entries %v, want %d", len(result), texts, tt.wantCount)
			}
			if tt.wantTexts != nil {
				for i, want := range tt.wantTexts {
					if i >= len(result) {
						break
					}
					got := result[i].Text
					if got != want {
						t.Errorf("result[%d].Text = %q, want %q", i, got, want)
					}
				}
			}
		})
	}
}

func TestMarkEntriesByTextFromEntries(t *testing.T) {
	t.Parallel()
	baseTime := time.Date(2026, 2, 13, 14, 0, 0, 0, time.UTC)

	sourceEntries := []Entry{
		{Timestamp: baseTime, Type: "operational", Text: "fact one"},
		{Timestamp: baseTime.Add(time.Minute), Type: "codebase", Text: "fact two"},
		// state-change records should be skipped
		{Timestamp: baseTime.Add(time.Hour), StateChange: "proposed", EntryTS: baseTime.Format(time.RFC3339)},
	}

	dir := t.TempDir()
	destPath := filepath.Join(dir, "state.jsonl")

	err := MarkEntriesByTextFromEntries(sourceEntries, destPath, "applied", []string{"fact one", "nonexistent"}, "prop-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read the dest file - should have one state-change record for "fact one"
	entries, err := ReadEntries(destPath, nil)
	if err != nil {
		t.Fatalf("unexpected error reading dest: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 state-change record, got %d", len(entries))
	}
	if entries[0].StateChange != "applied" {
		t.Errorf("expected state_change 'applied', got %q", entries[0].StateChange)
	}
	if entries[0].ProposalID != "prop-test" {
		t.Errorf("expected proposal_id 'prop-test', got %q", entries[0].ProposalID)
	}
}

func TestEventLineToEntry_Failure(t *testing.T) {
	t.Parallel()
	data := []byte(`{"ts":"2026-02-13T14:00:00Z","type":"failure","tool":"Bash","input":"npm build","error":"exit code 1","category":"build"}`)
	el := events.EventLine{
		RawEvent: events.RawEvent{Ts: "2026-02-13T14:00:00Z", Type: "failure"},
		Data:     data,
	}

	entry := eventLineToEntry(el, "sess-1", "ws-1")

	if entry.Type != "failure" {
		t.Errorf("Type = %q, want 'failure'", entry.Type)
	}
	if entry.Session != "sess-1" {
		t.Errorf("Session = %q, want 'sess-1'", entry.Session)
	}
	if entry.Workspace != "ws-1" {
		t.Errorf("Workspace = %q, want 'ws-1'", entry.Workspace)
	}
	if entry.Tool != "Bash" {
		t.Errorf("Tool = %q, want 'Bash'", entry.Tool)
	}
	if entry.InputSummary != "npm build" {
		t.Errorf("InputSummary = %q, want 'npm build'", entry.InputSummary)
	}
	if entry.ErrorSummary != "exit code 1" {
		t.Errorf("ErrorSummary = %q, want 'exit code 1'", entry.ErrorSummary)
	}
	if entry.Category != "build" {
		t.Errorf("Category = %q, want 'build'", entry.Category)
	}
	wantTS := time.Date(2026, 2, 13, 14, 0, 0, 0, time.UTC)
	if !entry.Timestamp.Equal(wantTS) {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, wantTS)
	}
}

func TestEventLineToEntry_Reflection(t *testing.T) {
	t.Parallel()
	data := []byte(`{"ts":"2026-02-13T15:00:00Z","type":"reflection","text":"always run tests before committing"}`)
	el := events.EventLine{
		RawEvent: events.RawEvent{Ts: "2026-02-13T15:00:00Z", Type: "reflection"},
		Data:     data,
	}

	entry := eventLineToEntry(el, "sess-2", "ws-2")

	if entry.Type != "reflection" {
		t.Errorf("Type = %q, want 'reflection'", entry.Type)
	}
	if entry.Text != "always run tests before committing" {
		t.Errorf("Text = %q, want 'always run tests before committing'", entry.Text)
	}
}

func TestEventLineToEntry_Friction(t *testing.T) {
	t.Parallel()
	data := []byte(`{"ts":"2026-02-13T16:00:00Z","type":"friction","text":"build system is confusing"}`)
	el := events.EventLine{
		RawEvent: events.RawEvent{Ts: "2026-02-13T16:00:00Z", Type: "friction"},
		Data:     data,
	}

	entry := eventLineToEntry(el, "sess-3", "ws-3")

	if entry.Type != "friction" {
		t.Errorf("Type = %q, want 'friction'", entry.Type)
	}
	if entry.Text != "build system is confusing" {
		t.Errorf("Text = %q, want 'build system is confusing'", entry.Text)
	}
}

func TestEventLineToEntry_UnknownType(t *testing.T) {
	t.Parallel()
	data := []byte(`{"ts":"2026-02-13T17:00:00Z","type":"unknown"}`)
	el := events.EventLine{
		RawEvent: events.RawEvent{Ts: "2026-02-13T17:00:00Z", Type: "unknown"},
		Data:     data,
	}

	entry := eventLineToEntry(el, "sess-4", "ws-4")

	// Unknown type should still populate base fields
	if entry.Type != "unknown" {
		t.Errorf("Type = %q, want 'unknown'", entry.Type)
	}
	if entry.Tool != "" || entry.Text != "" {
		t.Errorf("expected empty Tool and Text for unknown type, got Tool=%q Text=%q", entry.Tool, entry.Text)
	}
}

func TestEventLineToEntry_MalformedData(t *testing.T) {
	t.Parallel()
	el := events.EventLine{
		RawEvent: events.RawEvent{Ts: "2026-02-13T18:00:00Z", Type: "failure"},
		Data:     []byte(`not valid json`),
	}

	entry := eventLineToEntry(el, "sess-5", "ws-5")

	// Should still produce an entry with base fields, just empty tool/error
	if entry.Type != "failure" {
		t.Errorf("Type = %q, want 'failure'", entry.Type)
	}
	if entry.Tool != "" {
		t.Errorf("expected empty Tool for malformed data, got %q", entry.Tool)
	}
}

func TestMarkEntriesDirect(t *testing.T) {
	t.Parallel()
	baseTime := time.Date(2026, 2, 13, 14, 0, 0, 0, time.UTC)

	entries := []Entry{
		{Timestamp: baseTime, Type: "reflection", Text: "use go run instead"},
		{Timestamp: baseTime.Add(time.Minute), Type: "failure", Tool: "Bash", InputSummary: "npm run build"},
		{Timestamp: baseTime.Add(2 * time.Minute), Type: "friction", Text: "slow build"},
		// state-change records should be skipped
		{Timestamp: baseTime.Add(time.Hour), StateChange: "proposed", EntryTS: "some-old-ts"},
	}

	dir := t.TempDir()
	destPath := filepath.Join(dir, "state.jsonl")

	err := MarkEntriesDirect(entries, destPath, "proposed", "prop-direct")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 3 state-change records (one per non-state-change entry)
	stateEntries, err := ReadEntries(destPath, nil)
	if err != nil {
		t.Fatalf("unexpected error reading dest: %v", err)
	}
	if len(stateEntries) != 3 {
		t.Fatalf("expected 3 state-change records, got %d", len(stateEntries))
	}
	for _, e := range stateEntries {
		if e.StateChange != "proposed" {
			t.Errorf("expected state_change 'proposed', got %q", e.StateChange)
		}
		if e.ProposalID != "prop-direct" {
			t.Errorf("expected proposal_id 'prop-direct', got %q", e.ProposalID)
		}
	}
}


func TestMarkEntriesDirect_DeduplicatesByTimestamp(t *testing.T) {
	t.Parallel()
	baseTime := time.Date(2026, 2, 13, 14, 0, 0, 0, time.UTC)

	// Two entries with same timestamp — should produce only one state-change record
	entries := []Entry{
		{Timestamp: baseTime, Type: "reflection", Text: "fact A"},
		{Timestamp: baseTime, Type: "friction", Text: "fact B"},
	}

	dir := t.TempDir()
	destPath := filepath.Join(dir, "state.jsonl")

	err := MarkEntriesDirect(entries, destPath, "proposed", "prop-dedup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stateEntries, err := ReadEntries(destPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stateEntries) != 1 {
		t.Fatalf("expected 1 state-change record (deduped by timestamp), got %d", len(stateEntries))
	}
}
