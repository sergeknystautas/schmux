package chat

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

// nudgeCapture is a test sink for runtime NudgeCallback. It records the
// values it observes in arrival order so tests can assert both the
// sequence and the content.
type nudgeCapture struct {
	mu      sync.Mutex
	updates []NudgeUpdate
}

func (c *nudgeCapture) callback(u NudgeUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, u)
}

func (c *nudgeCapture) last() NudgeUpdate {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.updates) == 0 {
		return NudgeUpdate{}
	}
	return c.updates[len(c.updates)-1]
}

func (c *nudgeCapture) all() []NudgeUpdate {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]NudgeUpdate, len(c.updates))
	copy(out, c.updates)
	return out
}

func newRuntimeWithNudge(t *testing.T, cb NudgeCallback) (*Runtime, Paths) {
	t.Helper()
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime("s1", mustProto(t), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.SetNudgeCallback(cb)
	return rt, p
}

func writeOutput(t *testing.T, p Paths, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRuntimeNudge_NoSubscriber covers the basic path: the runtime emits
// a Nudge when a control_request is observed even though no chat socket
// is connected.
func TestRuntimeNudge_NoSubscriber(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	rt.Start()
	writeOutput(t, p,
		`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`,
	)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := cap.last(); got.State == "Needs Input" && got.Summary == "Approve Bash: ls" {
			if got.Summary != "Approve Bash: ls" {
				t.Fatalf("summary: %q", got.Summary)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected Needs Input, last=%+v all=%+v", cap.last(), cap.all())
}

// TestRuntimeNudge_SendWhileWaiting asserts that a queued user message
// does not erase a still-pending Needs Input state.
func TestRuntimeNudge_SendWhileWaiting(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	rt.Start()
	writeOutput(t, p,
		`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`,
	)
	waitFor(t, 2*time.Second, func() bool { return cap.last().Summary == "Approve Bash: ls" })
	if _, err := rt.Send("follow-up", nil); err != nil {
		t.Fatal(err)
	}
	// Give the runtime a moment to (incorrectly) clear the pending
	// state. We poll the capture and assert it never flips away.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		got := cap.last()
		if got.State != "Needs Input" || got.Summary != "Approve Bash: ls" {
			t.Fatalf("after send: %+v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRuntimeNudge_FailedAnswerWrite asserts that a control write that
// fails leaves the pending request pending. We force the failure by
// replacing appendInput with a function that returns an error.
func TestRuntimeNudge_FailedAnswerWrite(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	rt.Start()
	writeOutput(t, p,
		`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`,
	)
	waitFor(t, 2*time.Second, func() bool { return cap.last().Summary == "Approve Bash: ls" })

	// Force the answer write to fail.
	rt.mu.Lock()
	rt.appendInput = func(line []byte) error { return errors.New("simulated input write failure") }
	rt.mu.Unlock()

	err := rt.AnswerPermission("r1", true, nil, "")
	if err == nil {
		t.Fatal("expected an error from the simulated input write failure")
	}
	// Tracker must still show Needs Input, the same as before the answer.
	if got := cap.last(); got.State != "Needs Input" || got.Summary != "Approve Bash: ls" {
		t.Fatalf("after failed answer: %+v", got)
	}
}

// TestRuntimeNudge_DeltasDoNotPublish asserts that stream_event deltas
// do not cause extra Nudge publications. A burst of stream_events,
// followed by an assistant record and a control_request, should yield
// exactly one new publish (for the control_request) — not one per
// delta or one for the assistant.
func TestRuntimeNudge_DeltasDoNotPublish(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	rt.Start()
	if _, err := rt.Send("hi", nil); err != nil {
		t.Fatal(err)
	}
	cap.mu.Lock()
	before := len(cap.updates)
	cap.mu.Unlock()

	// Burst of stream events + assistant + control_request.
	f, _ := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	for i := 0; i < 5; i++ {
		f.WriteString(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}}` + "\n")
	}
	f.WriteString(`{"type":"assistant","message":{"content":[]}}` + "\n")
	f.WriteString(`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}` + "\n")
	f.Close()

	// Wait for the control_request to be consumed (it is the one
	// state-changing line in the burst).
	waitFor(t, 2*time.Second, func() bool {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		return len(cap.updates) > before && cap.updates[len(cap.updates)-1].State == "Needs Input"
	})
	// Allow any trailing publications to settle.
	time.Sleep(200 * time.Millisecond)
	cap.mu.Lock()
	deltaCount := len(cap.updates) - before
	cap.mu.Unlock()
	if deltaCount != 1 {
		t.Fatalf("deltas should not publish; expected exactly 1 new update, got %d", deltaCount)
	}
}

// TestRuntimeNudge_OrderedUpdates asserts that older Nudges do not
// arrive after newer ones when input and output are concurrent. The
// tracker emits under the runtime mutex, which also serializes
// record appends and input writes; this test races a flood of
// accepted user actions against a flood of output lines to make sure
// the final state matches the final record, not a stale snapshot.
func TestRuntimeNudge_OrderedUpdates(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	rt.Start()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := rt.Send("m", nil); err != nil {
				t.Error(err)
			}
		}()
		go func(i int) {
			defer wg.Done()
			line := asLine(map[string]any{"type": "control_request", "request_id": fmt.Sprint(i),
				"request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": fmt.Sprint(i)}}})
			writeOutput(t, p, string(line))
		}(i)
	}
	wg.Wait()
	waitFor(t, 2*time.Second, func() bool {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return len(rt.nudgeTracker.pending) == 30
	})
	rt.Stop()
	recs, err := rt.log.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	tr := NewNudgeTracker(ProtocolClaude)
	want := []NudgeUpdate{{State: "Idle"}}
	for _, rec := range recs {
		tr.Rec(rec)
		n := tr.Result()
		u := NudgeUpdate{State: n.State, Summary: n.Summary}
		if u != want[len(want)-1] {
			want = append(want, u)
		}
	}
	if got := cap.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("publications are not in record order:\ngot %v\nwant %v", got, want)
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// TestRuntimeNudge_RestoredEqualsLive verifies that a Nudge derived from
// a live runtime at a given boundary matches the Nudge the runtime
// produces after restoration (Restart) at the same boundary. The
// scenarios in the plan: pending request, successful answer, failed
// answer write, queued message, interruption, turn completion.
func TestRuntimeNudge_RestoredEqualsLive(t *testing.T) {
	scenarios := []struct {
		name string
		live func(t *testing.T, rt *Runtime, p Paths)
		want Nudge
	}{
		{
			name: "pending request",
			live: func(t *testing.T, rt *Runtime, p Paths) {
				writeOutput(t, p, `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
				waitFor(t, 2*time.Second, func() bool { return lastNudge(rt).Summary == "Approve Bash: ls" })
			},
			want: Nudge{State: "Needs Input", Summary: "Approve Bash: ls", Source: "headless"},
		},
		{
			name: "successful answer",
			live: func(t *testing.T, rt *Runtime, p Paths) {
				writeOutput(t, p, `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
				waitFor(t, 2*time.Second, func() bool { return lastNudge(rt).Summary == "Approve Bash: ls" })
				rt.AnswerPermission("r1", true, nil, "")
			},
			want: Nudge{State: "Working", Source: "headless"},
		},
		{
			name: "failed answer write",
			live: func(t *testing.T, rt *Runtime, p Paths) {
				writeOutput(t, p, `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
				waitFor(t, 2*time.Second, func() bool { return lastNudge(rt).Summary == "Approve Bash: ls" })
				rt.mu.Lock()
				rt.appendInput = func(line []byte) error { return errors.New("simulated failure") }
				rt.mu.Unlock()
				rt.AnswerPermission("r1", true, nil, "")
			},
			want: Nudge{State: "Needs Input", Summary: "Approve Bash: ls", Source: "headless"},
		},
		{
			name: "queued message",
			live: func(t *testing.T, rt *Runtime, p Paths) {
				writeOutput(t, p, `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
				waitFor(t, 2*time.Second, func() bool { return lastNudge(rt).Summary == "Approve Bash: ls" })
				rt.Send("queued", nil)
			},
			want: Nudge{State: "Needs Input", Summary: "Approve Bash: ls", Source: "headless"},
		},
		{
			name: "interruption",
			live: func(t *testing.T, rt *Runtime, p Paths) {
				rt.Send("hi", nil)
				writeOutput(t, p, `{"type":"assistant","message":{"content":[]}}`)
				rt.Interrupt()
				writeOutput(t, p, `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["interrupted"]}`)
				waitFor(t, 2*time.Second, func() bool { return lastState(rt) == "Idle" })
			},
			want: Nudge{State: "Idle", Source: "headless"},
		},
		{
			name: "turn completion",
			live: func(t *testing.T, rt *Runtime, p Paths) {
				rt.Send("hi", nil)
				writeOutput(t, p,
					`{"type":"assistant","message":{"content":[]}}`,
					`{"type":"result","subtype":"success","result":"Done"}`,
				)
				waitFor(t, 2*time.Second, func() bool { return lastState(rt) == "Completed" })
			},
			want: Nudge{State: "Completed", Summary: "Done", Source: "headless"},
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			live, p := newRuntimeWithNudge(t, nil)
			created := time.Now().Add(-time.Hour)
			liveActivity := &activityCapture{}
			live.SetActivityCallback(created, liveActivity.callback)
			live.Start()
			sc.live(t, live, p)
			live.Stop()
			restored, err := NewRuntime("s1", mustProto(t), p, "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restored.Stop)
			restoredActivity := &activityCapture{}
			restored.SetActivityCallback(created, restoredActivity.callback)
			restored.Start()

			if got := lastNudge(live); !got.Equal(sc.want) {
				t.Fatalf("live: got %+v want %+v", got, sc.want)
			}
			if got := lastNudge(restored); !got.Equal(sc.want) {
				t.Fatalf("restored: got %+v want %+v", got, sc.want)
			}
			liveTimes, restoredTimes := liveActivity.all(), restoredActivity.all()
			if len(restoredTimes) != 1 || !restoredTimes[0].Equal(liveTimes[len(liveTimes)-1]) {
				t.Fatalf("activity changed on restoration: live %v, restored %v", liveTimes, restoredTimes)
			}
		})
	}
}

// lastNudge returns the final value the runtime's tracker computed.
// It does not depend on the callback: the tracker is the source of
// truth; the callback is just a delivery channel.
func lastNudge(rt *Runtime) Nudge {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.nudgeTracker.Result()
}

func lastState(rt *Runtime) string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.nudgeTracker.Result().State
}

// TestRuntimeNudge_SeededHistoryConsumesNewOutput verifies that a
// runtime that starts with a record containing a session-ended marker
// (a Restart seed) and a fresh output file with new lines consumes
// those new lines and produces the new Nudge.
func TestRuntimeNudge_SeededHistoryConsumesNewOutput(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	// Pre-populate the record with an old lifetime (user, harness,
	// result success) and a session-ended marker.
	l, err := OpenLog(p.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	l.Append(NewUserMessage("first", nil))
	l.Append(NewHarness([]byte(`{"type":"assistant","message":{}}`)))
	l.Append(NewHarness([]byte(`{"type":"result","subtype":"success"}`)))
	l.Append(NewSessionEnded())
	// The output file is the new lifetime's output. It must NOT
	// contain the old lifetime's lines.
	if err := os.WriteFile(p.Output, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	cap := &nudgeCapture{}
	rt, err := NewRuntime("s1", mustProto(t), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.SetNudgeCallback(cap.callback)
	rt.Start()
	// Old completion is history, not the new process's state.
	if got := lastNudge(rt); got.State != "Idle" || got.Summary != "" {
		t.Fatalf("post-restart idle: %+v", got)
	}
	// Write a new output line for the new lifetime.
	writeOutput(t, p, `{"type":"control_request","request_id":"r-new","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	waitFor(t, 2*time.Second, func() bool { return lastNudge(rt).Summary == "Approve Bash: ls" })
	if got := lastNudge(rt); got.Summary != "Approve Bash: ls" {
		t.Fatalf("post-restart new output: %+v", got)
	}
}

// TestRuntimeNudge_StopDoesNotEndTurn verifies that runtime.Stop alone
// does not record a session-ended marker. The harness might still be
// running; only an explicit End (called by Manager.stopTracker) should
// close the turn.
func TestRuntimeNudge_StopDoesNotEndTurn(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	rt.Start()
	rt.Send("hi", nil)
	writeOutput(t, p, `{"type":"assistant","message":{}}`)
	waitFor(t, 2*time.Second, func() bool { return lastState(rt) == "Working" })
	rt.Stop()

	// Read the record directly; no session-ended record should be
	// present.
	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	for _, r := range recs {
		if r.Type == RecordSession && r.Event == "ended" {
			t.Fatal("Stop must not append a session-ended record")
		}
	}
	// And the tracker state is whatever it was at Stop time (Working
	// for the open turn). End is a separate call.
	rt.mu.Lock()
	state := rt.nudgeTracker.Result().State
	rt.mu.Unlock()
	if state != "Working" {
		t.Fatalf("after Stop, expected Working (open turn), got %s", state)
	}
	_ = cap
}

// TestRuntimeNudge_EndAppendsSessionEndedMarker verifies that the
// dispose path's explicit End writes a session-ended record. After End,
// a fresh runtime starts Idle, not with the old completion.
func TestRuntimeNudge_EndAppendsSessionEndedMarker(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	rt.Start()
	rt.Send("hi", nil)
	writeOutput(t, p,
		`{"type":"assistant","message":{}}`,
		`{"type":"result","subtype":"success"}`,
	)
	waitFor(t, 2*time.Second, func() bool { return lastState(rt) == "Completed" })
	if err := rt.End(); err != nil {
		t.Fatal(err)
	}
	rt.Stop()

	// Replay in a fresh runtime.
	cap2 := &nudgeCapture{}
	rt2, err := NewRuntime("s1", mustProto(t), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt2.Stop)
	rt2.SetNudgeCallback(cap2.callback)
	rt2.Start()
	// The new lifetime begins after the session-ended marker.
	if got := lastNudge(rt2); got.State != "Idle" || got.Summary != "" {
		t.Fatalf("after End and Restart: %+v", got)
	}
}
