package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSequenceRecordsAssignsOneThroughN(t *testing.T) {
	records := []Record{
		NewUserMessage("one", nil),
		NewHarness([]byte(`{"type":"system","subtype":"init"}`)),
		NewControl([]byte(`{"type":"control_request"}`)),
	}
	if got := SequenceRecords(records); got != 3 {
		t.Fatalf("last sequence = %d, want 3", got)
	}
	for i, rec := range records {
		if rec.Seq != uint64(i+1) {
			t.Fatalf("record %d sequence = %d", i, rec.Seq)
		}
	}
}

func TestRuntimeSubscribeReturnsDurableSuffix(t *testing.T) {
	rt, _ := newTestRuntime(t)
	l, err := OpenLog(rt.paths.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []Record{
		NewUserMessage("one", nil),
		NewHarness([]byte(`{"type":"system","subtype":"init"}`)),
		NewHarness([]byte(`{"type":"assistant"}`)),
	} {
		if err := l.Append(rec); err != nil {
			t.Fatal(err)
		}
	}

	sub, err := rt.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)
	if sub.LastSeq != 3 || sub.Reset || len(sub.History) != 2 {
		t.Fatalf("suffix = %+v", sub)
	}
	if sub.History[0].Seq != 2 || sub.History[1].Seq != 3 {
		t.Fatalf("suffix sequences = %d, %d", sub.History[0].Seq, sub.History[1].Seq)
	}
}

func TestRuntimeSubscribeIncludesHeldOverlayWithoutSequence(t *testing.T) {
	rt, _ := newTestRuntime(t)
	l, _ := OpenLog(rt.paths.Conversation)
	l.Append(NewUserMessage("held", nil))

	rt.mu.Lock()
	rt.held = append(rt.held, Record{ID: "held-id"})
	rt.mu.Unlock()

	sub, err := rt.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)
	if len(sub.History) != 1 || sub.History[0].Type != RecordUserMessageQueue || sub.History[0].Seq != 0 {
		t.Fatalf("overlay history = %+v", sub.History)
	}
	if sub.LastSeq != 1 {
		t.Fatalf("overlay advanced LastSeq: %+v", sub)
	}
}

func TestRuntimeSubscribeFutureAfterResets(t *testing.T) {
	rt, _ := newTestRuntime(t)
	l, _ := OpenLog(rt.paths.Conversation)
	l.Append(NewUserMessage("one", nil))
	l.Append(NewHarness([]byte(`{"type":"assistant"}`)))

	sub, err := rt.Subscribe(3)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)
	if !sub.Reset || sub.LastSeq != 2 || len(sub.History) != 2 {
		t.Fatalf("future resume = %+v", sub)
	}
}

func TestRuntimeAppendSequencesFanoutButNotLog(t *testing.T) {
	rt, p := newTestRuntime(t)
	l, _ := OpenLog(p.Conversation)
	user := NewUserMessage("one", nil)
	l.Append(user)
	l.Append(NewUserMessageDispatch(user.ID))
	l.Append(NewHarness([]byte(`{"type":"system","subtype":"init"}`)))
	rt.Start()

	sub, err := rt.Subscribe(3)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)

	next := NewHarness([]byte(`{"type":"assistant"}`))
	rt.mu.Lock()
	err = rt.appendLocked(next)
	rt.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := recv(t, sub.Live); got.Seq != 4 {
		t.Fatalf("live sequence = %d, want 4", got.Seq)
	}

	persisted, err := l.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(persisted[len(persisted)-1])
	if strings.Contains(string(raw), `"seq"`) {
		t.Fatalf("sequence persisted to log: %s", raw)
	}
}
