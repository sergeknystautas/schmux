package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergek/schmux/internal/tmux"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for localhost
	},
}

// handleTerminalWebSocket handles WebSocket connections for terminal streaming.
func (s *Server) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from URL
	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/terminal/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Verify session exists
	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("session not found: %v", err), http.StatusNotFound)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Channel for client control messages (pause/resume)
	controlChan := make(chan string, 10)

	// Goroutine to read client messages without blocking
	go func() {
		defer close(controlChan)
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				controlChan <- string(msg)
			}
		}
	}()

	// Send terminal output periodically
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Track pause state
	paused := false

	// Get initial output
	output, err := tmux.CaptureOutput(sess.TmuxSession)
	if err == nil {
		conn.WriteMessage(websocket.TextMessage, []byte(output))
	}

	for {
		select {
		case <-ticker.C:
			if paused {
				continue
			}

			// Check if session still exists
			if _, err := s.session.GetSession(sessionID); err != nil {
				conn.WriteMessage(websocket.TextMessage, []byte("\n[Session ended]"))
				return
			}

			// Capture new output
			output, err := tmux.CaptureOutput(sess.TmuxSession)
			if err != nil {
				// Session may have ended
				conn.WriteMessage(websocket.TextMessage, []byte("\n[Session ended]"))
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, []byte(output)); err != nil {
				return
			}

		case msg, ok := <-controlChan:
			if !ok {
				// Client closed connection
				return
			}
			// Handle client messages (pause/resume)
			switch msg {
			case "pause":
				paused = true
			case "resume":
				paused = false
			}
		}
	}
}
