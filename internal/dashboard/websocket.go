package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergek/schmux/internal/tmux"
)

// WSMessage represents a WebSocket message from the client
type WSMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// WSOutputMessage represents a WebSocket message to the client
type WSOutputMessage struct {
	Type    string `json:"type"`    // "full", "append"
	Content string `json:"content"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Only allow connections from the dashboard itself (localhost)
		origin := r.Header.Get("Origin")
		return origin == "http://localhost:7337"
	},
}

// handleTerminalWebSocket streams log file using byte-offset tracking.
func (s *Server) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/terminal/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("session not found: %v", err), http.StatusNotFound)
		return
	}

	// Check if session is already dead before doing anything else
	if !s.session.IsRunning(sessionID) {
		http.Error(w, "session not running", http.StatusGone)
		return
	}

	// Get log file path
	logPath, err := s.session.GetLogPath(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get log path: %v", err), http.StatusInternalServerError)
		return
	}

	// Check log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		http.Error(w, "log file not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	controlChan := make(chan WSMessage, 10)
	go func() {
		defer close(controlChan)
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				var wsMsg WSMessage
				if err := json.Unmarshal(msg, &wsMsg); err == nil {
					controlChan <- wsMsg
				}
			}
		}
	}()

	var offset int64 = 0
	paused := false
	pollInterval := 100 * time.Millisecond
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	sendOutput := func(msgType, content string) error {
		msg := WSOutputMessage{Type: msgType, Content: content}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	readFileAndSend := func(sendFull bool) error {
		// Open file (not ReadFile) to avoid race condition
		f, err := os.Open(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				sendOutput("append", "\n[Log file removed]")
				return err
			}
			fmt.Printf("[ws %s] open error: %v\n", sessionID[:8], err)
			return err
		}
		defer f.Close()

		// Get current file size from the open file handle
		info, err := f.Stat()
		if err != nil {
			fmt.Printf("[ws %s] stat error: %v\n", sessionID[:8], err)
			return err
		}
		fileSize := info.Size()

		// Truncation detection: file shrank (shouldn't happen with pipe-pane)
		if fileSize < offset {
			fmt.Printf("[ws %s] truncation fileSize=%d < offset=%d, resetting\n", sessionID[:8], fileSize, offset)
			offset = 0
			sendFull = true
		}

		// No change and not forcing full?
		if fileSize == offset && !sendFull {
			return nil
		}

		// Seek to offset and read only new bytes
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}

		// Read from offset to end
		buf := make([]byte, fileSize-offset)
		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			sendOutput("append", "\n[Failed to read log]")
			return err
		}

		data := buf[:n] // Actual bytes read

		// Send content
		if sendFull {
			if err := sendOutput("full", string(data)); err != nil {
				return err
			}
			offset = int64(len(data))
		} else {
			if err := sendOutput("append", string(data)); err != nil {
				return err
			}
			offset += int64(len(data))
		}

		return nil
	}

	// Send initial full content
	if err := readFileAndSend(true); err != nil {
		return
	}

	for {
		select {
		case <-ticker.C:
			if paused {
				continue
			}
			// Check if session is still running
			if !s.session.IsRunning(sessionID) {
				sendOutput("append", "\n[Session ended]")
				return
			}
			if err := readFileAndSend(false); err != nil {
				return
			}
		case msg, ok := <-controlChan:
			if !ok {
				return
			}
			switch msg.Type {
			case "pause":
				paused = true
			case "resume":
				paused = false
			case "input":
				if err := tmux.SendKeys(sess.TmuxSession, msg.Data); err != nil {
					fmt.Printf("Error sending keys to tmux: %v\n", err)
				}
			}
		}
	}
}
