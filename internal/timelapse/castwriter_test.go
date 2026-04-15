package timelapse

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCastWriter_Header(t *testing.T) {
	var buf bytes.Buffer
	_, err := NewCastWriter(&buf, CastHeader{
		Width:  80,
		Height: 24,
		Title:  "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 header line, got %d", len(lines))
	}

	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header["version"].(float64) != 2 {
		t.Errorf("version = %v, want 2", header["version"])
	}
	if header["width"].(float64) != 80 {
		t.Errorf("width = %v, want 80", header["width"])
	}
}

func TestCastWriter_Events(t *testing.T) {
	var buf bytes.Buffer
	cw, err := NewCastWriter(&buf, CastHeader{Width: 80, Height: 24})
	if err != nil {
		t.Fatal(err)
	}

	cw.WriteEvent(0.0, "hello")
	cw.WriteEvent(0.5, "world")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 { // header + 2 events
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Parse event: [timestamp, "o", data]
	var event [3]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &event); err != nil {
		t.Fatal(err)
	}
	if event[0].(float64) != 0.0 {
		t.Errorf("event 0 timestamp = %v, want 0", event[0])
	}
	if event[1].(string) != "o" {
		t.Errorf("event 0 type = %v, want 'o'", event[1])
	}
	if event[2].(string) != "hello" {
		t.Errorf("event 0 data = %v, want 'hello'", event[2])
	}
}

func TestCastWriter_WriteResize(t *testing.T) {
	var buf bytes.Buffer
	cw, err := NewCastWriter(&buf, CastHeader{Width: 80, Height: 24})
	if err != nil {
		t.Fatal(err)
	}

	cw.WriteResize(1.5, 162, 90)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 { // header + 1 resize event
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var event [3]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &event); err != nil {
		t.Fatal(err)
	}
	if event[1].(string) != "r" {
		t.Errorf("event type = %v, want 'r'", event[1])
	}
	if event[2].(string) != "162x90" {
		t.Errorf("event data = %v, want '162x90'", event[2])
	}
}
