package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newPreviewManager returns a preview.Manager wired to the given state store,
// using a small cap and an ephemeral port block so tests don't collide.
func newPreviewManager(t *testing.T, st *state.State, maxPerWorkspace int) *preview.Manager {
	t.Helper()
	logger := log.NewWithOptions(io.Discard, log.Options{})
	return preview.NewManager(
		st,
		maxPerWorkspace, // maxPerWorkspace
		100,             // maxGlobal — large enough not to matter for individual tests
		false,           // networkAccess
		54000,           // portBase — away from default 53000 to avoid clashes
		10,              // blockSize
		false,           // tlsEnabled
		"",              // tlsCertPath
		"",              // tlsKeyPath
		logger,
		nil, // portDetector — not needed for handler tests
	)
}

// startEchoServer starts a real TCP listener that serves minimal HTTP responses.
// It returns the port number and a cleanup function that closes the listener.
func startEchoServer(t *testing.T) (port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// postPreviewRequest builds a POST request for the previews-create endpoint
// with the given workspaceID injected into the chi route context.
func postPreviewRequest(t *testing.T, workspaceID string, body interface{}) *http.Request {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}
	return makeWorkspaceRequest(t, http.MethodPost,
		"/api/workspaces/"+workspaceID+"/previews",
		workspaceID,
		data,
	)
}

// decodePreviewResponse decodes the JSON preview response from a recorder.
func decodePreviewResponse(t *testing.T, rr *httptest.ResponseRecorder) previewResponse {
	t.Helper()
	var resp previewResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v, raw: %s", err, rr.Body.String())
	}
	return resp
}

// addWorkspaceToServer adds a minimal workspace to server state and returns it.
func addWorkspaceToServer(t *testing.T, st *state.State, id string) state.Workspace {
	t.Helper()
	ws := state.Workspace{
		ID:     id,
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}
	return ws
}

// tcpLookupPortOwner verifies a port is listening via TCP connect instead of
// lsof, avoiding flakiness under heavy parallel test load.
func tcpLookupPortOwner(port int) (int, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return 0, fmt.Errorf("nothing listening on port %d", port)
	}
	conn.Close()
	return os.Getpid(), nil
}

// TestHandlePreviewsCreate covers scenarios 12-23 from the spec.
func TestHandlePreviewsCreate(t *testing.T) {
	// ── scenario 12: happy path with source_session_id ──────────────────────
	t.Run("happy path with session", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.lookupPortOwner = tcpLookupPortOwner
		port := startEchoServer(t)
		ws := addWorkspaceToServer(t, st, "ws-happy-session")

		// Add a session the handler can look up.
		sess := state.Session{
			ID:          "sess-1",
			WorkspaceID: ws.ID,
			Target:      "claude",
			TmuxSession: "tmux-1",
		}
		st.AddSession(sess)

		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		req := postPreviewRequest(t, ws.ID, createPreviewRequest{
			TargetPort:      port,
			SourceSessionID: "sess-1",
		})
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
		}
		resp := decodePreviewResponse(t, rr)
		if resp.ServerPID <= 0 {
			t.Errorf("expected server_pid > 0, got %d", resp.ServerPID)
		}
		if resp.TargetPort != port {
			t.Errorf("expected target_port %d, got %d", port, resp.TargetPort)
		}
	})

	// ── scenario 13: happy path without source_session_id ───────────────────
	t.Run("happy path without session", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.lookupPortOwner = tcpLookupPortOwner
		port := startEchoServer(t)
		ws := addWorkspaceToServer(t, st, "ws-happy-nosession")

		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		req := postPreviewRequest(t, ws.ID, createPreviewRequest{
			TargetPort: port,
		})
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
		}
		resp := decodePreviewResponse(t, rr)
		if resp.TargetPort != port {
			t.Errorf("expected target_port %d, got %d", port, resp.TargetPort)
		}
	})

	// ── scenario 14: port not listening ──────────────────────────────────────
	t.Run("port not listening", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.lookupPortOwner = tcpLookupPortOwner

		// Pick a free port then immediately release it so nothing is listening.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		unusedPort := ln.Addr().(*net.TCPAddr).Port
		ln.Close()

		ws := addWorkspaceToServer(t, st, "ws-no-listen")
		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		req := postPreviewRequest(t, ws.ID, createPreviewRequest{
			TargetPort: unusedPort,
		})
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// ── scenario 15: non-loopback host ───────────────────────────────────────
	t.Run("non-loopback host", func(t *testing.T) {
		server, _, st := newTestServer(t)
		ws := addWorkspaceToServer(t, st, "ws-nonloopback")
		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		req := postPreviewRequest(t, ws.ID, createPreviewRequest{
			TargetPort: 3000,
			TargetHost: "0.0.0.0",
		})
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// ── scenario 16: invalid port (0) ────────────────────────────────────────
	t.Run("invalid port zero", func(t *testing.T) {
		server, _, st := newTestServer(t)
		ws := addWorkspaceToServer(t, st, "ws-badport")
		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		req := postPreviewRequest(t, ws.ID, createPreviewRequest{
			TargetPort: 0,
		})
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// ── scenario 17: workspace not found ─────────────────────────────────────
	t.Run("workspace not found", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		req := postPreviewRequest(t, "nonexistent-ws", createPreviewRequest{
			TargetPort: 3000,
		})
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// ── scenario 18: dedup — same port twice returns 201 then 200, same ID ──
	t.Run("dedup same port", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.lookupPortOwner = tcpLookupPortOwner
		port := startEchoServer(t)
		ws := addWorkspaceToServer(t, st, "ws-dedup")

		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		// First POST.
		req1 := postPreviewRequest(t, ws.ID, createPreviewRequest{TargetPort: port})
		rr1 := httptest.NewRecorder()
		server.handlePreviewsCreate(rr1, req1)
		if rr1.Code != http.StatusCreated {
			t.Fatalf("first POST: expected 201, got %d: %s", rr1.Code, rr1.Body.String())
		}
		resp1 := decodePreviewResponse(t, rr1)

		// Second POST — same port.
		req2 := postPreviewRequest(t, ws.ID, createPreviewRequest{TargetPort: port})
		rr2 := httptest.NewRecorder()
		server.handlePreviewsCreate(rr2, req2)
		if rr2.Code != http.StatusOK {
			t.Fatalf("second POST: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
		}
		resp2 := decodePreviewResponse(t, rr2)

		if resp1.ID != resp2.ID {
			t.Errorf("expected same preview ID on dedup, got %q vs %q", resp1.ID, resp2.ID)
		}
	})

	// ── scenario 19: dedup with different session — 200, original session preserved
	t.Run("dedup different session", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.lookupPortOwner = tcpLookupPortOwner
		port := startEchoServer(t)
		ws := addWorkspaceToServer(t, st, "ws-dedup-sess")

		// Add two sessions.
		st.AddSession(state.Session{ID: "sess-a", WorkspaceID: ws.ID, Target: "claude", TmuxSession: "tmux-a"})
		st.AddSession(state.Session{ID: "sess-b", WorkspaceID: ws.ID, Target: "claude", TmuxSession: "tmux-b"})

		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		// First POST with sess-a.
		req1 := postPreviewRequest(t, ws.ID, createPreviewRequest{TargetPort: port, SourceSessionID: "sess-a"})
		rr1 := httptest.NewRecorder()
		server.handlePreviewsCreate(rr1, req1)
		if rr1.Code != http.StatusCreated {
			t.Fatalf("first POST: expected 201, got %d: %s", rr1.Code, rr1.Body.String())
		}
		resp1 := decodePreviewResponse(t, rr1)

		// Second POST — same port, different session.
		req2 := postPreviewRequest(t, ws.ID, createPreviewRequest{TargetPort: port, SourceSessionID: "sess-b"})
		rr2 := httptest.NewRecorder()
		server.handlePreviewsCreate(rr2, req2)
		if rr2.Code != http.StatusOK {
			t.Fatalf("second POST: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
		}
		resp2 := decodePreviewResponse(t, rr2)

		if resp1.ID != resp2.ID {
			t.Errorf("expected same preview ID on dedup, got %q vs %q", resp1.ID, resp2.ID)
		}
		// Original session should be preserved.
		if resp2.SourceSessionID != "sess-a" {
			t.Errorf("expected original session sess-a preserved, got %q", resp2.SourceSessionID)
		}
	})

	// ── scenario 20: workspace cap ───────────────────────────────────────────
	t.Run("workspace cap exceeded", func(t *testing.T) {
		const cap = 2
		server, _, st := newTestServer(t)
		server.lookupPortOwner = tcpLookupPortOwner
		ws := addWorkspaceToServer(t, st, "ws-cap")

		server.previewManager = newPreviewManager(t, st, cap)
		t.Cleanup(func() { server.previewManager.Stop() })

		// Fill to cap.
		for i := 0; i < cap; i++ {
			port := startEchoServer(t)
			req := postPreviewRequest(t, ws.ID, createPreviewRequest{TargetPort: port})
			rr := httptest.NewRecorder()
			server.handlePreviewsCreate(rr, req)
			if rr.Code != http.StatusCreated {
				t.Fatalf("fill iteration %d: expected 201, got %d: %s", i, rr.Code, rr.Body.String())
			}
		}

		// One more should be rejected.
		port := startEchoServer(t)
		req := postPreviewRequest(t, ws.ID, createPreviewRequest{TargetPort: port})
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusConflict {
			t.Fatalf("expected 409 on cap exceeded, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// ── scenario 21: malformed body ──────────────────────────────────────────
	t.Run("malformed body", func(t *testing.T) {
		server, _, st := newTestServer(t)
		ws := addWorkspaceToServer(t, st, "ws-badbody")
		server.previewManager = newPreviewManager(t, st, 3)
		t.Cleanup(func() { server.previewManager.Stop() })

		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/previews",
			ws.ID,
			[]byte("{garbage json"),
		)
		rr := httptest.NewRecorder()
		server.handlePreviewsCreate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
