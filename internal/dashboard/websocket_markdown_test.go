package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

func (s *Server) markdownTestRouter() http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/ws/markdown/*", s.handleMarkdownWebSocket)
	return r
}

// newMarkdownWorkspace creates a git repo with notes.md and a gitignored file.
func newMarkdownWorkspace(t *testing.T, st *state.State) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ws-md")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	files := map[string]string{
		"notes.md":      "# notes\n",
		"docs/deep.mdx": "deep\n",
		"secret.md":     "ignored\n",
		".gitignore":    "secret.md\n",
		"code.go":       "package x\n",
		"with space.md": "space\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "notes.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspace(state.Workspace{ID: "ws-md", Path: dir, VCS: "git"}); err != nil {
		t.Fatal(err)
	}
	return "ws-md", dir
}

func dialMarkdown(t *testing.T, server *Server, path string) (*websocket.Conn, *http.Response, func(), error) {
	t.Helper()
	ts := httptest.NewServer(server.markdownTestRouter())
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+path, nil)
	return conn, resp, func() {
		if conn != nil {
			conn.Close()
		}
		ts.Close()
	}, err
}

func readDocument(t *testing.T, conn *websocket.Conn) contracts.MarkdownDocument {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // deadline backstop
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg contracts.MarkdownDocument
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return msg
}

func TestMarkdownWS_RejectsBeforeUpgrade(t *testing.T) {
	server, _, st := newTestServer(t)
	newMarkdownWorkspace(t, st)
	tests := []struct {
		name string
		path string
		code int
	}{
		{"unknown workspace", "/ws/markdown/nope/notes.md", http.StatusNotFound},
		{"traversal", "/ws/markdown/ws-md/..%2F..%2Fetc%2Fpasswd", http.StatusBadRequest},
		{"symlink", "/ws/markdown/ws-md/link.md", http.StatusForbidden},
		{"not markdown", "/ws/markdown/ws-md/code.go", http.StatusForbidden},
		{"ignored", "/ws/markdown/ws-md/secret.md", http.StatusForbidden},
		{"directory", "/ws/markdown/ws-md/docs", http.StatusForbidden},
		{"missing", "/ws/markdown/ws-md/none.md", http.StatusNotFound},
		{"wrong case", "/ws/markdown/ws-md/NOTES.md", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp, done, err := dialMarkdown(t, server, tt.path)
			defer done()
			if err == nil {
				t.Fatal("expected upgrade to be refused")
			}
			if resp == nil || resp.StatusCode != tt.code {
				t.Fatalf("status = %v, want %d", resp, tt.code)
			}
		})
	}
}

func TestMarkdownWS_RejectsRemoteWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-remote", Path: t.TempDir(), RemoteHostID: "host-1"}); err != nil {
		t.Fatal(err)
	}
	_, resp, done, err := dialMarkdown(t, server, "/ws/markdown/ws-remote/notes.md")
	defer done()
	if err == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for remote workspace, got err=%v resp=%v", err, resp)
	}
}

func TestMarkdownWS_SnapshotSaveAndPercentPath(t *testing.T) {
	server, _, st := newTestServer(t)
	_, dir := newMarkdownWorkspace(t, st)
	conn, _, done, err := dialMarkdown(t, server, "/ws/markdown/ws-md/with%20space.md")
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	first := readDocument(t, conn)
	if first.Content != "space\n" || first.Reply != "" {
		t.Fatalf("first = %+v", first)
	}
	save := contracts.MarkdownSave{Type: "save", ID: "s1", Base: "space\n", Draft: "space edited\n"}
	if err := conn.WriteJSON(save); err != nil {
		t.Fatal(err)
	}
	reply := readDocument(t, conn)
	if reply.Reply != "s1" || reply.Content != "space edited\n" {
		t.Fatalf("reply = %+v", reply)
	}
	got, err := os.ReadFile(filepath.Join(dir, "with space.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "space edited\n" {
		t.Fatalf("disk = %q", got)
	}
}

func TestMarkdownWS_BadFrameClosesWithReason(t *testing.T) {
	server, _, st := newTestServer(t)
	newMarkdownWorkspace(t, st)
	conn, _, done, err := dialMarkdown(t, server, "/ws/markdown/ws-md/notes.md")
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	readDocument(t, conn)
	if err := conn.WriteMessage(websocket.TextMessage, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = conn.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Text != "bad_request" {
		t.Fatalf("expected close with bad_request, got %v", err)
	}
}

func TestMarkdownWS_ReadLimitIsDedicated(t *testing.T) {
	if wsReadLimit != 64*1024 {
		t.Fatalf("shared wsReadLimit changed to %d", wsReadLimit)
	}
	if markdownWSReadLimit != 4*1024*1024 {
		t.Fatalf("markdownWSReadLimit = %d", markdownWSReadLimit)
	}
}

func TestMarkdownWS_RequiresAuthWhenEnabled(t *testing.T) {
	server, cfg, st := newTestServer(t)
	newMarkdownWorkspace(t, st)
	cfg.AccessControl = &config.AccessControlConfig{Enabled: true}
	_, resp, done, err := dialMarkdown(t, server, "/ws/markdown/ws-md/notes.md")
	defer done()
	if err == nil {
		t.Fatal("expected upgrade to be refused without credentials")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestMarkdownWS_RejectsBadOrigin(t *testing.T) {
	server, _, st := newTestServer(t)
	newMarkdownWorkspace(t, st)
	ts := httptest.NewServer(server.markdownTestRouter())
	defer ts.Close()
	header := http.Header{"Origin": {"http://evil.com"}}
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/markdown/ws-md/notes.md", header)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected upgrade to be refused for a foreign origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want 403", resp)
	}
}

// A subscriber that stops reading must not stall the document: every send to
// it is bounded by markdownWSWriteTimeout, after which it is dropped and the
// other subscribers keep getting their replies.
func TestMarkdownWS_StalledSubscriberDoesNotBlockOthers(t *testing.T) {
	server, _, st := newTestServer(t)
	newMarkdownWorkspace(t, st)
	prev := markdownWSWriteTimeout.Load()
	markdownWSWriteTimeout.Store(int64(200 * time.Millisecond))
	t.Cleanup(func() { markdownWSWriteTimeout.Store(prev) })

	stalled, _, doneA, err := dialMarkdown(t, server, "/ws/markdown/ws-md/notes.md")
	defer doneA()
	if err != nil {
		t.Fatal(err)
	}
	readDocument(t, stalled) // initial document; never read again

	active, _, doneB, err := dialMarkdown(t, server, "/ws/markdown/ws-md/notes.md")
	defer doneB()
	if err != nil {
		t.Fatal(err)
	}
	base := readDocument(t, active).Content

	// Enough pushed bytes to exhaust the stalled peer's socket buffers.
	filler := strings.Repeat("x", 512*1024)
	for i := 0; i < 16; i++ {
		draft := fmt.Sprintf("# rev %d\n%s\n", i, filler)
		if err := active.WriteJSON(contracts.MarkdownSave{Type: "save", ID: fmt.Sprintf("s%d", i), Base: base, Draft: draft}); err != nil {
			t.Fatal(err)
		}
		reply := readDocument(t, active) // 5 s deadline inside; fails if the document is stuck
		if reply.Reply != fmt.Sprintf("s%d", i) {
			t.Fatalf("reply %d = %q", i, reply.Reply)
		}
		base = reply.Content
	}
}

// The document outlives whichever tab opened it first. A save from a later
// tab must still validate after the first tab's handler has returned.
func TestMarkdownWS_SaveAfterFirstSubscriberLeaves(t *testing.T) {
	server, _, st := newTestServer(t)
	newMarkdownWorkspace(t, st)

	first, _, doneFirst, err := dialMarkdown(t, server, "/ws/markdown/ws-md/notes.md")
	if err != nil {
		t.Fatal(err)
	}
	readDocument(t, first)

	second, _, doneSecond, err := dialMarkdown(t, server, "/ws/markdown/ws-md/notes.md")
	defer doneSecond()
	if err != nil {
		t.Fatal(err)
	}
	base := readDocument(t, second).Content

	// Close the first tab. Its handler returns when it sees the close, which
	// cancels that request's context. No client-visible signal marks that
	// moment, so this test cannot await it: against the regression (validate
	// bound to the first request's context) it fails only when the handler
	// has already returned by the time the save validates. With validate
	// bound to the server's lifetime it passes regardless of that ordering.
	// TestMarkdownWS_StalledSubscriberDoesNotBlockOthers exercises the same
	// path with the document itself dropping the first subscriber.
	doneFirst()
	if err := second.WriteJSON(contracts.MarkdownSave{Type: "save", ID: "s1", Base: base, Draft: base + "more\n"}); err != nil {
		t.Fatal(err)
	}
	reply := readDocument(t, second)
	if reply.Reply != "s1" {
		t.Fatalf("reply = %+v", reply)
	}
}
