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
	"github.com/sergeknystautas/schmux/internal/state"
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

// filterMouseMode removes sequences that interfere with xterm.js scrollback.
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

	// Get session and check if this is a remote session
	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("session not found: %v", err), http.StatusNotFound)
		return
	}
	if sess.IsRemoteSession() {
		s.handleRemoteTerminalWebSocket(w, r, sess)
		return
	}
	tracker, err := s.session.GetTracker(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get tracker: %v", err), http.StatusInternalServerError)
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
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
	// Use a short timeout (2s) rather than the full operation timeout — the bootstrap
	// capture (tmux capture-pane) works independently of the tracker's PTY attachment,
	// so blocking longer just delays sending the bootstrap to the client.
	attachWait := 2 * time.Second
	attachDeadline := time.Now().Add(attachWait)
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

	// Run IsRunning checks in a background goroutine so they never block
	// the main select loop (tmux has-session can take 50-250ms).
	sessionDead := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
				running := s.session.IsRunning(ctx, sessionID)
				cancel()
				if !running {
					close(sessionDead)
					return
				}
			case <-sessionDead:
				return
			}
		}
	}()

	for {
		select {
		case chunk, ok := <-outputCh:
			if !ok {
				return
			}
			// Filter terminal mode sequences that interfere with xterm.js scrollback
			filtered := filterMouseMode(chunk)
			if len(filtered) > 0 {
				if err := sendOutput("append", string(filtered)); err != nil {
					return
				}
			}
		case <-sessionDead:
			sendOutput("append", "\n[Session ended]")
			return
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
				// Clear nudge atomically — avoid using stale sess pointer.
				if strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z") {
					if s.state.ClearSessionNudge(sessionID) {
						go func() {
							if err := s.state.Save(); err != nil {
								fmt.Printf("[nudgenik] error saving nudge clear: %v\n", err)
							} else {
								s.BroadcastSessions()
							}
						}()
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

// HandleAgentSignal processes a file-based signal from an agent and updates the session nudge state.
func (s *Server) HandleAgentSignal(sessionID string, sig signal.Signal) {
	// Map signal state to nudge format for frontend compatibility
	nudgeResult := nudgenik.Result{
		State:   signal.MapStateToNudge(sig.State),
		Summary: sig.Message,
		Source:  "agent",
	}

	// Update nudge atomically — avoids overwriting concurrent changes to other session fields
	if sig.State == "working" {
		if err := s.state.UpdateSessionNudge(sessionID, ""); err != nil {
			fmt.Printf("[signal] %s - failed to clear nudge: %v\n", sessionID, err)
			return
		}
	} else {
		payload, err := json.Marshal(nudgeResult)
		if err != nil {
			fmt.Printf("[signal] %s - failed to serialize nudge: %v\n", sessionID, err)
			return
		}
		if err := s.state.UpdateSessionNudge(sessionID, string(payload)); err != nil {
			fmt.Printf("[signal] %s - failed to update nudge: %v\n", sessionID, err)
			return
		}
	}

	// Update last signal time
	s.state.UpdateSessionLastSignal(sessionID, sig.Timestamp)

	// Only increment NudgeSeq for non-working signals.
	// "working" is a clear operation, not a notification — incrementing would
	// wastefully advance the sequence and confuse frontend dedup.
	var seq uint64
	if sig.State != "working" {
		seq = s.state.IncrementNudgeSeq(sessionID)
	} else {
		seq = s.state.GetNudgeSeq(sessionID)
	}

	if err := s.state.Save(); err != nil {
		fmt.Printf("[signal] %s - failed to save state: %v\n", sessionID, err)
		return
	}

	fmt.Printf("[signal] %s - received %s signal (seq=%d): %s\n", sessionID, sig.State, seq, sig.Message)

	// Broadcast via debouncer
	go s.BroadcastSessions()
}

// handleRemoteTerminalWebSocket streams terminal output from a remote session via control mode.
func (s *Server) handleRemoteTerminalWebSocket(w http.ResponseWriter, r *http.Request, sess *state.Session) {
	sessionID := sess.ID

	// Check if session has been created on remote host yet
	// Sessions are queued during provisioning and RemotePaneID is set when created
	if sess.RemotePaneID == "" {
		// Session is still provisioning
		http.Error(w, "Session is still provisioning. Please wait and try again in a moment.", http.StatusServiceUnavailable)
		return
	}

	// Get the remote manager from session manager
	rm := s.session.GetRemoteManager()
	if rm == nil {
		http.Error(w, "remote manager not configured", http.StatusInternalServerError)
		return
	}

	// Get the connection for this session's remote host
	conn := rm.GetConnection(sess.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		http.Error(w, "remote host not connected", http.StatusServiceUnavailable)
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

	// Wrap the connection for concurrent write safety
	wsConn := &wsConn{conn: rawConn}

	// Register this connection
	s.RegisterWebSocket(sessionID, wsConn)
	defer func() {
		s.UnregisterWebSocket(sessionID, wsConn)
		wsConn.Close()
	}()

	// Subscribe to output from the remote pane
	outputChan := conn.SubscribeOutput(sess.RemotePaneID)
	defer conn.UnsubscribeOutput(sess.RemotePaneID, outputChan)

	// Handle client messages (input, pause, resume)
	controlChan := make(chan WSMessage, 10)
	go func() {
		defer close(controlChan)
		for {
			msgType, msg, err := rawConn.ReadMessage()
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

	sendOutput := func(msgType, content string) error {
		msg := WSOutputMessage{Type: msgType, Content: content}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return wsConn.WriteMessage(websocket.TextMessage, data)
	}

	// Send initial pane history (for scrollback)
	initialLines := s.config.GetTerminalBootstrapLines()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	history, err := conn.CapturePaneLines(ctx, sess.RemotePaneID, initialLines)
	cancel()
	if err != nil {
		fmt.Printf("[ws remote %s] failed to capture initial pane content: %v\n", sessionID[:8], err)
		// Send empty full message as fallback
		if err := sendOutput("full", ""); err != nil {
			return
		}
	} else {
		// Send captured history as initial full content
		if err := sendOutput("full", history); err != nil {
			return
		}
	}

	paused := false
	checkTicker := time.NewTicker(5 * time.Second) // Periodic health check
	defer checkTicker.Stop()

	for {
		select {
		case outputEvent, ok := <-outputChan:
			if !ok {
				// Channel closed, connection lost
				sendOutput("append", "\n[Remote connection lost]")
				return
			}
			if !paused && outputEvent.Data != "" {
				// Update last output time for session activity tracking
				s.state.UpdateSessionLastOutput(sessionID, time.Now())
				if err := sendOutput("append", outputEvent.Data); err != nil {
					return
				}
			}

		case <-checkTicker.C:
			// Check if remote connection is still active
			if conn == nil || !conn.IsConnected() {
				sendOutput("append", "\n[Remote host disconnected]")
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
				// Send keys to remote pane
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
				if err := conn.SendKeys(ctx, sess.RemotePaneID, msg.Data); err != nil {
					cancel()
					fmt.Printf("[ws remote %s] error sending keys: %v\n", sessionID[:8], err)
				}
				cancel()

				// Clear nudge atomically — avoid using stale sess pointer.
				if strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z") {
					if s.state.ClearSessionNudge(sessionID) {
						if err := s.state.Save(); err != nil {
							fmt.Printf("[nudgenik] error saving nudge clear: %v\n", err)
						} else {
							go s.BroadcastSessions()
						}
					}
				}
			}
		}
	}
}

// handleProvisionWebSocket streams PTY I/O for remote host provisioning.
func (s *Server) handleProvisionWebSocket(w http.ResponseWriter, r *http.Request) {
	provisionID := strings.TrimPrefix(r.URL.Path, "/ws/provision/")
	if provisionID == "" {
		http.Error(w, "provision ID is required", http.StatusBadRequest)
		return
	}

	if s.config.GetAuthEnabled() {
		if _, err := s.authenticateRequest(r); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	if s.remoteManager == nil {
		http.Error(w, "remote workspace support not enabled", http.StatusServiceUnavailable)
		return
	}

	// Parse provision ID to get host ID: "provision-remote-XXXXXXXX" -> "remote-XXXXXXXX"
	hostID := strings.TrimPrefix(provisionID, "provision-")
	if hostID == provisionID {
		http.Error(w, "invalid provision ID format", http.StatusBadRequest)
		return
	}

	// Get the connection
	conn := s.remoteManager.GetConnection(hostID)
	if conn == nil {
		fmt.Printf("[ws provision] connection not found for host %s (provisionID=%s)\n", hostID, provisionID)
		http.Error(w, "remote host connection not found", http.StatusNotFound)
		return
	}

	fmt.Printf("[ws provision %s] connection found, waiting for PTY...\n", hostID[:8])

	// Get PTY (may need to wait briefly while connection initializes)
	ptmx := conn.PTY()
	if ptmx == nil {
		// Wait up to 5 seconds for PTY to become available
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			ptmx = conn.PTY()
			if ptmx != nil {
				break
			}
		}
		if ptmx == nil {
			fmt.Printf("[ws provision %s] PTY not available after 5s timeout\n", hostID[:8])
			http.Error(w, "provisioning terminal not available", http.StatusServiceUnavailable)
			return
		}
	}

	fmt.Printf("[ws provision %s] PTY available, upgrading to WebSocket\n", hostID[:8])

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

	// Wrap the connection for concurrent write safety
	wsConn := &wsConn{conn: rawConn}
	defer wsConn.Close()

	// Handle client messages (input and resize from browser)
	inputChan := make(chan []byte, 10)
	go func() {
		defer close(inputChan)
		for {
			msgType, msg, err := rawConn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				var wsMsg WSMessage
				if err := json.Unmarshal(msg, &wsMsg); err == nil {
					switch wsMsg.Type {
					case "input":
						inputChan <- []byte(wsMsg.Data)
					case "resize":
						var resizeData struct {
							Cols int `json:"cols"`
							Rows int `json:"rows"`
						}
						if err := json.Unmarshal([]byte(wsMsg.Data), &resizeData); err == nil {
							if resizeData.Cols > 0 && resizeData.Rows > 0 {
								if err := conn.ResizePTY(uint16(resizeData.Cols), uint16(resizeData.Rows)); err != nil {
									fmt.Printf("[ws provision %s] PTY resize error: %v\n", hostID[:8], err)
								}
							}
						}
					}
				}
			} else if msgType == websocket.BinaryMessage {
				// Direct binary input (from xterm.js)
				inputChan <- msg
			}
		}
	}()

	// Subscribe to PTY output (from parseProvisioningOutput fan-out)
	outputChan := conn.SubscribePTYOutput()
	defer conn.UnsubscribePTYOutput(outputChan)

	// Forward data between WebSocket and PTY
	for {
		select {
		case data, ok := <-outputChan:
			if !ok {
				// PTY closed
				return
			}
			// Send as binary message (works better with xterm.js)
			if err := wsConn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}

		case input, ok := <-inputChan:
			if !ok {
				// WebSocket closed
				return
			}
			// Write to PTY
			if _, err := ptmx.Write(input); err != nil {
				fmt.Printf("[ws provision %s] PTY write error: %v\n", hostID[:8], err)
				return
			}

		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}
