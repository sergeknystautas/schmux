package chat

import (
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

type activityCapture struct {
	mu      sync.Mutex
	updates []time.Time
}

func (c *activityCapture) callback(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, at)
}

func (c *activityCapture) all() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Time(nil), c.updates...)
}

func TestRuntimeActivity_RestoreAndReplacement(t *testing.T) {
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	active := created.Add(time.Minute)
	for _, protocol := range []string{ProtocolClaude, ProtocolCodex} {
		for _, boundary := range []string{"Working", "Completed", "Needs Input", "Idle"} {
			t.Run(protocol+"/"+boundary, func(t *testing.T) {
				p := PathsFor(t.TempDir())
				if err := p.Ensure(); err != nil {
					t.Fatal(err)
				}
				proto, err := ProtocolFor(protocol)
				if err != nil {
					t.Fatal(err)
				}
				rt, err := NewRuntime("old", proto, p, "", "", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(rt.Stop)
				lines := []string{`{"type":"assistant","message":{}}`}
				if protocol == ProtocolCodex {
					lines = []string{`{"method":"turn/started","params":{"threadId":"t","turn":{"id":"turn"}}}`}
				}
				var interrupt *Record
				switch boundary {
				case "Completed":
					if protocol == ProtocolClaude {
						lines = append(lines, `{"type":"result","subtype":"success"}`)
					} else {
						lines = append(lines, `{"method":"turn/completed","params":{"threadId":"t","turn":{"id":"turn","status":"completed"}}}`)
					}
				case "Needs Input":
					if protocol == ProtocolClaude {
						lines = append(lines, `{"type":"control_request","request_id":"r","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
					} else {
						lines = append(lines, `{"id":0,"method":"item/commandExecution/requestApproval","params":{"threadId":"t","turnId":"turn","command":"ls"}}`)
					}
				case "Idle":
					if protocol == ProtocolClaude {
						rec := NewControl([]byte(`{"type":"control_request","request_id":"i","request":{"subtype":"interrupt"}}`))
						rec.Ts = active.Format(time.RFC3339Nano)
						interrupt = &rec
						lines = append(lines, `{"type":"result","is_error":true,"errors":["interrupted"]}`)
					} else {
						lines = append(lines, `{"method":"turn/completed","params":{"threadId":"t","turn":{"id":"turn","status":"interrupted"}}}`)
					}
				}
				for i, line := range lines {
					if i == 1 && interrupt != nil {
						if err := rt.log.Append(*interrupt); err != nil {
							t.Fatal(err)
						}
						if err := AppendInput(p, interrupt.Line); err != nil {
							t.Fatal(err)
						}
					}
					rec := NewHarness([]byte(line))
					rec.Ts = active.Format(time.RFC3339Nano)
					if err := rt.log.Append(rec); err != nil {
						t.Fatal(err)
					}
				}
				cap := &activityCapture{}
				rt.SetActivityCallback(created, cap.callback)
				rt.Start()
				rt.Stop()
				if got := lastState(rt); got != boundary {
					t.Fatalf("restored state = %s, want %s", got, boundary)
				}
				if got := cap.all(); !reflect.DeepEqual(got, []time.Time{active}) {
					t.Fatalf("restore refreshed activity: %v", got)
				}

				// Session replacement copies history, but neither its old tasks nor
				// its status/clock. A daemon restart above kept the same lifetime.
				if err := rt.End(); err != nil {
					t.Fatal(err)
				}
				fresh := PathsFor(t.TempDir())
				if err := fresh.Ensure(); err != nil {
					t.Fatal(err)
				}
				if err := CopyLog(p.Conversation, fresh.Conversation); err != nil {
					t.Fatal(err)
				}
				proto, err = ProtocolFor(protocol)
				if err != nil {
					t.Fatal(err)
				}
				replacement, err := NewRuntime("new", proto, fresh, "", "", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(replacement.Stop)
				freshCap := &activityCapture{}
				freshCreated := active.Add(time.Hour)
				replacement.SetActivityCallback(freshCreated, freshCap.callback)
				replacement.Start()
				replacement.Stop()
				if got := lastNudge(replacement); got.State != "Idle" || got.Summary != "" {
					t.Fatalf("replacement inherited work: %+v", got)
				}
				if got := freshCap.all(); !reflect.DeepEqual(got, []time.Time{freshCreated}) {
					t.Fatalf("replacement clock = %v", got)
				}
				input, err := os.ReadFile(fresh.Input)
				if err != nil {
					t.Fatal(err)
				}
				if len(input) != 0 {
					t.Fatalf("replacement replayed work: %s", input)
				}
			})
		}
	}
}

func TestRuntimeActivity_DeltaTrailingFlushWithoutNudge(t *testing.T) {
	nudges := &nudgeCapture{}
	activity := &activityCapture{}
	rt, p := newRuntimeWithNudge(t, nudges.callback)
	created := time.Now().Add(-time.Hour)
	rt.SetActivityCallback(created, activity.callback)
	rt.Start()
	if _, err := rt.Send("hi", nil); err != nil {
		t.Fatal(err)
	}
	before := len(nudges.all())
	writeOutput(t, p, `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"x"}}}`)
	waitFor(t, 2*time.Second, func() bool { return len(activity.all()) >= 3 })
	rt.Stop()
	updates := activity.all()
	if len(updates) != 3 || !updates[1].After(created) || !updates[2].After(updates[1]) {
		t.Fatalf("creation/send/delta activity: %v", updates)
	}
	if len(nudges.all()) != before {
		t.Fatal("delta changed Nudge")
	}
	recs, err := rt.log.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Type != RecordUserMessage {
		t.Fatalf("delta was persisted: %+v", recs)
	}
}

func TestRuntimeActivity_DebounceAndBoundary(t *testing.T) {
	rt, _ := newRuntimeWithNudge(t, nil)
	cap := &activityCapture{}
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	rt.SetActivityCallback(base, cap.callback)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.publishActivityLocked(base, true)
	for i := 1; i <= 100; i++ {
		rec := NewHarness([]byte(`{"type":"stream_event"}`))
		rec.Ts = base.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)
		rt.noteActivityLocked(rec)
		rt.publishActivityLocked(base.Add(time.Duration(i)*time.Millisecond), false)
	}
	if len(cap.all()) != 1 {
		t.Fatal("burst was not debounced")
	}
	rt.publishActivityLocked(base.Add(activityInterval), false)
	if got := cap.all(); len(got) != 2 || !got[1].Equal(base.Add(100*time.Millisecond)) {
		t.Fatalf("trailing timestamp = %v", got)
	}
	rt.publishActivityLocked(base.Add(time.Hour), false)
	if len(cap.all()) != 2 {
		t.Fatal("idle tick refreshed activity")
	}
	// Completion flushes immediately even inside the debounce interval.
	rec := NewHarness([]byte(`{"type":"result","subtype":"success"}`))
	rec.Ts = base.Add(time.Second).Format(time.RFC3339Nano)
	rt.activityPublishedAt = time.Now()
	rt.feedAndEmitLocked(rec)
	if got := cap.all(); len(got) != 3 || !got[2].Equal(base.Add(time.Second)) {
		t.Fatalf("completion timestamp = %v", got)
	}
	rt.feedAndEmitLocked(NewSessionEnded())
	if len(cap.all()) != 3 {
		t.Fatal("session disposal refreshed activity")
	}
}

func TestRuntimeActivity_UnstartedRuntimeDoesNotPublish(t *testing.T) {
	rt, _ := newRuntimeWithNudge(t, nil)
	cap := &activityCapture{}
	rt.SetActivityCallback(time.Now(), cap.callback)
	rt.Stop() // manager may discard a concurrently-created duplicate runtime
	if len(cap.all()) != 0 {
		t.Fatal("discarded runtime overwrote activity")
	}
}
