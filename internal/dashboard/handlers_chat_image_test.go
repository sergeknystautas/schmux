package dashboard

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

type chatImageFixture struct {
	workspacePath string
	messageID     string
	dataDir       string
}

// setupChatImageFixture creates the workspace, chat session, schmux data dir,
// and a persisted conversation log holding a single user message with an
// inline image. The returned fixture is enough for handleChatImage to find
// either the cached preview or the conversation log.
func setupChatImageFixture(t *testing.T, srv *Server, st *state.State) chatImageFixture {
	t.Helper()
	workspacePath := t.TempDir()
	dataDir := filepath.Join(workspacePath, ".schmux")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspace(state.Workspace{ID: "ws-image", Repo: "r", Branch: "b", Path: workspacePath}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{
		ID: "chat-image", WorkspaceID: "ws-image", Target: "claude",
		Kind: state.SessionKindChat, Pid: 999999999, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// schmuxdir.Set is what the handler reads on a cache miss to locate the
	// persisted conversation log.
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	dataURI := base64.StdEncoding.EncodeToString(encoded.Bytes())

	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-image", "chat-image"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	logFile, err := chat.OpenLog(paths.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	msg := chat.NewUserMessage("look", []chat.Image{{
		MediaType: "image/png",
		Data:      dataURI,
	}})
	if err := logFile.Append(msg); err != nil {
		t.Fatal(err)
	}
	return chatImageFixture{workspacePath: workspacePath, messageID: msg.ID, dataDir: dataDir}
}

func writeCachedPreview(t *testing.T, fx chatImageFixture) {
	t.Helper()
	preview := chat.PreviewPath(filepath.Join(fx.dataDir, "cache", "chat-images"), "chat-image", fx.messageID, 0)
	if err := os.MkdirAll(filepath.Dir(preview), 0o700); err != nil {
		t.Fatal(err)
	}
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	f, err := os.Create(preview)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
}

func TestChatImageCollectsBaseFieldsOnCacheHit(t *testing.T) {
	srv, cfg, st := newTestServer(t)

	fx := setupChatImageFixture(t, srv, st)
	writeCachedPreview(t, fx)

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/chat/chat-image/images/" + fx.messageID + "/0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := readChatPerformanceEvents(t)
	if len(events) != 1 || events[0].Kind != "image" {
		t.Fatalf("events = %+v", events)
	}
	data := events[0].Data
	if data["session"] != "chat-image" || data["message"] != fx.messageID || data["index"] != float64(0) || data["cache_hit"] != true {
		t.Errorf("image event = %+v", data)
	}
	if _, ok := data["file_bytes"]; !ok {
		t.Errorf("file size missing: %+v", data)
	}
	if _, ok := data["lookup_ms"]; ok {
		t.Errorf("detailed fields captured while profiling disabled: %+v", data)
	}
	if cfg.GetChatLoadProfilingEnabled() {
		t.Fatal("chat load profiling should default off")
	}
}

func TestChatImageCollectsDetailedFieldsOnCacheMiss(t *testing.T) {
	srv, cfg, st := newTestServer(t)

	// No pre-populated cache: handler must scan the conversation log and
	// generate the preview itself. With profiling on, this exercises both the
	// lookup/serve stages and the miss-only log_scan/generate stages.
	fx := setupChatImageFixture(t, srv, st)
	cfg.ChatLoadProfilingEnabled = true

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/chat/chat-image/images/" + fx.messageID + "/0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := readChatPerformanceEvents(t)
	if len(events) != 1 || events[0].Kind != "image" {
		t.Fatalf("events = %+v", events)
	}
	data := events[0].Data
	if data["session"] != "chat-image" || data["message"] != fx.messageID || data["cache_hit"] != false {
		t.Errorf("image event = %+v", data)
	}
	for _, key := range []string{"lookup_ms", "cache_check_ms", "serve_ms", "log_scan_ms", "generate_ms"} {
		if _, ok := data[key]; !ok {
			t.Errorf("%s missing: %+v", key, data)
		}
	}
}
