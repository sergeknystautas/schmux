package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestAppendImagePathsToPrompt(t *testing.T) {
	prompt := "Build a login page"
	paths := []string{"/ws/.schmux/attachments/img-abc.png", "/ws/.schmux/attachments/img-def.png"}
	result := appendImagePathsToPrompt(prompt, paths)

	if !strings.HasPrefix(result, "Build a login page") {
		t.Error("original prompt should be preserved")
	}
	if !strings.Contains(result, "Image #1: /ws/.schmux/attachments/img-abc.png") {
		t.Error("missing image #1")
	}
	if !strings.Contains(result, "Image #2: /ws/.schmux/attachments/img-def.png") {
		t.Error("missing image #2")
	}
}

func TestAppendImagePathsToPrompt_Empty(t *testing.T) {
	prompt := "Build a login page"
	result := appendImagePathsToPrompt(prompt, nil)
	if result != prompt {
		t.Errorf("expected unmodified prompt, got %q", result)
	}
}
