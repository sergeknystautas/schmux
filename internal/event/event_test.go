package event

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseEvent(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	tsJSON := `"2025-01-15T10:30:00Z"`

	tests := []struct {
		name    string
		line    string
		want    Event
		wantErr bool
	}{
		{
			name: "valid status event",
			line: `{"ts":` + tsJSON + `,"type":"status","state":"working","message":"implementing feature","intent":"add tests","blockers":"none"}`,
			want: Event{
				Timestamp: ts,
				Type:      "status",
				State:     "working",
				Message:   "implementing feature",
				Intent:    "add tests",
				Blockers:  "none",
			},
		},
		{
			name: "valid failure event",
			line: `{"ts":` + tsJSON + `,"type":"failure","tool":"Bash","input":"npm run build","error":"command not found","category":"build"}`,
			want: Event{
				Timestamp: ts,
				Type:      "failure",
				Tool:      "Bash",
				Input:     "npm run build",
				Error:     "command not found",
				Category:  "build",
			},
		},
		{
			name: "valid reflection event",
			line: `{"ts":` + tsJSON + `,"type":"reflection","text":"Always use go run ./cmd/build-dashboard instead of npm"}`,
			want: Event{
				Timestamp: ts,
				Type:      "reflection",
				Text:      "Always use go run ./cmd/build-dashboard instead of npm",
			},
		},
		{
			name: "valid friction event",
			line: `{"ts":` + tsJSON + `,"type":"friction","text":"Config file format is undocumented"}`,
			want: Event{
				Timestamp: ts,
				Type:      "friction",
				Text:      "Config file format is undocumented",
			},
		},
		{
			name:    "malformed JSON",
			line:    `{not valid json}`,
			wantErr: true,
		},
		{
			name:    "empty line",
			line:    "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			line:    "   \t  ",
			wantErr: true,
		},
		{
			name: "line with leading/trailing whitespace is trimmed",
			line: `  {"ts":` + tsJSON + `,"type":"status","state":"completed"}  `,
			want: Event{
				Timestamp: ts,
				Type:      "status",
				State:     "completed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEvent(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseEvent(%q) expected error, got nil", tt.line)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEvent(%q) unexpected error: %v", tt.line, err)
			}
			if got.Timestamp != tt.want.Timestamp {
				t.Errorf("Timestamp = %v, want %v", got.Timestamp, tt.want.Timestamp)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.State != tt.want.State {
				t.Errorf("State = %q, want %q", got.State, tt.want.State)
			}
			if got.Message != tt.want.Message {
				t.Errorf("Message = %q, want %q", got.Message, tt.want.Message)
			}
			if got.Intent != tt.want.Intent {
				t.Errorf("Intent = %q, want %q", got.Intent, tt.want.Intent)
			}
			if got.Blockers != tt.want.Blockers {
				t.Errorf("Blockers = %q, want %q", got.Blockers, tt.want.Blockers)
			}
			if got.Tool != tt.want.Tool {
				t.Errorf("Tool = %q, want %q", got.Tool, tt.want.Tool)
			}
			if got.Input != tt.want.Input {
				t.Errorf("Input = %q, want %q", got.Input, tt.want.Input)
			}
			if got.Error != tt.want.Error {
				t.Errorf("Error = %q, want %q", got.Error, tt.want.Error)
			}
			if got.Category != tt.want.Category {
				t.Errorf("Category = %q, want %q", got.Category, tt.want.Category)
			}
			if got.Text != tt.want.Text {
				t.Errorf("Text = %q, want %q", got.Text, tt.want.Text)
			}
		})
	}
}

func TestReadEvents(t *testing.T) {
	t.Run("file not found returns nil nil", func(t *testing.T) {
		events, err := ReadEvents("/nonexistent/path/events.jsonl")
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if events != nil {
			t.Fatalf("expected nil events, got: %v", events)
		}
	})

	t.Run("empty file returns empty slice", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		events, err := ReadEvents(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 0 {
			t.Fatalf("expected 0 events, got %d", len(events))
		}
	})

	t.Run("multiple valid events returns all in order", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		content := `{"ts":"2025-01-15T10:00:00Z","type":"status","state":"working","message":"starting"}
{"ts":"2025-01-15T10:05:00Z","type":"failure","tool":"Bash","error":"exit 1"}
{"ts":"2025-01-15T10:10:00Z","type":"status","state":"completed","message":"done"}
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		events, err := ReadEvents(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 3 {
			t.Fatalf("expected 3 events, got %d", len(events))
		}
		if events[0].Type != "status" || events[0].State != "working" {
			t.Errorf("event[0]: got type=%q state=%q, want type=status state=working", events[0].Type, events[0].State)
		}
		if events[1].Type != "failure" || events[1].Tool != "Bash" {
			t.Errorf("event[1]: got type=%q tool=%q, want type=failure tool=Bash", events[1].Type, events[1].Tool)
		}
		if events[2].Type != "status" || events[2].State != "completed" {
			t.Errorf("event[2]: got type=%q state=%q, want type=status state=completed", events[2].Type, events[2].State)
		}
	})

	t.Run("file with malformed lines skips bad lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		content := `{"ts":"2025-01-15T10:00:00Z","type":"status","state":"working"}
{bad json here}
not json at all
{"ts":"2025-01-15T10:10:00Z","type":"status","state":"completed"}
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		events, err := ReadEvents(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected 2 events (skipping malformed), got %d", len(events))
		}
		if events[0].State != "working" {
			t.Errorf("event[0].State = %q, want working", events[0].State)
		}
		if events[1].State != "completed" {
			t.Errorf("event[1].State = %q, want completed", events[1].State)
		}
	})

	t.Run("file with blank lines skips them", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		content := `{"ts":"2025-01-15T10:00:00Z","type":"status","state":"working"}


{"ts":"2025-01-15T10:10:00Z","type":"status","state":"completed"}
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		events, err := ReadEvents(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected 2 events (skipping blanks), got %d", len(events))
		}
	})
}

func TestReadEventsFiltered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	content := `{"ts":"2025-01-15T10:00:00Z","type":"status","state":"working"}
{"ts":"2025-01-15T10:01:00Z","type":"failure","tool":"Bash","error":"exit 1"}
{"ts":"2025-01-15T10:02:00Z","type":"reflection","text":"learned something"}
{"ts":"2025-01-15T10:03:00Z","type":"friction","text":"docs unclear"}
{"ts":"2025-01-15T10:04:00Z","type":"status","state":"completed"}
{"ts":"2025-01-15T10:05:00Z","type":"failure","tool":"Read","error":"not found"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("filter by single type", func(t *testing.T) {
		events, err := ReadEventsFiltered(path, "failure")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected 2 failure events, got %d", len(events))
		}
		for _, e := range events {
			if e.Type != "failure" {
				t.Errorf("expected type=failure, got %q", e.Type)
			}
		}
	})

	t.Run("filter by multiple types", func(t *testing.T) {
		events, err := ReadEventsFiltered(path, "reflection", "friction")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected 2 events (reflection+friction), got %d", len(events))
		}
		if events[0].Type != "reflection" {
			t.Errorf("event[0].Type = %q, want reflection", events[0].Type)
		}
		if events[1].Type != "friction" {
			t.Errorf("event[1].Type = %q, want friction", events[1].Type)
		}
	})

	t.Run("no matches returns empty slice", func(t *testing.T) {
		events, err := ReadEventsFiltered(path, "nonexistent_type")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 0 {
			t.Fatalf("expected 0 events for nonexistent type, got %d", len(events))
		}
	})

	t.Run("file not found returns nil nil", func(t *testing.T) {
		events, err := ReadEventsFiltered("/nonexistent/path.jsonl", "status")
		if err != nil {
			t.Fatalf("expected nil error for missing file, got: %v", err)
		}
		if events != nil {
			t.Fatalf("expected nil events for missing file, got: %v", events)
		}
	})
}

func TestLatestStatus(t *testing.T) {
	t.Run("multiple status events returns last one", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		content := `{"ts":"2025-01-15T10:00:00Z","type":"status","state":"working","message":"starting"}
{"ts":"2025-01-15T10:05:00Z","type":"status","state":"needs_input","message":"need help"}
{"ts":"2025-01-15T10:10:00Z","type":"status","state":"completed","message":"all done"}
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := LatestStatus(path)
		if got == nil {
			t.Fatal("expected non-nil event, got nil")
		}
		if got.State != "completed" {
			t.Errorf("State = %q, want completed", got.State)
		}
		if got.Message != "all done" {
			t.Errorf("Message = %q, want 'all done'", got.Message)
		}
	})

	t.Run("no status events returns nil", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		content := `{"ts":"2025-01-15T10:00:00Z","type":"failure","tool":"Bash","error":"exit 1"}
{"ts":"2025-01-15T10:01:00Z","type":"reflection","text":"learned something"}
{"ts":"2025-01-15T10:02:00Z","type":"friction","text":"docs unclear"}
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := LatestStatus(path)
		if got != nil {
			t.Errorf("expected nil for no status events, got: %+v", got)
		}
	})

	t.Run("mixed event types returns last status", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		content := `{"ts":"2025-01-15T10:00:00Z","type":"status","state":"working"}
{"ts":"2025-01-15T10:01:00Z","type":"failure","tool":"Bash","error":"exit 1"}
{"ts":"2025-01-15T10:02:00Z","type":"status","state":"needs_input","message":"blocked"}
{"ts":"2025-01-15T10:03:00Z","type":"reflection","text":"learned something"}
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := LatestStatus(path)
		if got == nil {
			t.Fatal("expected non-nil event, got nil")
		}
		if got.State != "needs_input" {
			t.Errorf("State = %q, want needs_input", got.State)
		}
		if got.Message != "blocked" {
			t.Errorf("Message = %q, want 'blocked'", got.Message)
		}
	})

	t.Run("file does not exist returns nil", func(t *testing.T) {
		got := LatestStatus("/nonexistent/path/events.jsonl")
		if got != nil {
			t.Errorf("expected nil for nonexistent file, got: %+v", got)
		}
	})
}

func TestEventFilePath(t *testing.T) {
	tests := []struct {
		name          string
		workspacePath string
		sessionID     string
		want          string
	}{
		{
			name:          "basic path construction",
			workspacePath: "/home/user/workspace",
			sessionID:     "session-abc123",
			want:          filepath.Join("/home/user/workspace", ".schmux", "events", "session-abc123.jsonl"),
		},
		{
			name:          "workspace path with trailing slash behavior",
			workspacePath: "/tmp/ws",
			sessionID:     "s1",
			want:          filepath.Join("/tmp/ws", ".schmux", "events", "s1.jsonl"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EventFilePath(tt.workspacePath, tt.sessionID)
			if got != tt.want {
				t.Errorf("EventFilePath(%q, %q) = %q, want %q", tt.workspacePath, tt.sessionID, got, tt.want)
			}
		})
	}
}

func TestToSignal(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	t.Run("status event converts to signal correctly", func(t *testing.T) {
		e := Event{
			Timestamp: ts,
			Type:      "status",
			State:     "working",
			Message:   "implementing feature",
			Intent:    "add tests",
			Blockers:  "none",
		}

		sig := ToSignal(e)
		if sig == nil {
			t.Fatal("expected non-nil signal, got nil")
		}
		if sig.State != "working" {
			t.Errorf("State = %q, want working", sig.State)
		}
		if sig.Message != "implementing feature" {
			t.Errorf("Message = %q, want 'implementing feature'", sig.Message)
		}
		if sig.Intent != "add tests" {
			t.Errorf("Intent = %q, want 'add tests'", sig.Intent)
		}
		if sig.Blockers != "none" {
			t.Errorf("Blockers = %q, want 'none'", sig.Blockers)
		}
		if sig.Timestamp != ts {
			t.Errorf("Timestamp = %v, want %v", sig.Timestamp, ts)
		}
	})

	t.Run("non-status event returns nil", func(t *testing.T) {
		e := Event{
			Timestamp: ts,
			Type:      "failure",
			Tool:      "Bash",
			Error:     "exit 1",
		}

		sig := ToSignal(e)
		if sig != nil {
			t.Errorf("expected nil for non-status event, got: %+v", sig)
		}
	})

	t.Run("status event with invalid state returns nil", func(t *testing.T) {
		e := Event{
			Timestamp: ts,
			Type:      "status",
			State:     "invalid_state_value",
			Message:   "some message",
		}

		sig := ToSignal(e)
		if sig != nil {
			t.Errorf("expected nil for invalid state, got: %+v", sig)
		}
	})

	t.Run("all valid states convert successfully", func(t *testing.T) {
		validStates := []string{"needs_input", "needs_testing", "completed", "error", "working", "rotate"}
		for _, state := range validStates {
			e := Event{
				Timestamp: ts,
				Type:      "status",
				State:     state,
			}
			sig := ToSignal(e)
			if sig == nil {
				t.Errorf("expected non-nil signal for valid state %q, got nil", state)
			} else if sig.State != state {
				t.Errorf("State = %q, want %q", sig.State, state)
			}
		}
	})
}

func TestReadLoreEventsFromWorkspace(t *testing.T) {
	t.Run("multiple session files reads from all", func(t *testing.T) {
		dir := t.TempDir()
		eventsDir := filepath.Join(dir, ".schmux", "events")
		if err := os.MkdirAll(eventsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Session 1: failure + reflection
		session1 := `{"ts":"2025-01-15T10:00:00Z","type":"failure","tool":"Bash","input":"npm build","error":"not found","category":"build"}
{"ts":"2025-01-15T10:01:00Z","type":"reflection","text":"use go wrapper"}
`
		if err := os.WriteFile(filepath.Join(eventsDir, "sess-001.jsonl"), []byte(session1), 0644); err != nil {
			t.Fatal(err)
		}

		// Session 2: friction
		session2 := `{"ts":"2025-01-15T11:00:00Z","type":"friction","text":"config docs unclear"}
`
		if err := os.WriteFile(filepath.Join(eventsDir, "sess-002.jsonl"), []byte(session2), 0644); err != nil {
			t.Fatal(err)
		}

		entries, err := ReadLoreEventsFromWorkspace(dir, "ws-test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("expected 3 lore entries from 2 sessions, got %d", len(entries))
		}

		// Verify we have entries from both sessions
		sessionIDs := map[string]bool{}
		for _, e := range entries {
			sessionIDs[e.Session] = true
		}
		if !sessionIDs["sess-001"] || !sessionIDs["sess-002"] {
			t.Errorf("expected entries from both sessions, got sessions: %v", sessionIDs)
		}
	})

	t.Run("filters to only lore types", func(t *testing.T) {
		dir := t.TempDir()
		eventsDir := filepath.Join(dir, ".schmux", "events")
		if err := os.MkdirAll(eventsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Mix of status and lore-relevant events
		content := `{"ts":"2025-01-15T10:00:00Z","type":"status","state":"working"}
{"ts":"2025-01-15T10:01:00Z","type":"failure","tool":"Bash","input":"bad cmd","error":"fail","category":"runtime"}
{"ts":"2025-01-15T10:02:00Z","type":"status","state":"completed"}
{"ts":"2025-01-15T10:03:00Z","type":"reflection","text":"learned something"}
{"ts":"2025-01-15T10:04:00Z","type":"friction","text":"papercut found"}
`
		if err := os.WriteFile(filepath.Join(eventsDir, "sess-mix.jsonl"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		entries, err := ReadLoreEventsFromWorkspace(dir, "ws-test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("expected 3 lore entries (failure+reflection+friction), got %d", len(entries))
		}
		for _, e := range entries {
			if e.Type != "failure" && e.Type != "reflection" && e.Type != "friction" {
				t.Errorf("unexpected lore entry type: %q", e.Type)
			}
		}
	})

	t.Run("missing events directory returns empty no error", func(t *testing.T) {
		dir := t.TempDir()
		// No .schmux/events directory created

		entries, err := ReadLoreEventsFromWorkspace(dir, "ws-test")
		if err != nil {
			t.Fatalf("unexpected error for missing events dir: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected 0 entries for missing dir, got %d", len(entries))
		}
	})

	t.Run("failure event maps fields correctly", func(t *testing.T) {
		dir := t.TempDir()
		eventsDir := filepath.Join(dir, ".schmux", "events")
		if err := os.MkdirAll(eventsDir, 0755); err != nil {
			t.Fatal(err)
		}

		content := `{"ts":"2025-01-15T10:00:00Z","type":"failure","tool":"Bash","input":"npm run build","error":"command failed","category":"build"}
`
		if err := os.WriteFile(filepath.Join(eventsDir, "sess-f.jsonl"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		entries, err := ReadLoreEventsFromWorkspace(dir, "ws-mapped")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		e := entries[0]
		if e.Type != "failure" {
			t.Errorf("Type = %q, want failure", e.Type)
		}
		if e.Tool != "Bash" {
			t.Errorf("Tool = %q, want Bash", e.Tool)
		}
		if e.InputSummary != "npm run build" {
			t.Errorf("InputSummary = %q, want 'npm run build'", e.InputSummary)
		}
		if e.ErrorSummary != "command failed" {
			t.Errorf("ErrorSummary = %q, want 'command failed'", e.ErrorSummary)
		}
		if e.Category != "build" {
			t.Errorf("Category = %q, want build", e.Category)
		}
		if e.Workspace != "ws-mapped" {
			t.Errorf("Workspace = %q, want ws-mapped", e.Workspace)
		}
		if e.Session != "sess-f" {
			t.Errorf("Session = %q, want sess-f", e.Session)
		}
		if e.Agent != "claude-code" {
			t.Errorf("Agent = %q, want claude-code", e.Agent)
		}
	})

	t.Run("reflection event maps text correctly", func(t *testing.T) {
		dir := t.TempDir()
		eventsDir := filepath.Join(dir, ".schmux", "events")
		if err := os.MkdirAll(eventsDir, 0755); err != nil {
			t.Fatal(err)
		}

		content := `{"ts":"2025-01-15T10:00:00Z","type":"reflection","text":"always run tests before commit"}
`
		if err := os.WriteFile(filepath.Join(eventsDir, "sess-r.jsonl"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		entries, err := ReadLoreEventsFromWorkspace(dir, "ws-ref")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		e := entries[0]
		if e.Type != "reflection" {
			t.Errorf("Type = %q, want reflection", e.Type)
		}
		if e.Text != "always run tests before commit" {
			t.Errorf("Text = %q, want 'always run tests before commit'", e.Text)
		}
		if e.Session != "sess-r" {
			t.Errorf("Session = %q, want sess-r", e.Session)
		}
	})

	t.Run("friction event maps text correctly", func(t *testing.T) {
		dir := t.TempDir()
		eventsDir := filepath.Join(dir, ".schmux", "events")
		if err := os.MkdirAll(eventsDir, 0755); err != nil {
			t.Fatal(err)
		}

		content := `{"ts":"2025-01-15T10:00:00Z","type":"friction","text":"error messages are unhelpful"}
`
		if err := os.WriteFile(filepath.Join(eventsDir, "sess-fr.jsonl"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		entries, err := ReadLoreEventsFromWorkspace(dir, "ws-fric")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		e := entries[0]
		if e.Type != "friction" {
			t.Errorf("Type = %q, want friction", e.Type)
		}
		if e.Text != "error messages are unhelpful" {
			t.Errorf("Text = %q, want 'error messages are unhelpful'", e.Text)
		}
	})

	t.Run("timestamp is preserved", func(t *testing.T) {
		dir := t.TempDir()
		eventsDir := filepath.Join(dir, ".schmux", "events")
		if err := os.MkdirAll(eventsDir, 0755); err != nil {
			t.Fatal(err)
		}

		content := `{"ts":"2025-06-20T14:30:45Z","type":"failure","tool":"Read","input":"file.go","error":"not found","category":"fs"}
`
		if err := os.WriteFile(filepath.Join(eventsDir, "sess-ts.jsonl"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		entries, err := ReadLoreEventsFromWorkspace(dir, "ws-ts")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		expectedTS := time.Date(2025, 6, 20, 14, 30, 45, 0, time.UTC)
		if !entries[0].Timestamp.Equal(expectedTS) {
			t.Errorf("Timestamp = %v, want %v", entries[0].Timestamp, expectedTS)
		}
	})
}
