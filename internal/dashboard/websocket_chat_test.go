package dashboard

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	r.Get("/api/chat/{id}/images/{messageID}/{index}", s.handleChatImage)
	return r
}

func TestCompactCodexHistoryDropsUnusedDiffSnapshotsAndDuplicateStartPatches(t *testing.T) {
	diff := chat.NewHarness([]byte(`{"method":"turn/diff/updated","params":{"diff":"snapshot"}}`))
	start := chat.NewHarness([]byte(`{"method":"item/started","params":{"item":{"id":"edit-1","type":"fileChange","changes":[{"path":"a.go","kind":"update","diff":"patch"}]}}}`))
	completed := chat.NewHarness([]byte(`{"method":"item/completed","params":{"item":{"id":"edit-1","type":"fileChange","changes":[{"path":"a.go","kind":"update","diff":"patch"}]}}}`))
	active := chat.NewHarness([]byte(`{"method":"item/started","params":{"item":{"id":"edit-2","type":"fileChange","changes":[{"path":"b.go","kind":"update","diff":"in progress"}]}}}`))

	source := []chat.Record{diff, start, completed, active}
	history := compactCodexHistory(source)
	if len(history) != 3 {
		t.Fatalf("history count = %d, want 3", len(history))
	}
	type browserItem struct {
		Params struct {
			Item struct {
				ID      string `json:"id"`
				Changes []struct {
					Path string `json:"path"`
					Kind string `json:"kind"`
					Diff string `json:"diff"`
				} `json:"changes"`
			} `json:"item"`
		} `json:"params"`
	}
	var items []browserItem
	for _, rec := range history {
		var item browserItem
		if err := json.Unmarshal(rec.Line, &item); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if items[0].Params.Item.ID != "edit-1" || items[0].Params.Item.Changes[0].Path != "a.go" ||
		items[0].Params.Item.Changes[0].Kind != "update" || items[0].Params.Item.Changes[0].Diff != "" {
		t.Fatalf("completed edit's start should retain identity and path, not patch: %+v", items[0])
	}
	if items[1].Params.Item.Changes[0].Diff != "patch" {
		t.Fatalf("completed patch was lost: %+v", items[1])
	}
	if items[2].Params.Item.Changes[0].Diff != "in progress" {
		t.Fatalf("active patch was lost: %+v", items[2])
	}
	if !bytes.Contains(start.Line, []byte(`"diff":"patch"`)) {
		t.Fatal("original record was changed")
	}
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
	}{{"nope", 404}, {"term", 400}} {
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

func TestChatWebSocket_EndedSessionSendsHistoryThenCloses(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: t.TempDir()})
	st.AddSession(state.Session{ID: "ended", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: 999999999, CreatedAt: time.Now()})
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "ended"))
	paths.Ensure()
	l, _ := chat.OpenLog(paths.Conversation)
	l.Append(chat.NewUserMessage("ended history", nil))

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/ended", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var frame struct {
		Type     string        `json:"type"`
		Protocol string        `json:"protocol"`
		Records  []chat.Record `json:"records"`
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "history" || frame.Protocol != "claude-stream-json" || len(frame.Records) != 1 || frame.Records[0].Text != "ended history" {
		t.Fatalf("ended history frame: %+v err=%v", frame, err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("ended chat socket remained open after its history frame")
	}
}

func TestChatWebSocket_CodexHistoryOmitsUserMessageEchoes(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: t.TempDir()})
	st.AddSession(state.Session{ID: "ended", WorkspaceID: "ws-1", Target: "codex", Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex, Pid: 999999999, CreatedAt: time.Now()})
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "ended"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	l, err := chat.OpenLog(paths.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []chat.Record{
		chat.NewUserMessage("look at this", []chat.Image{{MediaType: "image/png", Data: "AAAA"}}),
		chat.NewHarness([]byte(`{"method":"item/started","params":{"item":{"type":"userMessage","content":[{"type":"image","data":"AAAA"}]}}}`)),
		chat.NewHarness([]byte(`{"method":"item/completed","params":{"item":{"type":"userMessage","content":[{"type":"image","data":"AAAA"}]}}}`)),
		chat.NewHarness([]byte(`{"method":"item/completed","params":{"item":{"type":"agentMessage","text":"done"}}}`)),
	} {
		if err := l.Append(rec); err != nil {
			t.Fatal(err)
		}
	}

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/ended", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var frame struct {
		Type     string        `json:"type"`
		Protocol string        `json:"protocol"`
		Records  []chat.Record `json:"records"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "history" || frame.Protocol != chat.ProtocolCodex || len(frame.Records) != 2 ||
		frame.Records[0].Type != chat.RecordUserMessage || len(frame.Records[0].Images) != 1 ||
		frame.Records[1].Line == nil || !strings.Contains(string(frame.Records[1].Line), `"agentMessage"`) {
		t.Fatalf("filtered Codex history: %+v", frame)
	}
	persisted, err := l.ReadAll()
	if err != nil || len(persisted) != 4 {
		t.Fatalf("persisted history: count=%d err=%v", len(persisted), err)
	}
}

func TestChatHistoryLoadsLegacyImageFromWorkspacePreviewCache(t *testing.T) {
	srv, _, st := newTestServer(t)
	workspacePath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: workspacePath})
	st.AddSession(state.Session{ID: "ended", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: 999999999, CreatedAt: time.Now()})
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "ended"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	log, err := chat.OpenLog(paths.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	originalData := base64.StdEncoding.EncodeToString(encoded.Bytes())
	message := chat.NewUserMessage("look", []chat.Image{{MediaType: "image/png", Data: originalData, Path: "/tmp/missing-chat-image.png"}})
	if err := log.Append(message); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/ended", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var frame struct {
		Records []chat.Record `json:"records"`
	}
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if len(frame.Records) != 1 || len(frame.Records[0].Images) != 1 {
		t.Fatalf("history records: %+v", frame.Records)
	}
	preview := frame.Records[0].Images[0]
	if preview.Data != "" || preview.Path != "" || preview.PreviewURL == "" || preview.PreviewWidth != 2 || preview.PreviewHeight != 1 {
		t.Fatalf("history must reference a sized preview without inline data: %+v", preview)
	}
	cacheDir := filepath.Join(state.SchmuxDataDir(workspacePath), "cache", "chat-images")
	previewPath := chat.PreviewPath(cacheDir, "ended", message.ID, 0)
	if _, err := os.Stat(previewPath); !os.IsNotExist(err) {
		t.Fatalf("legacy preview should be created on request, stat err=%v", err)
	}
	resp, err := http.Get(ts.URL + preview.PreviewURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("image GET: %d", resp.StatusCode)
	}
	served, err := png.Decode(resp.Body)
	if err != nil || served.Bounds().Dx() != 2 || served.Bounds().Dy() != 1 {
		t.Fatalf("served preview: bounds=%v err=%v", served, err)
	}
	if _, err := os.Stat(previewPath); err != nil {
		t.Fatalf("workspace preview cache: %v", err)
	}
	persisted, err := log.ReadAll()
	if err != nil || len(persisted) != 1 || persisted[0].Images[0].Data != originalData {
		t.Fatalf("original log image was changed: records=%d err=%v", len(persisted), err)
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
	if err := conn.ReadJSON(&frame); err != nil ||
		frame.Type != "history" || frame.Protocol != "claude-stream-json" ||
		len(frame.Records) != 2 || frame.Records[0].Text != "earlier" ||
		frame.Records[1].Type != chat.RecordClaudeTakeover {
		t.Fatalf("history frame: %+v err=%v", frame, err)
	}
	// The pre-marker history is an open legacy turn. Finish it before sending;
	// otherwise Runtime correctly holds the new message behind that turn.
	f, _ := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"result","subtype":"success","queued_turn_count":0}` + "\n")
	f.Close()
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "record" || frame.Record.Type != chat.RecordHarness {
		t.Fatalf("legacy result frame: %+v err=%v", frame, err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "send", "text": "hello"}); err != nil {
		t.Fatal(err)
	}
	// Claude appends a user_message_dispatch marker before the input
	// write; both records are fanned out. Read until the user_message
	// frame arrives, then drain the dispatch marker.
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "record" || frame.Record.Text != "hello" {
		t.Fatalf("live frame: %+v err=%v", frame, err)
	}
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "record" || frame.Record.Type != chat.RecordUserMessageDispatch {
		t.Fatalf("dispatch marker frame: %+v err=%v", frame, err)
	}
	var in []byte
	if !waitFor(time.Second, func() bool {
		in, _ = os.ReadFile(paths.Input)
		return strings.Contains(string(in), `"hello"`)
	}) {
		t.Fatalf("input not written: %s", in)
	}
	// Harness output is forwarded.
	f, _ = os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"result","subtype":"success"}` + "\n")
	f.Close()
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "record" || frame.Record.Type != chat.RecordHarness {
		t.Fatalf("harness frame: %+v err=%v", frame, err)
	}
}
