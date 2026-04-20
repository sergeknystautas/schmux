//go:build !notimelapse

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"

	"github.com/go-chi/chi/v5"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/timelapse"
)

func TestHandleTimelapseList_InProgressClearedForDisposedSessions(t *testing.T) {
	server, _, st := newTestServer(t)

	// Point schmuxdir at a temp dir so recordingsDir() resolves there.
	// Always restore to "" (not the resolved path) so subsequent tests in this
	// package that rely on HOME-based fallback resolve fresh, not against the
	// previously resolved path.
	tmpHome := t.TempDir()
	schmuxdir.Set(tmpHome)
	t.Cleanup(func() { schmuxdir.Set("") })

	recDir := filepath.Join(tmpHome, "recordings")
	os.MkdirAll(recDir, 0755)

	// Create a header-only recording (no output events) for a disposed session.
	// The file-based heuristic marks this as InProgress, but the handler
	// should clear it because the session no longer exists.
	headerDisposed := filepath.Join(recDir, "sess-disposed.cast")
	os.WriteFile(headerDisposed, []byte(
		fmt.Sprintf(`{"version":2,"width":80,"height":24,"timestamp":%d}`, time.Now().Unix())+"\n",
	), 0600)

	// Create another header-only recording for a session that still exists.
	headerAlive := filepath.Join(recDir, "sess-alive.cast")
	os.WriteFile(headerAlive, []byte(
		fmt.Sprintf(`{"version":2,"width":80,"height":24,"timestamp":%d}`, time.Now().Unix())+"\n",
	), 0600)

	// Add only sess-alive to state — sess-disposed is gone.
	st.AddWorkspace(state.Workspace{ID: "ws-t1", Repo: "https://example.com/r.git", Branch: "main", Path: t.TempDir()})
	st.AddSession(state.Session{
		ID:          "sess-alive",
		WorkspaceID: "ws-t1",
		Target:      "test",
		TmuxSession: "tmux-alive",
		Status:      "running",
	})

	req, _ := http.NewRequest("GET", "/api/timelapse", nil)
	rr := httptest.NewRecorder()
	server.handleTimelapseList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var recordings []timelapse.RecordingInfo
	if err := json.NewDecoder(rr.Body).Decode(&recordings); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recordings) != 2 {
		t.Fatalf("expected 2 recordings, got %d", len(recordings))
	}

	bySession := make(map[string]timelapse.RecordingInfo)
	for _, r := range recordings {
		bySession[r.SessionID] = r
	}

	// The recording for the disposed session should NOT be in progress.
	disposed := bySession["sess-disposed"]
	if disposed.InProgress {
		t.Error("recording for disposed session should have InProgress=false, got true")
	}

	// The recording for the still-alive session should remain in progress.
	alive := bySession["sess-alive"]
	if !alive.InProgress {
		t.Error("recording for active session should have InProgress=true, got false")
	}
}

func TestTimelapseHandlersRejectInvalidRecordingID(t *testing.T) {
	// chi does NOT URL-decode path params. Both raw and percent-encoded forms
	// reach the handler verbatim, so isValidResourceID rejects:
	//   - raw "../etc/passwd" via the "/" and "." checks
	//   - encoded "..%2Fetc%2Fpasswd" via the "%" failing the strict charset
	//     and via the "." also being rejected.
	// We assert 400 in both cases — the goal is "no path traversal reaches
	// filepath.Join", not specifically which character triggered rejection.
	cases := []struct {
		name string
		id   string // value to embed in URL (after url.PathEscape)
	}{
		{"raw_traversal_dotdot_slash", "../etc/passwd"},
		{"encoded_traversal_dotdot_pct2f", "..%2Fetc%2Fpasswd"},
		{"nul_byte", "foo\x00bar"},
		{"single_dot", "."},
		{"double_dot", ".."},
	}

	r := chi.NewRouter()
	s := &Server{}
	r.Get("/api/timelapse/{recordingId}/download", s.handleTimelapseDownload)
	r.Post("/api/timelapse/{recordingId}/export", s.handleTimelapseExport)
	r.Delete("/api/timelapse/{recordingId}", s.handleTimelapseDelete)

	srv := httptest.NewServer(r)
	defer srv.Close()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			escaped := url.PathEscape(c.id)

			// download (GET)
			resp, err := http.Get(srv.URL + "/api/timelapse/" + escaped + "/download")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("download: got %d, want 400", resp.StatusCode)
			}

			// export (POST)
			resp, err = http.Post(srv.URL+"/api/timelapse/"+escaped+"/export", "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("export: got %d, want 400", resp.StatusCode)
			}

			// delete (DELETE)
			req, _ := http.NewRequest("DELETE", srv.URL+"/api/timelapse/"+escaped, nil)
			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("delete: got %d, want 400", resp.StatusCode)
			}
		})
	}
}
