package event

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// writeEventLine is a test helper that appends a JSONL line to the given file.
func writeEventLine(t *testing.T, path string, e Event) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open event file for append: %v", err)
	}
	defer f.Close()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		t.Fatalf("failed to write event: %v", err)
	}
}

func TestEventWatcher_LifecycleStartStop(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ew, err := NewEventWatcher("sess-lifecycle", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	ew.Start()

	// Give the goroutine a moment to spin up
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic and should return (not hang)
	done := make(chan struct{})
	go func() {
		ew.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2 seconds")
	}
}

func TestEventWatcher_ReadCurrentReturnsLastStatusEvent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// Write multiple events: status, failure, status
	writeEventLine(t, filePath, Event{
		Timestamp: ts,
		Type:      "status",
		State:     "working",
		Message:   "starting work",
	})
	writeEventLine(t, filePath, Event{
		Timestamp: ts.Add(1 * time.Minute),
		Type:      "failure",
		Tool:      "Bash",
		Error:     "exit 1",
	})
	writeEventLine(t, filePath, Event{
		Timestamp: ts.Add(2 * time.Minute),
		Type:      "status",
		State:     "completed",
		Message:   "done",
	})

	ew, err := NewEventWatcher("sess-readcurrent", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}
	defer ew.Stop()
	ew.Start()

	got := ew.ReadCurrent()
	if got == nil {
		t.Fatal("ReadCurrent returned nil, expected last status event")
	}
	if got.State != "completed" {
		t.Errorf("State = %q, want completed", got.State)
	}
	if got.Message != "done" {
		t.Errorf("Message = %q, want done", got.Message)
	}
}

func TestEventWatcher_ReadCurrentUpdatesOffset(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// Write initial events
	writeEventLine(t, filePath, Event{
		Timestamp: ts,
		Type:      "status",
		State:     "working",
		Message:   "initial",
	})

	ew, err := NewEventWatcher("sess-offset", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	// Subscribe to status events so we can verify only new ones arrive
	received := make(chan Event, 10)
	ew.Subscribe("status", func(sessionID string, e Event) {
		received <- e
	})

	// ReadCurrent should advance offset past the initial event
	got := ew.ReadCurrent()
	if got == nil {
		t.Fatal("ReadCurrent returned nil")
	}
	if got.State != "working" {
		t.Errorf("State = %q, want working", got.State)
	}

	ew.Start()
	defer ew.Stop()

	// Write a new event after ReadCurrent
	writeEventLine(t, filePath, Event{
		Timestamp: ts.Add(5 * time.Minute),
		Type:      "status",
		State:     "completed",
		Message:   "after-readcurrent",
	})

	// Wait for the new event to be dispatched
	select {
	case e := <-received:
		if e.State != "completed" {
			t.Errorf("dispatched event State = %q, want completed", e.State)
		}
		if e.Message != "after-readcurrent" {
			t.Errorf("dispatched event Message = %q, want after-readcurrent", e.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dispatched event")
	}

	// The old "working" event should NOT have been dispatched since ReadCurrent moved the offset
	select {
	case e := <-received:
		t.Fatalf("unexpected extra event dispatched: %+v", e)
	case <-time.After(300 * time.Millisecond):
		// Good, no extra events
	}
}

func TestEventWatcher_EventDispatch(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	ew, err := NewEventWatcher("sess-dispatch", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	received := make(chan Event, 10)
	ew.Subscribe("status", func(sessionID string, e Event) {
		if sessionID != "sess-dispatch" {
			t.Errorf("sessionID = %q, want sess-dispatch", sessionID)
		}
		received <- e
	})
	ew.Subscribe("failure", func(sessionID string, e Event) {
		received <- e
	})

	ew.Start()
	defer ew.Stop()

	// Write events after start
	writeEventLine(t, filePath, Event{
		Timestamp: ts,
		Type:      "status",
		State:     "working",
		Message:   "dispatched event",
	})
	writeEventLine(t, filePath, Event{
		Timestamp: ts.Add(1 * time.Minute),
		Type:      "failure",
		Tool:      "Bash",
		Error:     "command not found",
	})

	// Collect both events
	var events []Event
	for i := 0; i < 2; i++ {
		select {
		case e := <-received:
			events = append(events, e)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d/2", i+1)
		}
	}

	// Verify both events were dispatched with correct data
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Find status and failure events (order within a single debounce batch is deterministic)
	var gotStatus, gotFailure bool
	for _, e := range events {
		switch e.Type {
		case "status":
			gotStatus = true
			if e.State != "working" {
				t.Errorf("status State = %q, want working", e.State)
			}
			if e.Message != "dispatched event" {
				t.Errorf("status Message = %q, want 'dispatched event'", e.Message)
			}
		case "failure":
			gotFailure = true
			if e.Tool != "Bash" {
				t.Errorf("failure Tool = %q, want Bash", e.Tool)
			}
			if e.Error != "command not found" {
				t.Errorf("failure Error = %q, want 'command not found'", e.Error)
			}
		}
	}
	if !gotStatus {
		t.Error("did not receive status event")
	}
	if !gotFailure {
		t.Error("did not receive failure event")
	}
}

func TestEventWatcher_SubscribeSpecificEventTypes(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	ew, err := NewEventWatcher("sess-filter", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	// Subscribe to status only
	statusReceived := make(chan Event, 10)
	ew.Subscribe("status", func(sessionID string, e Event) {
		statusReceived <- e
	})

	ew.Start()
	defer ew.Stop()

	// Write both status and failure events
	writeEventLine(t, filePath, Event{
		Timestamp: ts,
		Type:      "status",
		State:     "working",
		Message:   "should receive",
	})
	writeEventLine(t, filePath, Event{
		Timestamp: ts.Add(1 * time.Minute),
		Type:      "failure",
		Tool:      "Bash",
		Error:     "should NOT receive",
	})
	writeEventLine(t, filePath, Event{
		Timestamp: ts.Add(2 * time.Minute),
		Type:      "reflection",
		Text:      "should NOT receive either",
	})

	// We should receive exactly the status event
	select {
	case e := <-statusReceived:
		if e.Type != "status" {
			t.Errorf("received event type = %q, want status", e.Type)
		}
		if e.State != "working" {
			t.Errorf("State = %q, want working", e.State)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for status event")
	}

	// Wait a bit longer and confirm no extra events arrived
	select {
	case e := <-statusReceived:
		t.Fatalf("unexpected extra event received: %+v", e)
	case <-time.After(500 * time.Millisecond):
		// Good, no extra events dispatched to the status handler
	}
}

func TestEventWatcher_MultipleHandlersSameType(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	ew, err := NewEventWatcher("sess-multi", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	handler1Received := make(chan Event, 10)
	handler2Received := make(chan Event, 10)

	ew.Subscribe("status", func(sessionID string, e Event) {
		handler1Received <- e
	})
	ew.Subscribe("status", func(sessionID string, e Event) {
		handler2Received <- e
	})

	ew.Start()
	defer ew.Stop()

	writeEventLine(t, filePath, Event{
		Timestamp: ts,
		Type:      "status",
		State:     "completed",
		Message:   "both handlers should get this",
	})

	// Both handlers should receive the event
	for i, ch := range []chan Event{handler1Received, handler2Received} {
		select {
		case e := <-ch:
			if e.State != "completed" {
				t.Errorf("handler %d: State = %q, want completed", i+1, e.State)
			}
			if e.Message != "both handlers should get this" {
				t.Errorf("handler %d: Message = %q, want 'both handlers should get this'", i+1, e.Message)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("handler %d: timed out waiting for event", i+1)
		}
	}
}

func TestEventWatcher_FileCreatedAfterWatcherStarts(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	// File does NOT exist yet
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("expected file to not exist initially")
	}

	ew, err := NewEventWatcher("sess-latecreate", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	received := make(chan Event, 10)
	ew.Subscribe("status", func(sessionID string, e Event) {
		received <- e
	})

	ew.Start()
	defer ew.Stop()

	// Wait a bit, then create the file and write an event
	time.Sleep(100 * time.Millisecond)

	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	writeEventLine(t, filePath, Event{
		Timestamp: ts,
		Type:      "status",
		State:     "working",
		Message:   "file created after watcher started",
	})

	select {
	case e := <-received:
		if e.State != "working" {
			t.Errorf("State = %q, want working", e.State)
		}
		if e.Message != "file created after watcher started" {
			t.Errorf("Message = %q, want 'file created after watcher started'", e.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event from late-created file")
	}
}

func TestEventWatcher_StopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ew, err := NewEventWatcher("sess-stoptwice", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	ew.Start()
	time.Sleep(50 * time.Millisecond)

	// Calling Stop multiple times should not panic
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ew.Stop()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent Stop() calls did not all return within 3 seconds")
	}
}

func TestEventWatcher_ReadCurrentFileNotExist(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "nonexistent.jsonl")

	ew, err := NewEventWatcher("sess-nofile", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}
	defer ew.Stop()
	ew.Start()

	got := ew.ReadCurrent()
	if got != nil {
		t.Errorf("ReadCurrent on nonexistent file should return nil, got: %+v", got)
	}
}

func TestEventWatcher_NewEventWatcherBadDirectory(t *testing.T) {
	// Directory does not exist, so watching it should fail
	filePath := filepath.Join(t.TempDir(), "nonexistent_subdir", "events.jsonl")

	_, err := NewEventWatcher("sess-baddir", filePath)
	if err == nil {
		t.Fatal("expected error when directory does not exist, got nil")
	}
}

func TestEventWatcher_MultipleEventsAcrossWrites(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "events.jsonl")

	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	ew, err := NewEventWatcher("sess-multiwrites", filePath)
	if err != nil {
		t.Fatalf("NewEventWatcher failed: %v", err)
	}

	received := make(chan Event, 20)
	ew.Subscribe("status", func(sessionID string, e Event) {
		received <- e
	})

	ew.Start()
	defer ew.Stop()

	// Write first event
	writeEventLine(t, filePath, Event{
		Timestamp: ts,
		Type:      "status",
		State:     "working",
		Message:   "first",
	})

	// Wait for it to be dispatched
	select {
	case e := <-received:
		if e.Message != "first" {
			t.Errorf("Message = %q, want first", e.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first event")
	}

	// Wait for debounce to settle, then write second event
	time.Sleep(200 * time.Millisecond)

	writeEventLine(t, filePath, Event{
		Timestamp: ts.Add(5 * time.Minute),
		Type:      "status",
		State:     "completed",
		Message:   "second",
	})

	// Wait for the second event
	select {
	case e := <-received:
		if e.Message != "second" {
			t.Errorf("Message = %q, want second", e.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second event")
	}
}
