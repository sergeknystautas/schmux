package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestChatWebSocket_AnswerFrameCarriesLabelArrays(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: wsPath})
	st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: os.Getpid(), CreatedAt: time.Now()})
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "c1"))
	paths.Ensure()
	l, _ := chat.OpenLog(paths.Conversation)
	l.Append(chat.NewUserMessage("earlier", nil))

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var frame struct {
		Type    string        `json:"type"`
		Records []chat.Record `json:"records"`
		Record  chat.Record   `json:"record"`
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "answer", "request_id": "r1", "answers": map[string][]string{"Which?": {"A", "B"}}, "input": map[string]any{"questions": []any{}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn.SetReadDeadline(deadline)
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("record: %v", err)
		}
		if frame.Type == "record" {
			break
		}
	}
	if frame.Record.Type != chat.RecordControl {
		t.Fatalf("expected control record, got %+v", frame.Record)
	}
	if !strings.Contains(string(frame.Record.Line), "A, B") {
		t.Fatalf("line missing joined labels: %s", frame.Record.Line)
	}
	var decoded map[string]any
	_ = json.Unmarshal(frame.Record.Line, &decoded)
	_ = decoded
}
