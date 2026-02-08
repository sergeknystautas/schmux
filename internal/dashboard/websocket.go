package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

const bootstrapCaptureLines = 200

// Terminal query response prefixes to filter from input.
// These are responses from xterm.js to queries from tmux - we don't send them back.
var inputFilterPrefixes = []string{
	"\x1b[?",   // DA1 response (e.g., \x1b[?1;2c)
	"\x1b[>",   // DA2 response (e.g., \x1b[>0;276;0c)
	"\x1b]10;", // OSC 10 foreground color response
	"\x1b]11;", // OSC 11 background color response
}

// isTerminalResponse checks if input is a terminal query response that shouldn't be sent.
func isTerminalResponse(data string) bool {
	for _, prefix := range inputFilterPrefixes {
		if strings.HasPrefix(data, prefix) {
			return true
		}
	}
	return false
}

// Sequences to filter out so xterm.js handles scrolling locally.
var filterSequences = [][]byte{
	// Mouse mode sequences
	[]byte("\x1b[?1000h"), // X11 mouse tracking
	[]byte("\x1b[?1002h"), // Button event tracking
	[]byte("\x1b[?1003h"), // Any event tracking
	[]byte("\x1b[?1006h"), // SGR extended mouse mode
	[]byte("\x1b[?1015h"), // urxvt mouse mode
	// Alternate screen mode - disables scrollback in xterm.js
	[]byte("\x1b[?1049h"), // Enable alternate screen
}

// filterTerminalModes removes sequences that interfere with xterm.js scrollback.
func filterMouseMode(data []byte) []byte {
	for _, seq := range filterSequences {
		data = bytes.ReplaceAll(data, seq, nil)
	}
	return data
}

// WSMessage represents a WebSocket message from the client.
type WSMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// WSOutputMessage represents a WebSocket message to the client.
type WSOutputMessage struct {
	Type    string `json:"type"` // "full", "append"
	Content string `json:"content"`
}

// handleTerminalWebSocket streams tmux output to websocket clients.
// It sends a bootstrap snapshot from capture-pane and then forwards live bytes
// from the per-session tracker PTY.
func (s *Server) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/terminal/")
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

	// Check if session is already dead before upgrading.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
	if !s.session.IsRunning(ctx, sessionID) {
		cancel()
		http.Error(w, "session not running", http.StatusGone)
		return
	}
	cancel()

	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get session: %v", err), http.StatusInternalServerError)
		return
	}
	tracker, err := s.session.GetTracker(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get tracker: %v", err), http.StatusInternalServerError)
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if s.config.GetAuthEnabled() {
				return s.isAllowedOrigin(origin)
			}
			if origin == "" {
				return true
			}
			return s.isAllowedOrigin(origin)
		},
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn := &wsConn{conn: rawConn}
	s.RegisterWebSocket(sessionID, conn)
	defer func() {
		s.UnregisterWebSocket(sessionID, conn)
		conn.Close()
	}()

	sendOutput := func(msgType, content string) error {
		msg := WSOutputMessage{Type: msgType, Content: content}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	// Attach output stream immediately after websocket upgrade to avoid
	// dropping output generated during bootstrap capture/status setup.
	outputCh := tracker.AttachWebSocket()
	defer tracker.DetachWebSocket(outputCh)

	// A websocket can connect before the tracker finishes its first attach retry.
	// Give it a short window to come up so early pane output is not missed.
	attachDeadline := time.Now().Add(time.Duration(s.config.GetXtermOperationTimeoutMs()) * time.Millisecond)
	for !tracker.IsAttached() && time.Now().Before(attachDeadline) {
		time.Sleep(25 * time.Millisecond)
	}

	// Bootstrap with recent scrollback to avoid a blank terminal on connect.
	capCtx, capCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	bootstrap, err := tmux.CaptureLastLines(capCtx, sess.TmuxSession, bootstrapCaptureLines, true)
	capCancel()
	if err != nil {
		fmt.Printf("[ws %s] bootstrap capture failed: %v\n", sessionID[:8], err)
		bootstrap = ""
	}
	filteredBootstrap := string(filterMouseMode([]byte(bootstrap)))
	if err := sendOutput("full", filteredBootstrap); err != nil {
		return
	}

	// Configure status bar on connect (for existing sessions or future config changes)
	statusCtx, statusCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	_ = tmux.SetOption(statusCtx, sess.TmuxSession, "status-left", "#{pane_current_command} ")
	_ = tmux.SetOption(statusCtx, sess.TmuxSession, "window-status-format", "")
	_ = tmux.SetOption(statusCtx, sess.TmuxSession, "window-status-current-format", "")
	_ = tmux.SetOption(statusCtx, sess.TmuxSession, "status-right", "")
	statusCancel()

	// Flush any output that arrived while bootstrap/status setup was running.
	for {
		select {
		case chunk, ok := <-outputCh:
			if !ok {
				return
			}
			filtered := filterMouseMode(chunk)
			if len(filtered) > 0 {
				if err := sendOutput("append", string(filtered)); err != nil {
					return
				}
			}
		default:
			goto drained
		}
	}
drained:

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

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case chunk, ok := <-outputCh:
			if !ok {
				return
			}
			// Filter terminal mode sequences that interfere with xterm.js scrollback
			filtered := filterMouseMode(chunk)
			// Check for schmux OSC signals and strip them from output
			signals, cleanData := signal.ExtractAndStripSignals(filtered)
			for _, sig := range signals {
				s.handleAgentSignal(sessionID, sig)
			}
			if len(cleanData) > 0 {
				if err := sendOutput("append", string(cleanData)); err != nil {
					return
				}
			}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
			running := s.session.IsRunning(ctx, sessionID)
			cancel()
			if !running {
				sendOutput("append", "\n[Session ended]")
				return
			}
		case msg, ok := <-controlChan:
			if !ok {
				return
			}

			switch msg.Type {
			case "input":
				// Skip terminal query responses - these are xterm.js responding to tmux queries
				if isTerminalResponse(msg.Data) {
					continue
				}
				// Preserve existing nudge-clearing behavior.
				if sess.Nudge != "" && (strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z")) {
					sess.Nudge = ""
					if err := s.state.UpdateSession(*sess); err != nil {
						fmt.Printf("[nudgenik] error clearing nudge: %v\n", err)
					} else if err := s.state.Save(); err != nil {
						fmt.Printf("[nudgenik] error saving nudge clear: %v\n", err)
					} else {
						go s.BroadcastSessions()
					}
				}
				if err := tracker.SendInput(msg.Data); err != nil {
					fmt.Printf("[terminal] error sending keys to tmux: %v\n", err)
					// Don't return - input failure shouldn't kill connection
				}
			case "resize":
				var resizeData struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &resizeData); err != nil {
					fmt.Printf("[terminal] error parsing resize data: %v\n", err)
					continue
				}
				if resizeData.Cols <= 0 || resizeData.Rows <= 0 {
					continue
				}
				// Query tmux as source of truth and skip duplicate resize requests.
				queryCtx, queryCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
				currentCols, currentRows, err := tmux.GetWindowSize(queryCtx, sess.TmuxSession)
				queryCancel()
				if err != nil {
					fmt.Printf("[terminal] error querying tmux window size: %v\n", err)
				} else if currentCols == resizeData.Cols && currentRows == resizeData.Rows {
					continue
				}

				// Resize tmux and attached tracker PTY.
				resizeCtx, resizeCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
				if err := tmux.ResizeWindow(resizeCtx, sess.TmuxSession, resizeData.Cols, resizeData.Rows); err != nil {
					fmt.Printf("[terminal] error resizing tmux window: %v\n", err)
				}
				resizeCancel()
				if err := tracker.Resize(resizeData.Cols, resizeData.Rows); err != nil {
					fmt.Printf("[terminal] error resizing PTY: %v\n", err)
				}
			}
		}
	}
}

// handleAgentSignal processes an OSC 777 signal from an agent and updates the session nudge state.
func (s *Server) handleAgentSignal(sessionID string, sig signal.Signal) {
	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		return
	}

	// Map signal state to nudge format for frontend compatibility
	nudgeResult := nudgenik.Result{
		State:   signal.MapStateToNudge(sig.State),
		Summary: sig.Message,
		Source:  "agent",
	}

	// "working" clears the nudge
	if sig.State == "working" {
		sess.Nudge = ""
	} else {
		payload, err := json.Marshal(nudgeResult)
		if err != nil {
			fmt.Printf("[signal] %s - failed to serialize nudge: %v\n", sessionID, err)
			return
		}
		sess.Nudge = string(payload)
	}

	// Update last signal time (in-memory only)
	s.state.UpdateSessionLastSignal(sessionID, sig.Timestamp)

	if err := s.state.UpdateSession(*sess); err != nil {
		fmt.Printf("[signal] %s - failed to update session: %v\n", sessionID, err)
		return
	}
	if err := s.state.Save(); err != nil {
		fmt.Printf("[signal] %s - failed to save state: %v\n", sessionID, err)
		return
	}

	fmt.Printf("[signal] %s - received %s signal: %s\n", sessionID[:8], sig.State, sig.Message)

	// Broadcast the update to all clients
	go s.BroadcastSessions()
}
