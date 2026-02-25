package events

import (
	"encoding/json"
	"testing"
)

func TestStatusEventMarshal(t *testing.T) {
	e := StatusEvent{
		Ts:      "2026-02-18T14:30:00Z",
		Type:    "status",
		State:   "working",
		Message: "Refactoring auth module",
		Intent:  "Improve module structure",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "status" {
		t.Errorf("type = %v, want status", parsed["type"])
	}
	if parsed["state"] != "working" {
		t.Errorf("state = %v, want working", parsed["state"])
	}
}

func TestFailureEventMarshal(t *testing.T) {
	e := FailureEvent{
		Ts:       "2026-02-18T14:30:00Z",
		Type:     "failure",
		Tool:     "Bash",
		Input:    "go build ./...",
		Error:    "undefined: Foo",
		Category: "build_failure",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "failure" {
		t.Errorf("type = %v, want failure", parsed["type"])
	}
	if parsed["tool"] != "Bash" {
		t.Errorf("tool = %v, want Bash", parsed["tool"])
	}
}

func TestReflectionEventMarshal(t *testing.T) {
	e := ReflectionEvent{
		Ts:   "2026-02-18T14:30:00Z",
		Type: "reflection",
		Text: "When using bare repos, run git fetch before git show",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "reflection" {
		t.Errorf("type = %v, want reflection", parsed["type"])
	}
}

func TestFrictionEventMarshal(t *testing.T) {
	e := FrictionEvent{
		Ts:   "2026-02-18T14:30:00Z",
		Type: "friction",
		Text: "The build command is go run ./cmd/build-dashboard",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "friction" {
		t.Errorf("type = %v, want friction", parsed["type"])
	}
}

func TestParseRawEvent(t *testing.T) {
	line := `{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"test"}`
	raw, err := ParseRawEvent([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if raw.Type != "status" {
		t.Errorf("type = %v, want status", raw.Type)
	}
}

func TestParseRawEventInvalidJSON(t *testing.T) {
	_, err := ParseRawEvent([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
