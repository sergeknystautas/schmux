package dashboard

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/logstream"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/spawnlog"
)

const logsPageSize = 100

// Server -> client envelope. Each connection begins with up to `pageSize`
// history messages (newest-first), then one history_end. Requested older pages
// use the same framing.
type logServerMessage struct {
	Type    string `json:"type"`
	Line    string `json:"line,omitempty"`
	HasMore *bool  `json:"has_more,omitempty"`
	Message string `json:"message,omitempty"`
}

// Client -> server command envelope.
type logClientMessage struct {
	Type string `json:"type"`
}

func writeLogMessage(conn *wsConn, msg logServerMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// writeLogPage sends each `page.Lines` entry as a history message (newest-first),
// then a history_end with the HasMore flag. The HasMore pointer is required so
// `history_end` always carries `has_more` even when false.
func writeLogPage(conn *wsConn, page logstream.Page) error {
	for _, line := range page.Lines {
		if err := writeLogMessage(conn, logServerMessage{Type: "history", Line: string(line)}); err != nil {
			return err
		}
	}
	hasMore := page.HasMore
	return writeLogMessage(conn, logServerMessage{Type: "history_end", HasMore: &hasMore})
}

// handleLogsWebSocket streams a registered log source (e.g. spawn) to the Logs
// page: an initial page of newest history, then live appends.
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
	s.streamLogFile(w, r, filepath.Join(schmuxdir.FenceLaunchDir(sess.WorkspaceID, id), "monitor.log"))
}

// streamLogFile upgrades to a WebSocket and translates logstream events into
// the typed paged protocol: an initial page of newest history, then live
// appends. Client `load_older` commands request the next page. History reads
// are serialized in the read loop so Retry can reuse the unchanged server
// boundary.
func (s *Server) streamLogFile(w http.ResponseWriter, r *http.Request, path string) {
	rawConn, err := s.upgradeWebSocket(w, r, 1024, 64*1024)
	if err != nil {
		return
	}
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	var stream *logstream.Stream
	defer func() {
		if stream != nil {
			stream.Close()
		}
	}()

	sendOlder := func() {
		if stream == nil {
			return
		}
		page, err := stream.LoadOlder()
		if err != nil {
			_ = writeLogMessage(conn, logServerMessage{Type: "history_error", Message: err.Error()})
			return
		}
		if err := writeLogPage(conn, page); err != nil {
			_ = conn.Close()
		}
	}

	startStream := func() {
		next, err := logstream.New(
			path,
			logsPageSize,
			func(line []byte) {
				if err := writeLogMessage(conn, logServerMessage{Type: "append", Line: string(line)}); err != nil {
					_ = conn.Close()
				}
			},
			func(error) {
				// Live watcher failure — disconnect so the client sees Disconnected.
				_ = conn.Close()
			},
		)
		if err != nil {
			// No Stream exists yet, so no offsets have been claimed. Keep the
			// socket open and let load_older retry construction after the
			// underlying filesystem problem is fixed.
			if writeErr := writeLogMessage(conn, logServerMessage{Type: "history_error", Message: err.Error()}); writeErr != nil {
				_ = conn.Close()
			}
			return
		}
		stream = next
		sendOlder()
	}
	startStream()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg logClientMessage
		if json.Unmarshal(data, &msg) == nil && msg.Type == "load_older" {
			if stream == nil {
				startStream()
			} else {
				sendOlder()
			}
		}
	}
}
