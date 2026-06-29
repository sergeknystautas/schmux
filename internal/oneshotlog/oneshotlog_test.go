package oneshotlog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestPath(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	want := filepath.Join(schmuxdir.LogsDir(), "oneshot.jsonl")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestAppendRoundTrip(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	rec := contracts.OneshotLogRecord{
		TS: "t", Type: "commit-message", Transport: "cli",
		Model: "claude-sonnet-4-6", Workspace: "schmux-9", PromptChars: 12, OK: true,
	}
	if err := Append(rec); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got contracts.OneshotLogRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Model != "claude-sonnet-4-6" || got.Type != "commit-message" || !got.OK {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
