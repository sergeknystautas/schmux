package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/spawnlog"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleLogsWebSocket_UnknownSource(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/bogus", nil)
	rr := httptest.NewRecorder()
	s.logsTestRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestHandleFenceLogWebSocket_Unknown404(t *testing.T) {
	server, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/fence/nope", nil)
	rr := httptest.NewRecorder()
	server.logsTestRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404", rr.Code)
	}
}

func TestHandleFenceLogWebSocket_NotFenced404(t *testing.T) {
	server, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "plain", Target: "command", CreatedAt: time.Now(), Fence: false})
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/fence/plain", nil)
	rr := httptest.NewRecorder()
	server.logsTestRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("non-fenced session: status = %d, want 404", rr.Code)
	}
}

func TestHandleFenceLogWebSocket_FencedNot404(t *testing.T) {
	server, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "fenced", Target: "command", CreatedAt: time.Now(), Fence: true})
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/fence/fenced", nil)
	rr := httptest.NewRecorder()
	server.logsTestRouter().ServeHTTP(rr, req)
	// A non-websocket request can't be upgraded (recorder has no Hijack), so the
	// handler returns without a status — what matters is validation passed: not 404.
	if rr.Code == http.StatusNotFound {
		t.Errorf("fenced session should pass validation, got 404")
	}
}

// logsTestRouter stands up just the logs routes so chi.URLParam works.
func (s *Server) logsTestRouter() http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/ws/logs/{source}", s.handleLogsWebSocket)
	r.HandleFunc("/ws/logs/fence/{id}", s.handleFenceLogWebSocket)
	return r
}

// dialTestLogsWS opens a real WebSocket against the logs routes. The handler
// resolves the path parameter; this helper stays agnostic.
func dialTestLogsWS(t *testing.T, server *Server, path string) (*websocket.Conn, func()) {
	t.Helper()
	ts := httptest.NewServer(server.logsTestRouter())
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + path
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}
	return conn, func() {
		conn.Close()
		ts.Close()
	}
}

type testLogMessage struct {
	Type    string `json:"type"`
	Line    string `json:"line"`
	HasMore bool   `json:"has_more"`
	Message string `json:"message"`
}

type testLogPage struct {
	lines   []string
	hasMore bool
}

// readLogPage reads all `history` messages until the next `history_end`,
// returning the page contents and whether more history remains.
func readLogPage(t *testing.T, conn *websocket.Conn) testLogPage {
	t.Helper()
	var page testLogPage
	for {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read page: %v", err)
		}
		var msg testLogMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal page msg: %v", err)
		}
		switch msg.Type {
		case "history":
			page.lines = append(page.lines, msg.Line)
		case "history_end":
			page.hasMore = msg.HasMore
			return page
		default:
			t.Fatalf("unexpected message type %q during page read", msg.Type)
		}
	}
}

func readTestLogMessage(t *testing.T, conn *websocket.Conn) testLogMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	var msg testLogMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	return msg
}

// writeNumberedLines writes `n` newline-terminated lines "line-000" .. "line-(n-1)".
func writeNumberedLines(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "line-%03d\n", i)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

// schmuxDirForTest returns the schmuxdir the test server set up.
func schmuxDirForTest(t *testing.T) string {
	t.Helper()
	return schmuxdir.Get()
}

func assertStrings(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d strings, want %d (%q)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("entry %d = %q, want %q", i, got[i], w)
		}
	}
}

func TestLogsWebSocket_PagesNewestFirstThenTails(t *testing.T) {
	server, _, _ := newTestServer(t)
	path, _ := spawnlog.SourcePath("spawn")
	writeNumberedLines(t, path, 105) // line-000 through line-104

	conn, cleanup := dialTestLogsWS(t, server, "/ws/logs/spawn")
	defer cleanup()

	first := readLogPage(t, conn)
	if len(first.lines) != 100 || first.lines[0] != "line-104" || first.lines[99] != "line-005" {
		t.Fatalf("unexpected first page: %#v", first.lines)
	}
	if !first.hasMore {
		t.Fatal("first page should have older records")
	}

	if err := conn.WriteJSON(map[string]string{"type": "load_older"}); err != nil {
		t.Fatal(err)
	}
	second := readLogPage(t, conn)
	assertStrings(t, second.lines, "line-004", "line-003", "line-002", "line-001", "line-000")
	if second.hasMore {
		t.Fatal("second page should exhaust history")
	}

	appendLine(t, path, "line-105")
	msg := readTestLogMessage(t, conn)
	if msg.Type != "append" || msg.Line != "line-105" {
		t.Fatalf("append message = %#v", msg)
	}
}

func TestLogsWebSocket_EmptyFileReturnsTerminalHistoryEnd(t *testing.T) {
	server, _, _ := newTestServer(t)
	path, _ := spawnlog.SourcePath("spawn")
	// Ensure parent dir exists, then create an empty file.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	conn, cleanup := dialTestLogsWS(t, server, "/ws/logs/spawn")
	defer cleanup()

	page := readLogPage(t, conn)
	if len(page.lines) != 0 {
		t.Fatalf("empty file produced history messages: %q", page.lines)
	}
	if page.hasMore {
		t.Fatal("empty file should report has_more:false")
	}

	appendLine(t, path, "first\n")
	msg := readTestLogMessage(t, conn)
	if msg.Type != "append" || msg.Line != "first" {
		t.Fatalf("append = %#v", msg)
	}
}

func TestLogsWebSocket_LoadOlderAfterExhaustionIsNoOp(t *testing.T) {
	server, _, _ := newTestServer(t)
	path, _ := spawnlog.SourcePath("spawn")
	writeNumberedLines(t, path, 3)

	conn, cleanup := dialTestLogsWS(t, server, "/ws/logs/spawn")
	defer cleanup()

	page := readLogPage(t, conn)
	if len(page.lines) != 3 {
		t.Fatalf("unexpected lines: %q", page.lines)
	}
	if page.hasMore {
		t.Fatal("should exhaust history")
	}

	if err := conn.WriteJSON(map[string]string{"type": "load_older"}); err != nil {
		t.Fatal(err)
	}
	second := readLogPage(t, conn)
	if len(second.lines) != 0 {
		t.Fatalf("post-exhaustion page should be empty: %q", second.lines)
	}
	if second.hasMore {
		t.Fatal("post-exhaustion has_more should be false")
	}
}

func TestLogsWebSocket_HistoryErrorOnReadFailure(t *testing.T) {
	server, _, _ := newTestServer(t)
	path, _ := spawnlog.SourcePath("spawn")
	writeNumberedLines(t, path, 101)

	conn, cleanup := dialTestLogsWS(t, server, "/ws/logs/spawn")
	defer cleanup()

	first := readLogPage(t, conn)
	if len(first.lines) != 100 {
		t.Fatalf("first page size = %d, want 100", len(first.lines))
	}

	// Replace the file with a directory to break the next read.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := conn.WriteJSON(map[string]string{"type": "load_older"}); err != nil {
		t.Fatal(err)
	}
	errMsg := readTestLogMessage(t, conn)
	if errMsg.Type != "history_error" {
		t.Fatalf("expected history_error, got %#v", errMsg)
	}

	// Restore and Retry — the boundary was unchanged so the same single
	// remaining line should arrive followed by a terminal history_end.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("line-000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := conn.WriteJSON(map[string]string{"type": "load_older"}); err != nil {
		t.Fatal(err)
	}
	retry := readLogPage(t, conn)
	if len(retry.lines) != 1 || retry.lines[0] != "line-000" {
		t.Fatalf("retry page = %#v", retry.lines)
	}
	if retry.hasMore {
		t.Fatal("retry should exhaust history")
	}
}

func TestLogsWebSocket_StreamSetupFailureCanRetryOnSameConnection(t *testing.T) {
	server, _, _ := newTestServer(t)
	path, _ := spawnlog.SourcePath("spawn")
	logsDir := filepath.Dir(path)
	if err := os.RemoveAll(logsDir); err != nil {
		t.Fatal(err)
	}
	// A regular file where the logs directory belongs makes logstream.New fail
	// before it owns any offsets.
	if err := os.WriteFile(logsDir, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	conn, cleanup := dialTestLogsWS(t, server, "/ws/logs/spawn")
	defer cleanup()
	msg := readTestLogMessage(t, conn)
	if msg.Type != "history_error" {
		t.Fatalf("setup failure message = %#v", msg)
	}

	if err := os.Remove(logsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("recovered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "load_older"}); err != nil {
		t.Fatal(err)
	}
	retry := readLogPage(t, conn)
	assertStrings(t, retry.lines, "recovered")
	if retry.hasMore {
		t.Fatal("recovered page should exhaust history")
	}
}

func TestFenceLogsWebSocket_StreamsNewestFirstThroughProtocol(t *testing.T) {
	server, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "fenced", Target: "command", WorkspaceID: "ws-1", CreatedAt: time.Now(), Fence: true})

	// FenceLaunchDir lives under schmuxdir, not the workspace path.
	monitorDir := filepath.Join(schmuxDirForTest(t), "fence", "ws-1", "fenced")
	if err := os.MkdirAll(monitorDir, 0o700); err != nil {
		t.Fatal(err)
	}
	monitorLog := filepath.Join(monitorDir, "monitor.log")
	writeNumberedLines(t, monitorLog, 3) // line-000..line-002

	conn, cleanup := dialTestLogsWS(t, server, "/ws/logs/fence/fenced")
	defer cleanup()

	page := readLogPage(t, conn)
	assertStrings(t, page.lines, "line-002", "line-001", "line-000")
	if page.hasMore {
		t.Fatal("expected terminal history_end")
	}
}
