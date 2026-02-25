package events

import "encoding/json"

// RawEvent is the common envelope parsed from each JSONL line.
type RawEvent struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
}

// ParseRawEvent extracts the envelope fields from a JSONL line.
func ParseRawEvent(data []byte) (RawEvent, error) {
	var raw RawEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return RawEvent{}, err
	}
	return raw, nil
}

// StatusEvent represents an agent state change.
type StatusEvent struct {
	Ts       string `json:"ts"`
	Type     string `json:"type"`
	State    string `json:"state"`
	Message  string `json:"message,omitempty"`
	Intent   string `json:"intent,omitempty"`
	Blockers string `json:"blockers,omitempty"`
}

// ValidStates for status events.
var ValidStates = map[string]bool{
	"working":       true,
	"completed":     true,
	"needs_input":   true,
	"needs_testing": true,
	"error":         true,
	"rotate":        true,
}

// FailureEvent represents a tool failure.
type FailureEvent struct {
	Ts       string `json:"ts"`
	Type     string `json:"type"`
	Tool     string `json:"tool"`
	Input    string `json:"input"`
	Error    string `json:"error"`
	Category string `json:"category"`
}

// ReflectionEvent represents a friction learning.
type ReflectionEvent struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// FrictionEvent represents an ad-hoc friction note.
type FrictionEvent struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
	Text string `json:"text"`
}
