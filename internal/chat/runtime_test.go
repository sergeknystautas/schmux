package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	rt, err := NewRuntime("s1", mustProto(t), p, "", "", nil, nil)
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
	if len(recs) != 2 || recs[0].Text != "hello" || recs[1].Type != RecordUserMessageDispatch || recs[1].ID != recs[0].ID {
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

	rt, err := NewRuntime("s1", mustProto(t), p, "", "", nil, nil)
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
	if got.Type != RecordClaudeTakeover {
		t.Fatalf("expected takeover marker before unconsumed output, got %+v", got)
	}
	got = recv(t, live)
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
	rt, err := NewRuntime("s1", mustProto(t), p, "", eventsFile, nil, nil)
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
	rt, _ := NewRuntime("s1", mustProto(t), p, "", "", nil, nil)
	t.Cleanup(rt.Stop)
	_, live, _ := rt.Subscribe()
	rt.Start()
	var seen []string
	sawTakeover := false
	for i := 0; i < 5; i++ {
		select {
		case r := <-live:
			if r.Type == RecordClaudeTakeover {
				sawTakeover = true
				continue
			}
			seen = append(seen, string(r.Line))
		case <-time.After(500 * time.Millisecond):
		}
	}
	if !sawTakeover {
		t.Fatalf("takeover marker missing from live frames: %s", strings.Join(seen, "|"))
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
	if n != 3 || len(recs) != 5 {
		t.Fatalf("expected takeover+init+assistant+result recorded (3 harness of %d), got %d", len(recs), n)
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
	// Claude holds every send after the first one while a turn is active.
	// After 20 concurrent sends only the first wins the dispatch gate;
	// the remaining 19 sit in the held queue. The first send records both
	// a user_message and a user_message_dispatch; each held send records
	// only its user_message until a future flush appends a marker.
	if len(recs) != 21 || len(lines) != 1 {
		t.Fatalf("after concurrent sends records=%d input=%d", len(recs), len(lines))
	}
	// Drain 19 results; each one releases the oldest held message.
	for range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18} {
		writeClaudeOutput(t, p, `{"type":"result","subtype":"success","queued_turn_count":0}`)
		rt.drain()
	}
	in, _ = os.ReadFile(p.Input)
	lines = strings.Split(strings.TrimSpace(string(in)), "\n")
	if len(lines) != 20 {
		t.Fatalf("after draining 19 results input=%d", len(lines))
	}
	var want []string
	for _, rec := range recs {
		if rec.Type != RecordUserMessage {
			continue
		}
		want = append(want, rec.Text)
	}
	for i := range lines {
		if !strings.Contains(lines[i], `"content":"`+want[i]+`"`) {
			t.Fatalf("order differs at %d: record %q input %s", i, want[i], lines[i])
		}
	}
}

func TestRuntime_AbortRecordsControlThenInput(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	paths.Ensure()
	proto, _ := ProtocolFor(ProtocolCodex)
	rt, err := NewRuntime("s1", proto, paths, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.Start()
	if err := rt.Abort("3"); err != nil {
		t.Fatal(err)
	}
	recs, _ := (&Log{path: paths.Conversation}).ReadAll()
	if len(recs) != 1 || recs[0].Type != RecordControl || !strings.Contains(string(recs[0].Line), `"error"`) {
		t.Fatalf("records: %+v", recs)
	}
	in, _ := os.ReadFile(paths.Input)
	if !strings.Contains(string(in), `"id":3,"error"`) {
		t.Fatalf("input: %s", in)
	}
}

func TestRuntime_SendPersistsImages(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(t.TempDir())
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime("s1", mustProto(t), p, dir, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)

	images := []Image{{MediaType: "image/png", Data: "aGVsbG8=", Path: "/tmp/evil.png"}}
	rec, err := rt.Send("look", images)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Images[0].Path == "" || rec.Images[0].Path == "/tmp/evil.png" {
		t.Fatalf("daemon-assigned path missing or inbound path survived: %q", rec.Images[0].Path)
	}
	if filepath.Dir(rec.Images[0].Path) != dir {
		t.Fatalf("path %q not in %s", rec.Images[0].Path, dir)
	}
	if b, _ := os.ReadFile(rec.Images[0].Path); string(b) != "hello" {
		t.Fatalf("persisted content %q", b)
	}
	if images[0].Path != "/tmp/evil.png" {
		t.Fatal("caller's slice was mutated")
	}
	in, _ := os.ReadFile(p.Input)
	if !strings.Contains(string(in), "Image #1: "+rec.Images[0].Path) {
		t.Fatalf("input lacks path suffix: %s", in)
	}
}

func TestRuntime_SendImagePersistFailureDegrades(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime("s1", mustProto(t), p, blocker, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)

	rec, err := rt.Send("look", []Image{{MediaType: "image/png", Data: "aGVsbG8="}})
	if err != nil {
		t.Fatalf("persist failure must not block the send: %v", err)
	}
	if rec.Images[0].Path != "" {
		t.Fatalf("path %q set despite persist failure", rec.Images[0].Path)
	}
	in, _ := os.ReadFile(p.Input)
	if strings.Contains(string(in), "Image #1:") {
		t.Fatalf("input must lack the suffix: %s", in)
	}
	if !strings.Contains(string(in), `"data":"aGVsbG8="`) {
		t.Fatalf("inline base64 block must stay: %s", in)
	}
}

func TestRuntime_HeldImageKeepsPathSuffix(t *testing.T) {
	// Codex holds user messages until its thread id and account check arrive;
	// the flush happens from the runtime's output tail loop, so the handshake
	// results must be written to p.Output, not fed to the protocol directly.
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	proto, err := ProtocolFor(ProtocolCodex)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime("s1", proto, p, dir, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.Start()

	rec, err := rt.Send("look", []Image{{MediaType: "image/png", Data: "aGVsbG8="}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Images[0].Path == "" {
		t.Fatal("held message must still persist the image")
	}
	want := "Image #1: " + rec.Images[0].Path

	f, err := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"id":2,"result":{"account":{"id":"acct"}}}` + "\n")
	f.WriteString(`{"id":3,"result":{"thread":{"id":"t-1"}}}` + "\n")
	f.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		in, _ := os.ReadFile(p.Input)
		if strings.Contains(string(in), want) {
			return // flushed with the suffix
		}
		time.Sleep(10 * time.Millisecond)
	}
	in, _ := os.ReadFile(p.Input)
	t.Fatalf("held flush never wrote the path suffix; input: %s", in)
}

// userInputTexts decodes each JSON input line and returns the user-message
// content strings in order. Claude input lines have type:"user"; control
// responses and other control lines are skipped.
func userInputTexts(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	if err := eachLine(path, func(line []byte) {
		var v struct {
			Type    string `json:"type"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &v) != nil || v.Type != "user" {
			return
		}
		out = append(out, v.Message.Content)
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeClaudeOutput(t *testing.T, p Paths, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(p.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntime_ClaudeHoldsAndDispatchesOneMessageAtATime(t *testing.T) {
	rt, p := newTestRuntime(t)

	if _, err := rt.Send("A", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("B", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("C", nil); err != nil {
		t.Fatal(err)
	}
	if got := userInputTexts(t, p.Input); len(got) != 1 || got[0] != "A" {
		t.Fatalf("before A's result, input = %v", got)
	}

	writeClaudeOutput(t, p, `{"type":"result","subtype":"success","queued_turn_count":0}`)
	rt.drain()
	if got := userInputTexts(t, p.Input); len(got) != 2 || got[1] != "B" {
		t.Fatalf("after A's result, input = %v", got)
	}

	writeClaudeOutput(t, p, `{"type":"result","subtype":"error_during_execution","is_error":true}`)
	rt.drain()
	if got := userInputTexts(t, p.Input); len(got) != 3 || got[2] != "C" {
		t.Fatalf("after B's terminal error, input = %v", got)
	}
}

func TestRuntime_ClaudeDispatchMarkerIsIdempotentOnInputRetry(t *testing.T) {
	rt, p := newTestRuntime(t)
	if _, err := rt.Send("A", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("B", nil); err != nil {
		t.Fatal(err)
	}

	originalAppend := rt.appendInput
	rt.appendInput = func([]byte) error { return errors.New("simulated input failure") }
	writeClaudeOutput(t, p, `{"type":"result","subtype":"success","queued_turn_count":0}`)
	rt.drain()
	if got := userInputTexts(t, p.Input); len(got) != 1 {
		t.Fatalf("failed B append must not reach input: %v", got)
	}

	rt.appendInput = originalAppend
	rt.mu.Lock()
	rt.flushHeldLocked()
	rt.mu.Unlock()
	if got := userInputTexts(t, p.Input); len(got) != 2 || got[1] != "B" {
		t.Fatalf("B retry = %v", got)
	}

	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	count := 0
	for _, rec := range recs {
		if rec.Type == RecordUserMessageDispatch && rec.ID != "" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected one marker each for A and B, got %d", count)
	}
}

func TestRuntime_ClaudeRebuildDoesNotDuplicateHeldMessage(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}

	a := NewUserMessage("A", nil)
	b := NewUserMessage("B", nil)
	c := NewUserMessage("C", nil)
	result := `{"type":"result","subtype":"success","queued_turn_count":0}`
	aLine, _ := UserMessageLine("A", nil)
	bLine, _ := UserMessageLine("B", nil)
	input := append(append(append([]byte{}, aLine...), '\n'), append(append([]byte{}, bLine...), '\n')...)
	if err := os.WriteFile(p.Input, input, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Output, []byte(result+"\n"+result+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := OpenLog(p.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []Record{
		a,
		NewUserMessageDispatch(a.ID),
		NewHarness([]byte(result)),
		b,
		NewUserMessageDispatch(b.ID),
		NewHarness([]byte(result)),
		c,
	} {
		if err := l.Append(rec); err != nil {
			t.Fatal(err)
		}
	}

	rt, err := NewRuntime("s1", mustProto(t), p, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.Start()
	if got := userInputTexts(t, p.Input); len(got) != 3 || got[2] != "C" {
		t.Fatalf("initial rebuild dispatch = %v", got)
	}

	writeClaudeOutput(t, p, result)
	rt.drain()
	if got := userInputTexts(t, p.Input); len(got) != 3 {
		t.Fatalf("C was dispatched more than once: %v", got)
	}
}

func TestRuntime_ClaudeLegacyQueueDrainsBeforeTakeover(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}

	a := NewUserMessage("A", nil)
	b := NewUserMessage("B", nil)
	firstResult := `{"type":"result","subtype":"success","queued_turn_count":0}`
	aLine, _ := UserMessageLine("A", nil)
	bLine, _ := UserMessageLine("B", nil)
	input := append(append(append([]byte{}, aLine...), '\n'), append(append([]byte{}, bLine...), '\n')...)
	if err := os.WriteFile(p.Input, input, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Output, []byte(firstResult+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := OpenLog(p.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []Record{a, b, NewHarness([]byte(firstResult))} {
		if err := l.Append(rec); err != nil {
			t.Fatal(err)
		}
	}

	rt, err := NewRuntime("s1", mustProto(t), p, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.Start()
	if _, err := rt.Send("C", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("D", nil); err != nil {
		t.Fatal(err)
	}
	if got := userInputTexts(t, p.Input); len(got) != 2 {
		t.Fatalf("C and D must wait for legacy native queue B: %v", got)
	}

	writeClaudeOutput(
		t,
		p,
		`{"type":"user","isReplay":true,"message":{"content":"B"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"B done"}]}}`,
		`{"type":"result","subtype":"success","queued_turn_count":0}`,
	)
	rt.drain()
	got := userInputTexts(t, p.Input)
	if len(got) != 3 || got[2] != "C" {
		t.Fatalf("C must dispatch only after B completes: %v", got)
	}

	writeClaudeOutput(t, p, `{"type":"result","subtype":"success","queued_turn_count":0}`)
	rt.drain()
	got = userInputTexts(t, p.Input)
	if len(got) != 4 || got[3] != "D" {
		t.Fatalf("D must dispatch only after C completes: %v", got)
	}
}

func TestRuntime_ClaudeLegacyTakeoverSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	p := PathsFor(dir)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}

	a := NewUserMessage("A", nil)
	b := NewUserMessage("B", nil)
	firstResult := `{"type":"result","subtype":"success","queued_turn_count":0}`
	aLine, _ := UserMessageLine("A", nil)
	bLine, _ := UserMessageLine("B", nil)
	input := append(append(append([]byte{}, aLine...), '\n'), append(append([]byte{}, bLine...), '\n')...)
	if err := os.WriteFile(p.Input, input, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Output, []byte(firstResult+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := OpenLog(p.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []Record{a, b, NewHarness([]byte(firstResult))} {
		if err := l.Append(rec); err != nil {
			t.Fatal(err)
		}
	}

	first, err := NewRuntime("s1", mustProto(t), p, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.Start()
	for _, text := range []string{"C", "D"} {
		if _, err := first.Send(text, nil); err != nil {
			t.Fatal(err)
		}
	}
	first.Stop()

	restored, err := NewRuntime("s1", mustProto(t), p, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Stop)
	restored.Start()
	if got := userInputTexts(t, p.Input); len(got) != 2 {
		t.Fatalf("restart must not dispatch C or D while B is pending: %v", got)
	}

	writeClaudeOutput(
		t,
		p,
		`{"type":"user","isReplay":true,"message":{"content":"B"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"B done"}]}}`,
		`{"type":"result","subtype":"success","queued_turn_count":0}`,
	)
	restored.drain()
	if got := userInputTexts(t, p.Input); len(got) != 3 || got[2] != "C" {
		t.Fatalf("restart must dispatch only C after B completes: %v", got)
	}
}

func TestRuntime_ClaudeConcurrentSendsPreserveInputOrder(t *testing.T) {
	rt, p := newTestRuntime(t)
	if _, err := rt.Send("A", nil); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, text := range []string{"B", "C"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			if _, err := rt.Send(text, nil); err != nil {
				t.Errorf("send %s: %v", text, err)
			}
		}(text)
	}
	wg.Wait()

	recs, _ := (&Log{path: p.Conversation}).ReadAll()
	var want []string
	for _, rec := range recs {
		if rec.Type == RecordUserMessage {
			want = append(want, rec.Text)
		}
	}
	if len(want) != 3 {
		t.Fatalf("recorded user order = %v", want)
	}

	for range []int{0, 1} {
		writeClaudeOutput(t, p, `{"type":"result","subtype":"success","queued_turn_count":0}`)
		rt.drain()
	}
	got := userInputTexts(t, p.Input)
	if len(got) != len(want) {
		t.Fatalf("input = %v, record order = %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("input = %v, record order = %v", got, want)
		}
	}
}
