package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLog_AppendReadCount(t *testing.T) {
	dir := t.TempDir()
	path := ConversationPath(dir)
	if path != filepath.Join(dir, "conversation.jsonl") {
		t.Fatalf("path %q", path)
	}
	l, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	um := NewUserMessage("hello", nil)
	if um.ID == "" || um.Ts == "" || um.Type != RecordUserMessage {
		t.Fatalf("bad user message %+v", um)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(l.Append(um))
	must(l.Append(NewHarness([]byte(`{"type":"system","subtype":"init","session_id":"abc"}`))))
	must(l.Append(NewControl([]byte(`{"type":"control_request","request_id":"int-1","request":{"subtype":"interrupt"}}`))))
	must(l.Append(NewHarness([]byte(`{"type":"result","subtype":"success"}`))))

	recs, err := l.ReadAll()
	must(err)
	if len(recs) != 4 || recs[0].Text != "hello" || recs[1].Type != RecordHarness || recs[2].Type != RecordControl {
		t.Fatalf("records: %+v", recs)
	}
	var line map[string]any
	must(json.Unmarshal(recs[1].Line, &line))
	if line["session_id"] != "abc" {
		t.Fatalf("line not embedded verbatim: %s", recs[1].Line)
	}

	dst := filepath.Join(t.TempDir(), "copy.jsonl")
	must(CopyLog(path, dst))
	a, _ := os.ReadFile(path)
	b, _ := os.ReadFile(dst)
	if string(a) != string(b) {
		t.Fatal("copy differs")
	}
}

func TestOpenLog_MissingFileIsEmpty(t *testing.T) {
	l, err := OpenLog(filepath.Join(t.TempDir(), "nested", "x.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	recs, err := l.ReadAll()
	if err != nil || len(recs) != 0 {
		t.Fatalf("recs=%v err=%v", recs, err)
	}
}

func TestUserMessageLine(t *testing.T) {
	line, err := UserMessageLine("hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != `{"type":"user","message":{"role":"user","content":"hi"}}` {
		t.Fatalf("text-only line: %s", line)
	}
	line, err = UserMessageLine("look", []Image{{MediaType: "image/png", Data: "AAAA"}})
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Message.Content) != 2 || v.Message.Content[0]["type"] != "text" || v.Message.Content[1]["type"] != "image" {
		t.Fatalf("image line: %s", line)
	}
}

func TestNewSessionEnded(t *testing.T) {
	rec := NewSessionEnded()
	if rec.Type != RecordSession || rec.Event != "ended" || rec.Ts == "" {
		t.Fatalf("bad ended record %+v", rec)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(line)
	if !strings.Contains(s, `"type":"session"`) || !strings.Contains(s, `"event":"ended"`) {
		t.Fatalf("marshal: %s", s)
	}
	dir := t.TempDir()
	path := ConversationPath(dir)
	l, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append(rec); err != nil {
		t.Fatal(err)
	}
	recs, err := l.ReadAll()
	if err != nil || len(recs) != 1 || recs[0].Type != RecordSession || recs[0].Event != "ended" {
		t.Fatalf("recs=%+v err=%v", recs, err)
	}
}
