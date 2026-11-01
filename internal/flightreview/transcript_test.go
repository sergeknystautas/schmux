package flightreview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/chat"
)

func TestReadTranscriptSequencesRecordsAndPreservesMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	log, err := chat.OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	user := chat.NewUserMessage("first", nil)
	for _, rec := range []chat.Record{
		user,
		chat.NewUserMessageDispatch(user.ID),
		chat.NewUserMessageQueue(user.ID, true),
	} {
		if err := log.Append(rec); err != nil {
			t.Fatal(err)
		}
	}
	appendRaw(t, path, "not-json\n")
	for _, rec := range []chat.Record{
		chat.NewControl([]byte(`{"type":"control_request"}`)),
		chat.NewHarness([]byte(`{"type":"assistant"}`)),
	} {
		if err := log.Append(rec); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(path)

	got, err := ReadTranscript(path, "executor", "s-1")
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}

	if got.Role != "executor" || got.SessionID != "s-1" {
		t.Errorf("identity = %s/%s", got.Role, got.SessionID)
	}
	if len(got.Records) != 5 {
		t.Fatalf("records = %d, want 5", len(got.Records))
	}
	wantTypes := []chat.RecordType{chat.RecordUserMessage, chat.RecordUserMessageDispatch, chat.RecordUserMessageQueue, chat.RecordControl, chat.RecordHarness}
	for i, rec := range got.Records {
		if rec.Seq != i+1 || rec.Record.Type != wantTypes[i] {
			t.Errorf("record %d = seq %d type %s, want seq %d type %s", i, rec.Seq, rec.Record.Type, i+1, wantTypes[i])
		}
	}
	// The malformed line sits at file line 4; it consumes no sequence number.
	if got.Records[2].Line != 3 || got.Records[3].Line != 5 || got.Records[4].Line != 6 {
		t.Errorf("lines = %d %d %d, want 3 5 6", got.Records[2].Line, got.Records[3].Line, got.Records[4].Line)
	}
	if len(got.Malformed) != 1 || got.Malformed[0].Line != 4 || string(got.Malformed[0].Bytes) != "not-json\n" {
		t.Errorf("malformed = %+v", got.Malformed)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Error("ReadTranscript modified the transcript")
	}
}

func TestReadTranscriptMissingFileIsAnError(t *testing.T) {
	if _, err := ReadTranscript(filepath.Join(t.TempDir(), "gone.jsonl"), "planner", "s-1"); err == nil {
		t.Fatal("ReadTranscript returned no error for a missing transcript")
	}
}

func appendRaw(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}
