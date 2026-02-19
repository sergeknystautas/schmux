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
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

const bootstrapCaptureLines = 5000

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

// Sequences to strip so xterm.js handles scrolling locally.
var filterSequences = [][]byte{
	// Mouse mode sequences
	[]byte("\x1b[?1000h"), // X11 mouse tracking
	[]byte("\x1b[?1002h"), // Button event tracking
	[]byte("\x1b[?1003h"), // Any event tracking
	[]byte("\x1b[?1006h"), // SGR extended mouse mode
	[]byte("\x1b[?1015h"), // urxvt mouse mode
	// Alternate screen mode - disables scrollback in xterm.js
	[]byte("\x1b[?1049h"), // Enable alternate screen
	// Erase scrollback - prevents tmux from clearing xterm.js scrollback
	[]byte("\x1b[3J"), // ED 3 (Erase Scrollback)
}

// Erase Display → Scroll Up replacement.
// tmux sends \x1b[2J during full-screen redraws which erases viewport content
// without pushing it into scrollback. Replacing with \x1b[999S (Scroll Up)
// pushes viewport lines into scrollback so they remain accessible.
var (
	eraseDisplay = []byte("\x1b[2J")
	scrollUp999  = []byte("\x1b[999S")
)

// filterMouseMode removes or replaces sequences that interfere with xterm.js scrollback.
func filterMouseMode(data []byte) []byte {
	for _, seq := range filterSequences {
		data = bytes.ReplaceAll(data, seq, nil)
	}
	data = bytes.ReplaceAll(data, eraseDisplay, scrollUp999)
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

// checkWSOrigin validates WebSocket upgrade origins.
func (s *Server) checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if s.requiresAuth() {
		return s.isAllowedOrigin(origin)
	}
	if origin == "" {
		return true
	}
	return s.isAllowedOrigin(origin)
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
	if s.requiresAuth() {
		// Local requests bypass tunnel-only auth (consistent with withAuth middleware)
		if s.authEnabled() || !s.isTrustedRequest(r) {
			if _, err := s.authenticateRequest(r); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}

	// Check if this is a conflict resolution ephemeral session (not in state store)
	if tracker := s.getCRTracker(sessionID); tracker != nil {
		s.handleCRTerminalWebSocket(w, r, sessionID, tracker)
		return
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
		CheckOrigin:     s.checkWSOrigin,
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
	tmux.ConfigureStatusBar(statusCtx, sess.TmuxSession)
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

	// Scrollback sync: tmux attach-session sends rendered screen snapshots
	// during rapid output, not the raw scrolling content. When output exceeds
	// one screen, tmux only sends the final visible rows — earlier lines exist
	// in tmux's internal scrollback but are never sent to our PTY. After a
	// burst ends (no output for scrollbackSyncDelay), we do a capture-pane to
	// resync the client with the full scrollback.
	const scrollbackSyncThreshold = 4096               // bytes before considering a sync
	const scrollbackSyncDelay = 300 * time.Millisecond // quiet period after burst
	var outputSinceSync int
	var syncTimer *time.Timer
	var syncCh <-chan time.Time
	defer func() {
		if syncTimer != nil {
			syncTimer.Stop()
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
				outputSinceSync += len(filtered)
				if outputSinceSync >= scrollbackSyncThreshold {
					if syncTimer != nil {
						syncTimer.Stop()
					}
					syncTimer = time.NewTimer(scrollbackSyncDelay)
					syncCh = syncTimer.C
				}
			}
		case <-syncCh:
			// Burst ended — resync scrollback from tmux's internal buffer.
			syncCh = nil
			syncTimer = nil
			outputSinceSync = 0
			capCtx, capCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
			snapshot, err := tmux.CaptureLastLines(capCtx, sess.TmuxSession, bootstrapCaptureLines, true)
			capCancel()
			if err == nil {
				filtered := string(filterMouseMode([]byte(snapshot)))
				if err := sendOutput("full", filtered); err != nil {
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
				// Escape (\x1b alone) also clears nudge so the spinner stops
				// immediately when the user presses Esc to interrupt an agent.
				if strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z") || msg.Data == "\x1b" {
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

// handleCRTerminalWebSocket handles WebSocket connections for ephemeral conflict resolution
// tmux sessions. Unlike handleTerminalWebSocket, this is read-only (client input is ignored)
// and uses a directly-provided tracker rather than looking up sessions in the state store.
func (s *Server) handleCRTerminalWebSocket(w http.ResponseWriter, r *http.Request,
	tmuxName string, tracker *session.SessionTracker) {

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     s.checkWSOrigin,
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	outputCh := tracker.AttachWebSocket()
	defer tracker.DetachWebSocket(outputCh)

	// Wait briefly for tracker to attach
	deadline := time.Now().Add(2 * time.Second)
	for !tracker.IsAttached() && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}

	// Bootstrap with scrollback
	capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
	bootstrap, _ := tmux.CaptureLastLines(capCtx, tmuxName, bootstrapCaptureLines, true)
	capCancel()
	if bootstrap != "" {
		filtered := string(filterMouseMode([]byte(bootstrap)))
		msg, _ := json.Marshal(WSOutputMessage{Type: "full", Content: filtered})
		conn.WriteMessage(websocket.TextMessage, msg)
	}

	// Read-only: drain client messages (required by gorilla) but ignore input
	controlChan := make(chan struct{})
	go func() {
		defer close(controlChan)
		for {
			_, _, err := rawConn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Stream output until connection closes or tracker stops
	for {
		select {
		case data, ok := <-outputCh:
			if !ok {
				return
			}
			filtered := filterMouseMode(data)
			if len(filtered) == 0 {
				continue
			}
			msg, _ := json.Marshal(WSOutputMessage{Type: "append", Content: string(filtered)})
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-controlChan:
			return
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
	payload, err := json.Marshal(nudgeResult)
	if err != nil {
		fmt.Printf("[signal] %s - failed to serialize nudge: %v\n", sessionID, err)
		return
	}
	if err := s.state.UpdateSessionNudge(sessionID, string(payload)); err != nil {
		fmt.Printf("[signal] %s - failed to update nudge: %v\n", sessionID, err)
		return
	}

	// Update last signal time
	s.state.UpdateSessionLastSignal(sessionID, sig.Timestamp)

	seq := s.state.IncrementNudgeSeq(sessionID)

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
		CheckOrigin:     s.checkWSOrigin,
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	history, err := conn.CapturePaneLines(ctx, sess.RemotePaneID, bootstrapCaptureLines)
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
				// Escape (\x1b alone) also clears nudge so the spinner stops
				// immediately when the user presses Esc to interrupt an agent.
				if strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z") || msg.Data == "\x1b" {
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

	if s.requiresAuth() {
		// Local requests bypass tunnel-only auth (consistent with withAuth middleware)
		if s.authEnabled() || !s.isTrustedRequest(r) {
			if _, err := s.authenticateRequest(r); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
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
		CheckOrigin:     s.checkWSOrigin,
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
