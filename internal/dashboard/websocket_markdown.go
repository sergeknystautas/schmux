package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/mdedit"
)

// markdownWSReadLimit fits a base and a draft of MaxDocumentBytes each plus
// JSON framing and escaping. The shared wsReadLimit stays at 64 KB.
const markdownWSReadLimit = 4 * 1024 * 1024

// markdownWSWriteTimeout bounds every write the document makes to a socket,
// in nanoseconds. The document writes while holding its mutex, so a peer that
// has stopped reading would otherwise stall every other subscriber, the
// watcher, and server shutdown. Atomic so tests can shorten it while handler
// goroutines on hijacked connections are still running.
var markdownWSWriteTimeout atomic.Int64

func init() { markdownWSWriteTimeout.Store(int64(10 * time.Second)) }

func markdownWriteTimeout() time.Duration { return time.Duration(markdownWSWriteTimeout.Load()) }

// markdownConn adapts wsConn to mdedit.Conn. The document is the only writer.
type markdownConn struct{ conn *wsConn }

func (c *markdownConn) Send(msg contracts.MarkdownDocument) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMessageTimeout(websocket.TextMessage, data, markdownWriteTimeout())
}

func (c *markdownConn) CloseWithReason(reason mdedit.CloseReason) {
	frame := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, string(reason))
	_ = c.conn.WriteMessageTimeout(websocket.CloseMessage, frame, markdownWriteTimeout())
	_ = c.conn.Close()
}

// handleMarkdownWebSocket serves /ws/markdown/{workspaceId}/{url-encoded path}.
// Authentication and every path rule run before upgrade; afterwards the
// document owns the socket and this loop only feeds it save frames.
func (s *Server) handleMarkdownWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.requiresAuth() {
		if s.authEnabled() || !s.isTrustedRequest(r) {
			if _, err := s.authenticateRequest(r); err != nil {
				writeJSONError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}

	workspaceID, filePath, errMsg := splitWorkspaceFileParam(chi.URLParam(r, "*"))
	if errMsg != "" {
		writeJSONError(w, errMsg, http.StatusBadRequest)
		return
	}
	// The shared validator covers containment, casing, symlinks, regular-file,
	// local-only, and VCS-ignore.
	if verr := validateWorkspaceFileTarget(r.Context(), s.state, workspaceID, filePath); verr != nil {
		writeJSONError(w, verr.message, verr.status)
		return
	}
	if !isMarkdownPath(filePath) {
		writeJSONError(w, "file type not allowed", http.StatusForbidden)
		return
	}
	ws, _ := s.state.GetWorkspace(workspaceID)

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 64 * 1024,
		CheckOrigin:     s.checkWSOrigin,
	}
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	rawConn.SetReadLimit(markdownWSReadLimit)
	conn := &wsConn{conn: rawConn}
	mc := &markdownConn{conn: conn}
	defer conn.Close()

	absPath := filepath.Join(ws.Path, filePath)
	// Re-run on every save, from any tab. The hub keeps the first subscriber's
	// closure for the document's whole life, so it must not capture this
	// request's context: that is cancelled when this handler returns, while
	// other tabs keep saving. The server's lifetime context is the right bound;
	// the validator adds its own 5 s timeout for the ignore check.
	validate := func() error {
		if verr := validateWorkspaceFileTarget(s.shutdownCtx, s.state, workspaceID, filePath); verr != nil {
			return errors.New(verr.message)
		}
		return nil
	}
	doc, err := s.markdownHub.Subscribe(absPath, validate, mc)
	if err != nil {
		reason := mdedit.ReasonInvalidPath
		if ce, ok := err.(*mdedit.CloseError); ok {
			reason = ce.Reason
		}
		mc.CloseWithReason(reason)
		return
	}
	defer s.markdownHub.Unsubscribe(absPath, mc)

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.TextMessage {
			doc.Reject(mc, mdedit.ReasonBadRequest)
			return
		}
		var save contracts.MarkdownSave
		if err := json.Unmarshal(data, &save); err != nil || save.Type != "save" {
			doc.Reject(mc, mdedit.ReasonBadRequest)
			return
		}
		doc.Save(mc, save)
	}
}
