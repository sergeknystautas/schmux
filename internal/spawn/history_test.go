package spawn

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectPromptHistory_Empty(t *testing.T) {
	dir := t.TempDir()
	result := CollectPromptHistory([]string{dir}, 100)
	if len(result) != 0 {
		t.Errorf("expected 0 entries for empty dir, got %d", len(result))
	}
}

func TestCollectPromptHistory_WithEvents(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	// Write a sample JSONL file.
	data := `{"ts":"2026-02-25T10:00:00Z","type":"status","state":"working","intent":"fix lint errors in src/"}
{"ts":"2026-02-25T11:00:00Z","type":"status","state":"completed","message":"done"}
{"ts":"2026-02-25T12:00:00Z","type":"status","state":"working","intent":"fix lint errors in src/"}
{"ts":"2026-02-26T09:00:00Z","type":"status","state":"working","intent":"add tests for auth"}
`
	os.WriteFile(filepath.Join(eventsDir, "session1.jsonl"), []byte(data), 0644)

	result := CollectPromptHistory([]string{dir}, 100)
	if len(result) != 2 {
		t.Fatalf("expected 2 unique prompts, got %d", len(result))
	}

	// Should be sorted by last_seen descending.
	if result[0].Text != "add tests for auth" {
		t.Errorf("first entry = %q, want 'add tests for auth' (most recent)", result[0].Text)
	}
	if result[0].Count != 1 {
		t.Errorf("first count = %d, want 1", result[0].Count)
	}

	if result[1].Text != "fix lint errors in src/" {
		t.Errorf("second entry = %q, want 'fix lint errors in src/'", result[1].Text)
	}
	if result[1].Count != 2 {
		t.Errorf("second count = %d, want 2", result[1].Count)
	}
}

func TestCollectPromptHistory_MaxResults(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	data := `{"ts":"2026-02-25T10:00:00Z","type":"status","state":"working","intent":"prompt 1"}
{"ts":"2026-02-25T11:00:00Z","type":"status","state":"working","intent":"prompt 2"}
{"ts":"2026-02-25T12:00:00Z","type":"status","state":"working","intent":"prompt 3"}
`
	os.WriteFile(filepath.Join(eventsDir, "session1.jsonl"), []byte(data), 0644)

	result := CollectPromptHistory([]string{dir}, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries (capped), got %d", len(result))
	}
}

func TestCollectPromptHistory_MultipleWorkspaces(t *testing.T) {
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

	result := CollectPromptHistory([]string{dir1, dir2}, 100)
	if len(result) != 1 {
		t.Fatalf("expected 1 deduplicated entry, got %d", len(result))
	}
	if result[0].Count != 2 {
		t.Errorf("count = %d, want 2", result[0].Count)
	}
}

func TestCollectPromptHistory_SkipsNonStatus(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	data := `{"ts":"2026-02-25T10:00:00Z","type":"failure","tool":"bash","error":"oops"}
{"ts":"2026-02-25T11:00:00Z","type":"reflection","text":"learned something"}
`
	os.WriteFile(filepath.Join(eventsDir, "s1.jsonl"), []byte(data), 0644)

	result := CollectPromptHistory([]string{dir}, 100)
	if len(result) != 0 {
		t.Errorf("expected 0 entries (no status events), got %d", len(result))
	}
}
