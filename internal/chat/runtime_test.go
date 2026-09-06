package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustProto(t *testing.T) Protocol {
	t.Helper()
	p, err := ProtocolFor(ProtocolClaude)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestRuntime(t *testing.T) (*Runtime, Paths) {
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
	return rt, p
}

func recv(t *testing.T, ch <-chan Record) Record {
	t.Helper()
	select {
	case r, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("no record within 2s")
	}
	return Record{}
}

func TestRuntime_SendRecordsBeforeInput(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()
	_, live, err := rt.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := rt.Send("hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := recv(t, live); got.ID != rec.ID || got.Type != RecordUserMessage {
		t.Fatalf("fan-out got %+v", got)
	}
	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	if len(recs) != 1 || recs[0].Text != "hello" {
		t.Fatalf("record: %+v", recs)
	}
	in, _ := os.ReadFile(p.Input)
	if !strings.Contains(string(in), `"content":"hello"`) {
		t.Fatalf("input: %s", in)
	}
}

func TestRuntime_TailsOutputIntoRecord(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()
	_, live, _ := rt.Subscribe()
	f, _ := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"system","subtype":"init","session_id":"abc"}` + "\n")
	f.WriteString(`{"type":"result","subtype":"success"}` + "\n")
	f.Close()
	a := recv(t, live)
	b := recv(t, live)
	if a.Type != RecordHarness || !strings.Contains(string(a.Line), `"init"`) || !strings.Contains(string(b.Line), `"result"`) {
		t.Fatalf("got %+v %+v", a, b)
	}
}

func TestRuntime_RestartSkipsConsumedLines(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	// Two lines already consumed and one not yet.
	l, _ := OpenLog(p.Conversation)
	l.Append(NewUserMessage("q", nil))
	l.Append(NewHarness([]byte(`{"type":"system","subtype":"init"}`)))
	l.Append(NewHarness([]byte(`{"type":"assistant"}`)))
	os.WriteFile(p.Output, []byte("{\"type\":\"system\",\"subtype\":\"init\"}\n{\"type\":\"assistant\"}\n{\"type\":\"result\"}\n"), 0o644)

	rt, err := NewRuntime("s1", mustProto(t), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	hist, live, _ := rt.Subscribe()
	if len(hist) != 3 {
		t.Fatalf("history %d", len(hist))
	}
	rt.Start()
	got := recv(t, live)
	if !strings.Contains(string(got.Line), `"result"`) {
		t.Fatalf("expected only the unconsumed line, got %s", got.Line)
	}
	select {
	case extra := <-live:
		t.Fatalf("duplicate: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRuntime_InterruptAndAnswersWriteControlThenInput(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()
	if err := rt.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := rt.AnswerPermission("req-1", false, nil, "denied from UI"); err != nil {
		t.Fatal(err)
	}
	if err := rt.AnswerQuestion("req-2", map[string][]string{"Which?": {"Beta"}}, json.RawMessage(`{"questions":[{"question":"Which?"}]}`)); err != nil {
		t.Fatal(err)
	}
	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	if len(recs) != 3 {
		t.Fatalf("records: %d", len(recs))
	}
	for i, want := range []string{`"subtype":"interrupt"`, `"behavior":"deny"`, `"answers":{"Which?":"Beta"}`} {
		if recs[i].Type != RecordControl || !strings.Contains(string(recs[i].Line), want) {
			t.Fatalf("record %d: %s", i, recs[i].Line)
		}
	}
	in, _ := os.ReadFile(p.Input)
	lines := strings.Split(strings.TrimSpace(string(in)), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], `"request_id":"req-1"`) || !strings.Contains(lines[2], `"request_id":"req-2"`) {
		t.Fatalf("input lines: %q", lines)
	}
}

func TestRuntime_ResumeIDEvent(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(dir)
	p.Ensure()
	eventsFile := dir + "/events.jsonl"
	rt, err := NewRuntime("s1", mustProto(t), p, eventsFile, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.Start()
	f, _ := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"system","subtype":"init","session_id":"conv-9"}` + "\n")
	f.WriteString(`{"type":"system","subtype":"init","session_id":"conv-9"}` + "\n")
	f.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(eventsFile); strings.Count(string(b), `"resume_id"`) == 1 && strings.Contains(string(b), `"id":"conv-9"`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(eventsFile)
	t.Fatalf("expected exactly one resume_id event, got: %s", b)
}

func TestRuntime_StreamEventsForwardedNotRecorded(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()
	_, live, _ := rt.Subscribe()
	f, _ := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}` + "\n")
	f.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}` + "\n")
	f.Close()
	a := recv(t, live)
	b := recv(t, live)
	if !strings.Contains(string(a.Line), `"stream_event"`) || !strings.Contains(string(b.Line), `"assistant"`) {
		t.Fatalf("live order: %s / %s", a.Line, b.Line)
	}
	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	if len(recs) != 1 || !strings.Contains(string(recs[0].Line), `"assistant"`) {
		t.Fatalf("expected only the assistant record stored, got %+v", recs)
	}
}

func TestRuntime_RestartSkipsRecordableLinesOnly(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(dir)
	p.Ensure()
	l, _ := OpenLog(p.Conversation)
	l.Append(NewUserMessage("q", nil))
	l.Append(NewHarness([]byte(`{"type":"system","subtype":"init"}`)))
	// Output: init, two deltas, assistant (unconsumed), result (unconsumed).
	os.WriteFile(p.Output, []byte(
		"{\"type\":\"system\",\"subtype\":\"init\"}\n"+
			"{\"type\":\"stream_event\",\"event\":{\"type\":\"content_block_start\",\"index\":0}}\n"+
			"{\"type\":\"stream_event\",\"event\":{\"type\":\"content_block_delta\",\"index\":0}}\n"+
			"{\"type\":\"assistant\"}\n"+
			"{\"type\":\"result\"}\n"), 0o644)
	rt, _ := NewRuntime("s1", mustProto(t), p, "", nil, nil)
	t.Cleanup(rt.Stop)
	_, live, _ := rt.Subscribe()
	rt.Start()
	var seen []string
	for i := 0; i < 4; i++ {
		select {
		case r := <-live:
			seen = append(seen, string(r.Line))
		case <-time.After(500 * time.Millisecond):
		}
	}
	joined := strings.Join(seen, "|")
	if strings.Contains(joined, `"init"`) {
		t.Fatalf("init re-consumed: %s", joined)
	}
	if !strings.Contains(joined, `"assistant"`) || !strings.Contains(joined, `"result"`) {
		t.Fatalf("unconsumed durable lines not tailed: %s", joined)
	}
	recs, _ := l.ReadAll()
	n := 0
	for _, rec := range recs {
		if rec.Type == RecordHarness {
			n++
		}
	}
	if n != 3 || len(recs) != 4 {
		t.Fatalf("expected init+assistant+result recorded (3 harness of %d), got %d", len(recs), n)
	}
}

func TestRuntime_EndAppendsSessionRecordOnce(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()
	_, live, _ := rt.Subscribe()
	if err := rt.End(); err != nil {
		t.Fatal(err)
	}
	if err := rt.End(); err != nil {
		t.Fatal(err)
	}
	got := recv(t, live)
	if got.Type != RecordSession || got.Event != "ended" {
		t.Fatalf("got %+v", got)
	}
	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	if len(recs) != 1 {
		t.Fatalf("End must be idempotent, got %d records", len(recs))
	}
}

func TestRuntime_ConcurrentSendsKeepRecordAndInputInOrder(t *testing.T) {
	rt, p := newTestRuntime(t)
	rt.Start()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := rt.Send(fmt.Sprintf("m%02d", i), nil); err != nil {
				t.Errorf("Send: %v", err)
			}
		}(i)
	}
	wg.Wait()
	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	in, _ := os.ReadFile(p.Input)
	lines := strings.Split(strings.TrimSpace(string(in)), "\n")
	if len(recs) != 20 || len(lines) != 20 {
		t.Fatalf("counts %d %d", len(recs), len(lines))
	}
	for i := range recs {
		if !strings.Contains(lines[i], `"content":"`+recs[i].Text+`"`) {
			t.Fatalf("order differs at %d: record %q input %s", i, recs[i].Text, lines[i])
		}
	}
}
