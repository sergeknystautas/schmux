package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/session"
)

// clearNudgeOnInput checks if the input data contains interactive characters
// (Enter, Tab, backtab, or bare Escape) and clears any pending nudge for the
// session. Save and broadcast are performed asynchronously.
func (s *Server) clearNudgeOnInput(sessionID, data string) {
	if strings.Contains(data, "\r") || strings.Contains(data, "\t") || strings.Contains(data, "\x1b[Z") || data == "\x1b" {
		if s.state.ClearSessionNudge(sessionID) {
			go func() {
				if err := s.state.Save(); err != nil {
					logging.Sub(s.logger, "nudgenik").Error("failed to save nudge clear", "err", err)
				} else {
					s.BroadcastSessions()
				}
			}()
		}
	}
}

// upgradeWebSocket upgrades an HTTP connection to a WebSocket connection with
// the given buffer sizes and the server's origin check. Sets a 64KB read limit.
func (s *Server) upgradeWebSocket(w http.ResponseWriter, r *http.Request, readBufSize, writeBufSize int) (*websocket.Conn, error) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  readBufSize,
		WriteBufferSize: writeBufSize,
		CheckOrigin:     s.checkWSOrigin,
	}
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}
	rawConn.SetReadLimit(64 * 1024)
	return rawConn, nil
}

// startWSMessageReader starts a goroutine that reads messages from the
// WebSocket connection and sends them on the returned channel. Binary messages
// are routed directly as input (avoiding JSON overhead on the hot path).
// Text messages are JSON-unmarshalled into WSMessage structs for control
// messages (resize, gap, etc.). The channel is closed when the connection
// errors or is closed.
func startWSMessageReader(conn wsReader) chan WSMessage {
	controlChan := make(chan WSMessage, 10)
	go func() {
		defer close(controlChan)
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			switch msgType {
			case websocket.BinaryMessage:
				controlChan <- WSMessage{Type: "input", Data: string(msg)}
			case websocket.TextMessage:
				var wsMsg WSMessage
				if err := json.Unmarshal(msg, &wsMsg); err == nil {
					controlChan <- wsMsg
				}
			}
		}
	}()
	return controlChan
}

// wsReader is the interface needed by startWSMessageReader. Both
// *websocket.Conn and *wsConn satisfy it.
type wsReader interface {
	ReadMessage() (messageType int, p []byte, err error)
}

// waitForTrackerAttach waits up to the given timeout for the session
// tracker to attach to control mode, checking every 25ms. If ctx is
// cancelled before the tracker attaches, it returns early.
func waitForTrackerAttach(ctx context.Context, tracker *session.SessionRuntime, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for !tracker.IsAttached() && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
