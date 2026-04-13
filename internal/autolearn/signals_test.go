package autolearn

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/events"
)

func TestReadEntries_FiltersRaw(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.jsonl")
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

func TestReadEntries_NonexistentFile(t *testing.T) {
	entries, err := ReadEntries("/nonexistent/path.jsonl", nil)
	if err != nil {
		t.Fatalf("expected nil error for nonexistent file, got %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil entries for nonexistent file, got %v", entries)
	}
}

func TestReadEntries_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.jsonl")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadEntries(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestFilterRaw(t *testing.T) {
	baseTime := time.Date(2026, 2, 13, 14, 0, 0, 0, time.UTC)

	entries := []Entry{
		{Timestamp: baseTime, Type: "operational", Text: "fact one"},
		{Timestamp: baseTime.Add(time.Minute), Type: "codebase", Text: "fact two"},
		{Timestamp: baseTime.Add(time.Hour), StateChange: "proposed", EntryTS: baseTime.Format(time.RFC3339)},
	}

	filter := FilterRaw()
	result := filter(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 raw entry, got %d", len(result))
	}
	if result[0].Text != "fact two" {
		t.Errorf("expected 'fact two', got %s", result[0].Text)
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

func TestCollectIntentSignals_Empty(t *testing.T) {
	dir := t.TempDir()
	signals, err := CollectIntentSignals([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals for empty dir, got %d", len(signals))
	}
}

func TestCollectIntentSignals_WithEvents(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	data := `{"ts":"2026-02-25T10:00:00Z","type":"status","state":"working","intent":"fix lint errors in src/"}
{"ts":"2026-02-25T11:00:00Z","type":"status","state":"completed","message":"done"}
{"ts":"2026-02-25T12:00:00Z","type":"status","state":"working","intent":"fix lint errors in src/"}
{"ts":"2026-02-26T09:00:00Z","type":"status","state":"working","intent":"add tests for auth"}
`
	os.WriteFile(filepath.Join(eventsDir, "session1.jsonl"), []byte(data), 0644)

	signals, err := CollectIntentSignals([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 unique signals, got %d", len(signals))
	}

	// Should be sorted by timestamp descending
	if signals[0].Text != "add tests for auth" {
		t.Errorf("first signal = %q, want 'add tests for auth' (most recent)", signals[0].Text)
	}
	if signals[0].Count != 1 {
		t.Errorf("first count = %d, want 1", signals[0].Count)
	}

	if signals[1].Text != "fix lint errors in src/" {
		t.Errorf("second signal = %q, want 'fix lint errors in src/'", signals[1].Text)
	}
	if signals[1].Count != 2 {
		t.Errorf("second count = %d, want 2", signals[1].Count)
	}
}

func TestCollectIntentSignals_MultipleWorkspaces(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	eventsDir1 := filepath.Join(dir1, ".schmux", "events")
	eventsDir2 := filepath.Join(dir2, ".schmux", "events")
	os.MkdirAll(eventsDir1, 0755)
	os.MkdirAll(eventsDir2, 0755)

	os.WriteFile(filepath.Join(eventsDir1, "s1.jsonl"),
		[]byte(`{"ts":"2026-02-25T10:00:00Z","type":"status","state":"working","intent":"shared prompt"}`+"\n"), 0644)
	os.WriteFile(filepath.Join(eventsDir2, "s2.jsonl"),
		[]byte(`{"ts":"2026-02-26T10:00:00Z","type":"status","state":"working","intent":"shared prompt"}`+"\n"), 0644)

	signals, err := CollectIntentSignals([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// IntentSignal deduplicates by (text, workspace) — different workspace dirs = 2 signals
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals (different workspaces), got %d", len(signals))
	}
}

func TestCollectIntentSignals_SkipsNonStatus(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	data := `{"ts":"2026-02-25T10:00:00Z","type":"failure","tool":"bash","error":"oops"}
{"ts":"2026-02-25T11:00:00Z","type":"reflection","text":"learned something"}
`
	os.WriteFile(filepath.Join(eventsDir, "s1.jsonl"), []byte(data), 0644)

	signals, err := CollectIntentSignals([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals (no status events), got %d", len(signals))
	}
}

func TestCollectIntentSignals_SkipsEmptyIntent(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	data := `{"ts":"2026-02-25T10:00:00Z","type":"status","state":"working","intent":""}
{"ts":"2026-02-25T11:00:00Z","type":"status","state":"completed","message":"done"}
`
	os.WriteFile(filepath.Join(eventsDir, "s1.jsonl"), []byte(data), 0644)

	signals, err := CollectIntentSignals([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals (empty intents), got %d", len(signals))
	}
}

func TestStatePath(t *testing.T) {
	path, err := StatePath("test-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %s", path)
	}
	expectedSuffix := filepath.Join("test-repo", "state.jsonl")
	if !filepath.IsAbs(path) || !containsPath(path, expectedSuffix) {
		t.Errorf("expected path ending with %s, got %s", expectedSuffix, path)
	}
	// Clean up parent dir
	os.RemoveAll(filepath.Dir(path))
}

func TestStateDir(t *testing.T) {
	dir, err := StateDir("test-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %s", dir)
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

func TestStateDir_InvalidRepoName(t *testing.T) {
	_, err := StateDir("../evil")
	if err == nil {
		t.Fatal("expected error for path traversal repo name")
	}

	_, err = StateDir("evil/path")
	if err == nil {
		t.Fatal("expected error for repo name with slash")
	}
}

// containsPath checks if path contains the given suffix.
func containsPath(path, suffix string) bool {
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}
