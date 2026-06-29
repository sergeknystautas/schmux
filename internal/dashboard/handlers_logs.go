package dashboard

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/spawnlog"
)

// handleLogsWebSocket streams a registered log source (e.g. spawn) to the Logs
// page: existing contents as backlog, then each appended line live.
func (s *Server) handleLogsWebSocket(w http.ResponseWriter, r *http.Request) {
	source := chi.URLParam(r, "source")
	path, ok := spawnlog.SourcePath(source)
	if !ok {
		http.Error(w, "unknown log source", http.StatusNotFound)
		return
	}
	s.streamLogFile(w, r, path)
}

// handleFenceLogWebSocket streams one fenced session's Fence monitor.log. The
// session id becomes a directory name, so it is validated against the session
// manager (must exist and be fenced) before any path is built — this both
// scopes access to real fenced sessions and prevents path traversal.
func (s *Server) handleFenceLogWebSocket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.session.GetSession(id)
	if err != nil || !sess.Fence {
		http.Error(w, "unknown fenced session", http.StatusNotFound)
		return
	}
	s.streamLogFile(w, r, filepath.Join(schmuxdir.FenceLaunchDir(id), "monitor.log"))
}

// streamLogFile upgrades to a websocket and sends path's existing lines as
// backlog (one text message per line), then live-tails appended lines until the
// client disconnects. Read-only; the tailer stops on disconnect.
func (s *Server) streamLogFile(w http.ResponseWriter, r *http.Request, path string) {
	rawConn, err := s.upgradeWebSocket(w, r, 1024, 64*1024)
	if err != nil {
		return
	}
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	// Backlog: send existing lines, remembering the offset we reached so the
	// tailer resumes exactly there — no gaps, no duplicates.
	var offset int64
	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
				f.Close()
				return
			}
		}
		offset, _ = f.Seek(0, io.SeekCurrent)
		f.Close()
	}

	// Live tail. wsConn serializes concurrent writes with its mutex.
	tailer, err := spawnlog.NewTailer(path, offset, func(line []byte) {
		_ = conn.WriteMessage(websocket.TextMessage, line)
	})
	if err != nil {
		return
	}
	defer tailer.Stop()

	// Block until the client disconnects (read loop returns on close/error).
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
