package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

// chatTestRouter stands up just the chat route so chi.URLParam works.
func chatTestRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/ws/chat/{id}", s.handleChatWebSocket)
	return r
}

func TestChatWebSocket_Rejections(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: t.TempDir()})
	st.AddSession(state.Session{ID: "term", WorkspaceID: "ws-1", Target: "claude", CreatedAt: time.Now()})
	st.AddSession(state.Session{ID: "dead", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: 999999999, CreatedAt: time.Now()})
	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	for _, tc := range []struct {
		id   string
		want int
	}{{"nope", 404}, {"term", 400}, {"dead", 410}} {
		resp, err := http.Get(ts.URL + "/ws/chat/" + tc.id)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s: got %d want %d", tc.id, resp.StatusCode, tc.want)
		}
	}
}

func TestChatWebSocket_HistoryThenLive(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: wsPath})
	// A running chat session: the pid is this test process, so IsRunning is true.
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
		Type     string        `json:"type"`
		Protocol string        `json:"protocol"`
		Records  []chat.Record `json:"records"`
		Record   chat.Record   `json:"record"`
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "history" || frame.Protocol != "claude-stream-json" || len(frame.Records) != 1 || frame.Records[0].Text != "earlier" {
		t.Fatalf("history frame: %+v err=%v", frame, err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "send", "text": "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "record" || frame.Record.Text != "hello" {
		t.Fatalf("live frame: %+v err=%v", frame, err)
	}
	var in []byte
	if !waitFor(time.Second, func() bool {
		in, _ = os.ReadFile(paths.Input)
		return strings.Contains(string(in), `"hello"`)
	}) {
		t.Fatalf("input not written: %s", in)
	}
	// Harness output is forwarded.
	f, _ := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"result","subtype":"success"}` + "\n")
	f.Close()
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "record" || frame.Record.Type != chat.RecordHarness {
		t.Fatalf("harness frame: %+v err=%v", frame, err)
	}
}
