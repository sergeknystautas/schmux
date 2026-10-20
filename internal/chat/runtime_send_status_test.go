package chat

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestSendWithStatus_AcceptedOnAddressableRuntime covers the happy path:
// a runtime with no active turn accepts the message immediately. The bool
// is false; the record id matches what the harness will see in the input
// line.
func TestSendWithStatus_AcceptedOnAddressableRuntime(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()

	rec, queued, err := rt.SendWithStatus("hello", nil)
	if err != nil {
		t.Fatalf("SendWithStatus() error = %v", err)
	}
	if queued {
		t.Errorf("queued = true, want false on an addressable runtime")
	}
	if rec.ID == "" || rec.Text != "hello" {
		t.Errorf("record = %+v", rec)
	}
	in, _ := os.ReadFile(p.Input)
	if !strings.Contains(string(in), `"content":"hello"`) {
		t.Errorf("input = %q", in)
	}
}

// TestSendWithStatus_QueuedWhileTurnActive proves the second message of two
// is held: the first one opens a turn, so the runtime reports queued=true
// for the second send. The held record is still persisted; a result
// releases it.
func TestSendWithStatus_QueuedWhileTurnActive(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()

	first, queued, err := rt.SendWithStatus("first", nil)
	if err != nil {
		t.Fatalf("first send error = %v", err)
	}
	if queued {
		t.Errorf("first send queued = true, want false")
	}
	second, queued, err := rt.SendWithStatus("second", nil)
	if err != nil {
		t.Fatalf("second send error = %v", err)
	}
	if !queued {
		t.Errorf("second send queued = false, want true while a turn is active")
	}
	if second.ID == first.ID {
		t.Errorf("second reused first id %s", first.ID)
	}
	in, _ := os.ReadFile(p.Input)
	if strings.Count(string(in), `"content"`) != 1 {
		t.Errorf("input should hold one line until a result arrives: %q", in)
	}

	// Drain the held queue and confirm the second message gets accepted.
	writeClaudeOutput(t, p, `{"type":"result","subtype":"success","queued_turn_count":0}`)
	rt.drain()
	in, _ = os.ReadFile(p.Input)
	if !strings.Contains(string(in), `"content":"second"`) {
		t.Errorf("input after result = %q", in)
	}
}

// TestSendWithStatus_InputWriteFailureReturnsPersistedRecord covers the
// case the runtime reports queued=true with the persisted record id and
// the error from the input write, so a caller can decide whether to retry.
func TestSendWithStatus_InputWriteFailureReturnsPersistedRecord(t *testing.T) {
	rt, _ := newTestRuntime(t)
	rt.Start()
	rt.appendInput = func([]byte) error { return errors.New("simulated write failure") }

	rec, queued, err := rt.SendWithStatus("hello", nil)
	if err == nil {
		t.Fatal("SendWithStatus() error = nil, want write failure")
	}
	if !queued {
		t.Errorf("queued = false, want true when the runtime kept the record in its queue")
	}
	if rec.ID == "" {
		t.Errorf("record id = empty, want the persisted id even on input failure")
	}
}

// TestSendStillReturnsRecordIgnoresQueuedStatus verifies the Send
// compatibility wrapper does not surface the queued bool.
func TestSendStillReturnsRecordIgnoresQueuedStatus(t *testing.T) {
	rt, _ := newTestRuntime(t)
	rt.Start()

	first, err := rt.Send("first", nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	second, err := rt.Send("second", nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if first.ID == second.ID {
		t.Errorf("Send returned the same record id twice: %s", first.ID)
	}
}
