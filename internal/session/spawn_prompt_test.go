package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/events"
)

func TestSpawnPromptWrittenToEventsFile(t *testing.T) {
	// Simulate what Spawn does: create events dir, write initial event
	tmpDir := t.TempDir()
	eventsDir := filepath.Join(tmpDir, ".schmux", "events")
	if err := os.MkdirAll(eventsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-workspace-abc12345"
	eventsFile := filepath.Join(eventsDir, sessionID+".jsonl")
	prompt := "Implement OAuth2 token refresh with full error handling"

	evt := events.StatusEvent{
		Type:    "status",
		State:   "working",
		Message: "Session spawned",
		Intent:  prompt,
	}
	if err := events.AppendEvent(eventsFile, evt); err != nil {
		t.Fatal(err)
	}

	// Read and verify
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatal(err)
	}

	var parsed events.StatusEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.State != "working" {
		t.Errorf("expected state working, got %s", parsed.State)
	}
	if parsed.Intent != prompt {
		t.Errorf("expected full prompt in intent, got %q", parsed.Intent)
	}
	if parsed.Message != "Session spawned" {
		t.Errorf("expected 'Session spawned', got %q", parsed.Message)
	}
}
