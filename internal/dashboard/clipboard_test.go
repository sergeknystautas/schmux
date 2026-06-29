package dashboard

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleClipboardPaste_MethodNotAllowed(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/clipboard-paste", nil)
	rr := httptest.NewRecorder()
	server.handleClipboardPaste(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestHandleClipboardPaste_InvalidJSON(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/clipboard-paste", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()
	server.handleClipboardPaste(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleClipboardPaste_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		body clipboardPasteRequest
	}{
		{"missing sessionId", clipboardPasteRequest{SessionID: "", ImageB64: base64.StdEncoding.EncodeToString([]byte("png"))}},
		{"missing imageBase64", clipboardPasteRequest{SessionID: "sess-123", ImageB64: ""}},
		{"both empty", clipboardPasteRequest{SessionID: "", ImageB64: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, _ := newTestServer(t)

			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/clipboard-paste", bytes.NewReader(body))
			rr := httptest.NewRecorder()
			server.handleClipboardPaste(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", rr.Code)
			}
		})
	}
}

func TestHandleClipboardPaste_InvalidBase64(t *testing.T) {
	server, _, _ := newTestServer(t)

	body, _ := json.Marshal(clipboardPasteRequest{
		SessionID: "sess-12345678",
		ImageB64:  "not-valid-base64!!!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/clipboard-paste", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleClipboardPaste(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleClipboardPaste_SessionNotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	// Mock clipboard so we get past that step
	origFunc := setClipboardImageFunc
	setClipboardImageFunc = func(string) error { return nil }
	t.Cleanup(func() { setClipboardImageFunc = origFunc })

	body, _ := json.Marshal(clipboardPasteRequest{
		SessionID: "nonexistent-session",
		ImageB64:  base64.StdEncoding.EncodeToString([]byte("fake-png-data")),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/clipboard-paste", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleClipboardPaste(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleClipboardPaste_ClipboardWriteCalled(t *testing.T) {
	server, _, st := newTestServer(t)

	var clipboardPath string
	origFunc := setClipboardImageFunc
	setClipboardImageFunc = func(path string) error {
		clipboardPath = path
		return nil
	}
	t.Cleanup(func() { setClipboardImageFunc = origFunc })

	// Add a session to state so lookup succeeds.
	// The handler will then try to get a tracker (which won't exist for a
	// test session), returning 404 — but we can verify clipboard was called.
	sess := state.Session{
		ID:          "sess-12345678-abcd",
		WorkspaceID: "ws-1",
		Target:      "test",
		TmuxSession: "test-tmux",
	}
	st.AddSession(sess)

	body, _ := json.Marshal(clipboardPasteRequest{
		SessionID: "sess-12345678-abcd",
		ImageB64:  base64.StdEncoding.EncodeToString([]byte("fake-png-data")),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/clipboard-paste", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleClipboardPaste(rr, req)

	// Clipboard should have been called with a temp file path
	if clipboardPath == "" {
		t.Fatal("setClipboardImageFunc was not called")
	}

	// GetTracker succeeds (creates a tracker), but SendInput fails because
	// there's no real tmux session — returns 500 "failed to send input".
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (SendInput fails without tmux), got %d", rr.Code)
	}
}

func TestHandleClipboardPaste_FencedTypesFilePath(t *testing.T) {
	server, _, st := newTestServer(t)

	// Fenced sessions deny clipboard access inside the sandbox (mach-lookup
	// com.apple.pasteboard.1), so the handler must NOT touch the macOS
	// clipboard — it writes the image to a fence-readable file and types the
	// path into the agent instead, mirroring the remote file fallback.
	clipboardCalled := false
	origFunc := setClipboardImageFunc
	setClipboardImageFunc = func(string) error {
		clipboardCalled = true
		return nil
	}
	t.Cleanup(func() { setClipboardImageFunc = origFunc })

	st.AddSession(state.Session{
		ID:          "fenced-sess-1234",
		WorkspaceID: "ws-1",
		Target:      "test",
		TmuxSession: "test-tmux",
		Fence:       true,
	})

	imageData := []byte("fake-png-data")
	body, _ := json.Marshal(clipboardPasteRequest{
		SessionID: "fenced-sess-1234",
		ImageB64:  base64.StdEncoding.EncodeToString(imageData),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/clipboard-paste", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleClipboardPaste(rr, req)

	if clipboardCalled {
		t.Fatal("fenced session must not write to the macOS clipboard")
	}
	// Typing the path is best-effort (SendInput fails without a real tmux
	// session), but the file is written and the path returned regardless.
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["method"] != "file" {
		t.Fatalf("expected method=file, got %q", resp["method"])
	}
	if resp["file_path"] == "" {
		t.Fatal("expected file_path in response")
	}
	t.Cleanup(func() { os.Remove(resp["file_path"]) })

	got, err := os.ReadFile(resp["file_path"])
	if err != nil {
		t.Fatalf("reading written image: %v", err)
	}
	if !bytes.Equal(got, imageData) {
		t.Fatalf("written image = %q, want %q", got, imageData)
	}
}

func TestHandleClipboardPaste_ClipboardError(t *testing.T) {
	server, _, st := newTestServer(t)

	// Add a local session so the lookup succeeds and reaches the clipboard code
	st.AddSession(state.Session{
		ID:          "sess-12345678",
		WorkspaceID: "ws-1",
		Target:      "test",
		TmuxSession: "test-tmux",
	})

	origFunc := setClipboardImageFunc
	setClipboardImageFunc = func(string) error {
		return fmt.Errorf("osascript not found")
	}
	t.Cleanup(func() { setClipboardImageFunc = origFunc })

	body, _ := json.Marshal(clipboardPasteRequest{
		SessionID: "sess-12345678",
		ImageB64:  base64.StdEncoding.EncodeToString([]byte("fake-png-data")),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/clipboard-paste", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleClipboardPaste(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Fatal("expected error message in response")
	}
}
