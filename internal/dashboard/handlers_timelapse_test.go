//go:build !notimelapse

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	tmpHome := t.TempDir()
	oldDir := schmuxdir.Get()
	schmuxdir.Set(tmpHome)
	t.Cleanup(func() { schmuxdir.Set(oldDir) })

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
