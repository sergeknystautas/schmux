package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/streamjson"
)

// wsStreamJsonClient wraps a wsConn to implement streamjson.ClientSender.
type wsStreamJsonClient struct {
	conn *wsConn
}

func (c *wsStreamJsonClient) SendJSON(data []byte) error {
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// handleStreamJsonWebSocket handles WebSocket connections for stream-json (HTML mode) sessions.
func (s *Server) handleStreamJsonWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/streamjson/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	if s.config.GetAuthEnabled() {
		if _, err := s.authenticateRequest(r); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Verify session exists and is in HTML mode
	sess, found := s.state.GetSession(sessionID)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if sess.RenderMode != "html" {
		http.Error(w, "session is not in HTML mode", http.StatusBadRequest)
		return
	}

	// Upgrade to WebSocket
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 16384,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return s.isAllowedOrigin(origin)
		},
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("[ws/streamjson] upgrade error for session %s: %v\n", sessionID[:8], err)
		return
	}

	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	client := &wsStreamJsonClient{conn: conn}

	// Send full message history on connect
	messages, err := s.session.StreamJSON.GetMessages(sessionID)
	if err != nil {
		// Session may not be tracked in streamjson manager (e.g., daemon restart)
		// Send empty history
		messages = nil
	}

	// Build raw message array for history
	rawMessages := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		rawMessages = append(rawMessages, msg.Raw)
	}

	historyMsg, _ := json.Marshal(map[string]interface{}{
		"type":     "history",
		"messages": rawMessages,
	})
	if err := conn.WriteMessage(websocket.TextMessage, historyMsg); err != nil {
		return
	}

	// Send current status
	status := "stopped"
	if s.session.StreamJSON.IsRunning(sessionID) {
		status = "running"
	}
	statusMsg, _ := json.Marshal(map[string]string{
		"type":   "status",
		"status": status,
	})
	if err := conn.WriteMessage(websocket.TextMessage, statusMsg); err != nil {
		return
	}

	// Register client to receive new messages
	if err := s.session.StreamJSON.RegisterClient(sessionID, client); err != nil {
		// Session not tracked, but we already sent history — just keep the connection
		// for reading client messages
	}
	defer s.session.StreamJSON.UnregisterClient(sessionID, client)

	// Read client messages (user messages and permission responses)
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// Connection closed
			break
		}

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			continue
		}

		switch envelope.Type {
		case "user_message":
			var userMsg struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(msg, &userMsg); err != nil {
				continue
			}
			if err := s.session.StreamJSON.SendUserMessage(sessionID, userMsg.Content); err != nil {
				fmt.Printf("[ws/streamjson] failed to send user message: %v\n", err)
			}

		case "permission_response":
			var permMsg struct {
				RequestID string `json:"request_id"`
				Approved  bool   `json:"approved"`
			}
			if err := json.Unmarshal(msg, &permMsg); err != nil {
				continue
			}
			if err := s.session.StreamJSON.SendPermissionResponse(sessionID, permMsg.RequestID, permMsg.Approved); err != nil {
				fmt.Printf("[ws/streamjson] failed to send permission response: %v\n", err)
			}
		}
	}
}

// Ensure wsStreamJsonClient implements streamjson.ClientSender
var _ streamjson.ClientSender = (*wsStreamJsonClient)(nil)
