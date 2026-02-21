// Package event provides the unified event system for agent-to-schmux communication.
// Each session writes to its own append-only JSONL file at <workspace>/.schmux/events/<session-id>.jsonl.
// Consumers subscribe to event types they care about (status, failure, reflection, friction).
package event

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/signal"
)

// Event represents a single event in the unified event file.
// All events share the same envelope; type-specific fields are omitted when empty.
type Event struct {
	Timestamp time.Time `json:"ts"`
	Type      string    `json:"type"` // "status", "failure", "reflection", "friction"

	// Status fields
	State    string `json:"state,omitempty"`
	Message  string `json:"message,omitempty"`
	Intent   string `json:"intent,omitempty"`
	Blockers string `json:"blockers,omitempty"`

	// Failure fields
	Tool     string `json:"tool,omitempty"`
	Input    string `json:"input,omitempty"`
	Error    string `json:"error,omitempty"`
	Category string `json:"category,omitempty"`

	// Reflection/friction fields
	Text string `json:"text,omitempty"`
}

// ParseEvent parses a single JSONL line into an Event.
func ParseEvent(line string) (Event, error) {
	var e Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &e); err != nil {
		return Event{}, fmt.Errorf("failed to parse event: %w", err)
	}
	return e, nil
}

// ReadEvents reads all events from a JSONL event file.
// Returns nil, nil if the file does not exist.
func ReadEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		e, err := ParseEvent(line)
		if err != nil {
			fmt.Printf("[event] skipping malformed event: %v\n", err)
			continue
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

// ReadEventsFiltered reads events from a JSONL event file, returning only those
// matching the specified types.
func ReadEventsFiltered(path string, types ...string) ([]Event, error) {
	all, err := ReadEvents(path)
	if err != nil {
		return nil, err
	}
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	var filtered []Event
	for _, e := range all {
		if typeSet[e.Type] {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

// LatestStatus scans the event file for the last "status" event and returns it.
// Returns nil if no status event is found or the file doesn't exist.
func LatestStatus(path string) *Event {
	events, err := ReadEvents(path)
	if err != nil || len(events) == 0 {
		return nil
	}
	var last *Event
	for i := range events {
		if events[i].Type == "status" {
			last = &events[i]
		}
	}
	return last
}

// EventFilePath returns the path to a session's event file.
func EventFilePath(workspacePath, sessionID string) string {
	return filepath.Join(workspacePath, ".schmux", "events", sessionID+".jsonl")
}

// ToSignal converts a status event to a signal.Signal for backward compatibility.
// Returns nil if the event is not a status event or has an invalid state.
func ToSignal(e Event) *signal.Signal {
	if e.Type != "status" {
		return nil
	}
	if !signal.IsValidState(e.State) {
		return nil
	}
	return &signal.Signal{
		State:     e.State,
		Message:   e.Message,
		Intent:    e.Intent,
		Blockers:  e.Blockers,
		Timestamp: e.Timestamp,
	}
}

// loreRelevantTypes are event types that map to lore entries.
var loreRelevantTypes = map[string]bool{
	"failure":    true,
	"reflection": true,
	"friction":   true,
}

// ReadLoreEventsFromWorkspace reads all lore-relevant events (failure, reflection,
// friction) from all session event files in a workspace.
// Session ID and workspace ID are derived from the file path.
func ReadLoreEventsFromWorkspace(workspacePath, workspaceID string) ([]lore.Entry, error) {
	eventsDir := filepath.Join(workspacePath, ".schmux", "events")
	pattern := filepath.Join(eventsDir, "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob event files: %w", err)
	}

	var entries []lore.Entry
	for _, file := range files {
		sessionID := strings.TrimSuffix(filepath.Base(file), ".jsonl")
		events, err := ReadEventsFiltered(file, "failure", "reflection", "friction")
		if err != nil {
			fmt.Printf("[event] warning: failed to read %s: %v\n", file, err)
			continue
		}
		for _, e := range events {
			entries = append(entries, eventToLoreEntry(e, sessionID, workspaceID))
		}
	}
	return entries, nil
}

// PruneEventFiles removes .jsonl event files in eventsDir that don't belong to
// any active session. Active session IDs are provided as a set.
// Event files are expected to be named "{sessionID}.jsonl".
func PruneEventFiles(eventsDir string, activeIDs map[string]bool) (removed int) {
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		if !activeIDs[sessionID] {
			filePath := filepath.Join(eventsDir, entry.Name())
			if os.Remove(filePath) == nil {
				removed++
			}
		}
	}
	return removed
}

// eventToLoreEntry converts an Event to a lore.Entry.
func eventToLoreEntry(e Event, sessionID, workspaceID string) lore.Entry {
	entry := lore.Entry{
		Timestamp: e.Timestamp,
		Workspace: workspaceID,
		Session:   sessionID,
		Agent:     "claude-code",
		Type:      e.Type,
	}
	switch e.Type {
	case "failure":
		entry.Tool = e.Tool
		entry.InputSummary = e.Input
		entry.ErrorSummary = e.Error
		entry.Category = e.Category
	case "reflection", "friction":
		entry.Text = e.Text
	}
	return entry
}
