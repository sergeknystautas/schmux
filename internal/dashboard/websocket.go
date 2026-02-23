package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/escbuf"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
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

// WSMessage represents a WebSocket message from the client.
type WSMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// WSOutputMessage represents a WebSocket message to the client (used by remote sessions).
type WSOutputMessage struct {
	Type    string `json:"type"` // "full", "append"
	Content string `json:"content"`
}

// WSStatsMessage represents a periodic diagnostics stats message sent on the terminal WebSocket.
type WSStatsMessage struct {
	Type              string `json:"type"`
	EventsDelivered   int64  `json:"eventsDelivered"`
	EventsDropped     int64  `json:"eventsDropped"`
	BytesDelivered    int64  `json:"bytesDelivered"`
	BytesPerSec       int64  `json:"bytesPerSec"`
	Reconnects        int64  `json:"controlModeReconnects"`
	SyncChecksSent    int64  `json:"syncChecksSent"`
	SyncCorrections   int64  `json:"syncCorrections"`
	SyncSkippedActive int64  `json:"syncSkippedActive"`
}

// WSSyncCursor holds cursor position for sync messages.
type WSSyncCursor struct {
	Row     int  `json:"row"`
	Col     int  `json:"col"`
	Visible bool `json:"visible"`
}

// WSSyncMessage is a periodic screen snapshot sent to the frontend for desync detection.
type WSSyncMessage struct {
	Type   string       `json:"type"`
	Screen string       `json:"screen"`
	Cursor WSSyncCursor `json:"cursor"`
}

// buildSyncMessage constructs a sync message from a capture-pane output and cursor state.
func buildSyncMessage(screen string, cursor controlmode.CursorState) WSSyncMessage {
	return WSSyncMessage{
		Type:   "sync",
		Screen: screen,
		Cursor: WSSyncCursor{Row: cursor.Y, Col: cursor.X, Visible: cursor.Visible},
	}
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

// handleTerminalWebSocket streams tmux output to websocket clients via binary frames.
// It sends a bootstrap snapshot from capture-pane and then forwards live bytes
// from the per-session control mode output subscription.
func (s *Server) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/terminal/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}
	if s.requiresAuth() {
		if s.authEnabled() || !s.isTrustedRequest(r) {
			if _, err := s.authenticateRequest(r); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}

	// Check if this is a conflict resolution ephemeral session
	if tracker := s.getCRTracker(sessionID); tracker != nil {
		s.handleCRTerminalWebSocket(w, r, sessionID, tracker)
		return
	}

	// Check if session is running
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
	if !s.session.IsRunning(ctx, sessionID) {
		cancel()
		http.Error(w, "session not running", http.StatusGone)
		return
	}
	cancel()

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
		WriteBufferSize: 8192,
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

	// Wait for tracker to attach before subscribing
	attachDeadline := time.Now().Add(2 * time.Second)
	for !tracker.IsAttached() && time.Now().Before(attachDeadline) {
		time.Sleep(25 * time.Millisecond)
	}

	// Start reading client messages early so we can process resize before bootstrap
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

	// Wait briefly for frontend to send terminal size via resize message
	// Frontend calls sendResize() immediately on WebSocket open, so this should
	// arrive within ~10-100ms. This ensures we know the terminal size before
	// sending bootstrap content.
	var preBootstrapMessages []WSMessage
	resizeDeadline := time.Now().Add(100 * time.Millisecond)
resizeWaitLoop:
	for time.Now().Before(resizeDeadline) {
		select {
		case msg, ok := <-controlChan:
			if !ok {
				// Channel closed (connection lost)
				return
			}
			if msg.Type == "resize" {
				var resizeData struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &resizeData); err == nil && resizeData.Cols > 0 && resizeData.Rows > 0 {
					if err := tracker.Resize(resizeData.Cols, resizeData.Rows); err == nil {
						// Successfully received and processed resize before bootstrap
						break resizeWaitLoop
					}
				}
			}
			// Buffer non-resize messages to be processed by the main loop after bootstrap
			preBootstrapMessages = append(preBootstrapMessages, msg)
		case <-time.After(time.Until(resizeDeadline)):
			// Timeout reached, proceed with bootstrap
			break resizeWaitLoop
		}
	}

	// Re-queue buffered messages so the main loop can process them
	// Use non-blocking send to avoid deadlock if channel is full
	for _, msg := range preBootstrapMessages {
		select {
		case controlChan <- msg:
			// Successfully re-queued
		default:
			// Channel full, process immediately to avoid losing the message
			// This handles input messages that need to clear nudges, etc.
			switch msg.Type {
			case "input":
				if !isTerminalResponse(msg.Data) {
					if strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z") || msg.Data == "\x1b" {
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
					if err := tracker.SendInput(msg.Data); err != nil {
						logging.Sub(s.logger, "terminal").Error("failed to send input", "err", err)
					}
				}
			case "resize":
				var resizeData struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &resizeData); err == nil && resizeData.Cols > 0 && resizeData.Rows > 0 {
					if err := tracker.Resize(resizeData.Cols, resizeData.Rows); err != nil {
						logging.Sub(s.logger, "terminal").Error("failed to resize", "err", err)
					}
				}
			}
		}
	}

	// Bootstrap with scrollback — send as binary frame
	// Use tracker's control mode capture if attached, fall back to tmux CLI
	capCtx, capCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	bootstrap, err := tracker.CaptureLastLines(capCtx, bootstrapCaptureLines)
	if err != nil {
		// Fallback to direct tmux CLI capture
		bootstrap, err = tmux.CaptureLastLines(capCtx, sess.TmuxSession, bootstrapCaptureLines, true)
		if err != nil {
			logging.Sub(s.logger, "ws").Error("bootstrap capture failed", "session_id", sessionID[:8], "err", err)
			bootstrap = ""
		}
	}
	capCancel()

	// Restore cursor state (position + visibility) so xterm.js matches tmux.
	// capture-pane doesn't preserve terminal modes like cursor visibility,
	// so without this: (1) the cursor sits at column 0 of the last non-empty
	// line, and (2) a hidden cursor (e.g. Claude Code's TUI) shows as visible.
	var curX, curY int
	var curVisible bool
	curCtx, curCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	curState, curErr := tracker.GetCursorState(curCtx)
	curCancel()
	if curErr == nil {
		curX, curY, curVisible = curState.X, curState.Y, curState.Visible
	} else {
		// Fallback to tmux CLI
		curCtx2, curCancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		cliState, cliErr := tmux.GetCursorState(curCtx2, sess.TmuxSession)
		curCancel2()
		if cliErr == nil {
			curX, curY, curVisible = cliState.X, cliState.Y, cliState.Visible
			curErr = nil
		}
	}
	if curErr == nil {
		// CSI H is 1-indexed
		bootstrap += fmt.Sprintf("\033[%d;%dH", curY+1, curX+1)
		// DECTCEM: cursor visibility
		if curVisible {
			bootstrap += "\033[?25h"
		} else {
			bootstrap += "\033[?25l"
		}
	}

	// Subscribe to output — after capture to avoid TOCTOU double-delivery.
	// Events arriving after subscribe are guaranteed not to be in the bootstrap snapshot.
	outputCh := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(outputCh)

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(bootstrap)); err != nil {
		return
	}

	// Escape-sequence holdback: prevents partial ANSI sequences at frame boundaries
	var escHoldback []byte

	// Sync check counters (per-connection)
	var syncChecksSent atomic.Int64
	var syncCorrections atomic.Int64
	var syncSkippedActive atomic.Int64

	// Dev mode diagnostics: ring buffer and stats ticker
	var ringBuf *RingBuffer
	var statsTickerC <-chan time.Time
	var statsTicker *time.Ticker
	var prevBytes int64
	var prevTime time.Time
	if s.devMode {
		ringBuf = NewRingBuffer(256 * 1024) // 256KB
		statsTicker = time.NewTicker(3 * time.Second)
		statsTickerC = statsTicker.C
		prevTime = time.Now()
		defer statsTicker.Stop()
	}

	// Session liveness check
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

	// Control mode health monitor — notify frontend when tmux control mode detaches/reattaches
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		lastAttached := true // assume attached at start
		for {
			select {
			case <-ticker.C:
				attached := tracker.IsAttached()
				if attached != lastAttached {
					msg, _ := json.Marshal(map[string]interface{}{
						"type":     "controlMode",
						"attached": attached,
					})
					conn.WriteMessage(websocket.TextMessage, msg)
					lastAttached = attached
				}
			case <-sessionDead:
				return
			}
		}
	}()

	// Activity-triggered sync: large output events trigger debounced sync check
	syncNow := make(chan struct{}, 1)

	// Periodic sync check goroutine — sends screen snapshots for desync detection
	go func() {
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()

		interval := 10 * time.Second

		for {
			select {
			case <-timer.C:
			case <-syncNow:
				// Debounce: wait 200ms for activity to settle
				debounce := time.NewTimer(200 * time.Millisecond)
			drainSync:
				for {
					select {
					case <-syncNow:
					case <-debounce.C:
						break drainSync
					}
				}
				debounce.Stop()
			case <-sessionDead:
				return
			}

			if conn.IsClosed() {
				return
			}

			capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
			screen, err := tracker.CapturePane(capCtx)
			capCancel()
			if err != nil {
				timer.Reset(interval)
				continue
			}

			cursorCtx, cursorCancel := context.WithTimeout(context.Background(), 2*time.Second)
			cursor, err := tracker.GetCursorState(cursorCtx)
			cursorCancel()
			if err != nil {
				timer.Reset(interval)
				continue
			}

			msg := buildSyncMessage(screen, cursor)
			data, _ := json.Marshal(msg)
			syncChecksSent.Add(1)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

			timer.Reset(interval)
		}
	}()

	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				// Flush any held-back bytes before closing
				if len(escHoldback) > 0 {
					conn.WriteMessage(websocket.BinaryMessage, escHoldback)
				}
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(1000, "session ended"))
				return
			}
			if len(event.Data) > 0 {
				send, hb := escbuf.SplitClean(escHoldback, []byte(event.Data))
				escHoldback = hb
				if ringBuf != nil && len(send) > 0 {
					ringBuf.Write(send)
				}
				if len(send) > 0 {
					if err := conn.WriteMessage(websocket.BinaryMessage, send); err != nil {
						return
					}
					// Trigger activity-based sync for large output (TUI redraws)
					if len(send) > 500 {
						select {
						case syncNow <- struct{}{}:
						default: // already pending
						}
					}
				}
			}
		case <-statsTickerC:
			counters := tracker.DiagnosticCounters()
			now := time.Now()
			elapsed := now.Sub(prevTime).Seconds()
			currentBytes := counters["bytesDelivered"]
			var bytesPerSec int64
			if elapsed > 0 {
				bytesPerSec = int64(float64(currentBytes-prevBytes) / elapsed)
			}
			prevBytes = currentBytes
			prevTime = now
			statsMsg := WSStatsMessage{
				Type:              "stats",
				EventsDelivered:   counters["eventsDelivered"],
				EventsDropped:     counters["eventsDropped"],
				BytesDelivered:    counters["bytesDelivered"],
				BytesPerSec:       bytesPerSec,
				Reconnects:        counters["controlModeReconnects"],
				SyncChecksSent:    syncChecksSent.Load(),
				SyncCorrections:   syncCorrections.Load(),
				SyncSkippedActive: syncSkippedActive.Load(),
			}
			data, _ := json.Marshal(statsMsg)
			conn.WriteMessage(websocket.TextMessage, data)
		case <-sessionDead:
			// Flush any held-back bytes before closing
			if len(escHoldback) > 0 {
				conn.WriteMessage(websocket.BinaryMessage, escHoldback)
			}
			conn.WriteMessage(websocket.BinaryMessage, []byte("\n[Session ended]"))
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(1000, "session ended"))
			return
		case msg, ok := <-controlChan:
			if !ok {
				return
			}
			switch msg.Type {
			case "input":
				if isTerminalResponse(msg.Data) {
					continue
				}
				if strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z") || msg.Data == "\x1b" {
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
				if err := tracker.SendInput(msg.Data); err != nil {
					logging.Sub(s.logger, "terminal").Error("failed to send input", "err", err)
				}
			case "resize":
				var resizeData struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &resizeData); err != nil {
					continue
				}
				if resizeData.Cols <= 0 || resizeData.Rows <= 0 {
					continue
				}
				if err := tracker.Resize(resizeData.Cols, resizeData.Rows); err != nil {
					logging.Sub(s.logger, "terminal").Error("failed to resize", "err", err)
				}
			case "syncResult":
				var result struct {
					Corrected bool  `json:"corrected"`
					DiffRows  []int `json:"diffRows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &result); err != nil {
					break
				}
				if result.Corrected {
					syncCorrections.Add(1)
					logging.Sub(s.logger, "sync").Debug("corrected rows", "session_id", sessionID[:8], "rows_count", len(result.DiffRows), "diff_rows", result.DiffRows)
				} else {
					syncSkippedActive.Add(1)
				}
			case "diagnostic":
				if !s.devMode {
					break
				}
				// Capture tmux screen via control mode
				capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
				tmuxScreen, err := tracker.CaptureLastLines(capCtx, 0)
				capCancel()
				if err != nil {
					logging.Sub(s.logger, "diagnostic").Debug("capture-pane failed", "session_id", sessionID[:8], "err", err)
					break
				}
				counters := tracker.DiagnosticCounters()
				// Build findings from automated checks
				findings := []string{}
				verdict := ""
				if counters["eventsDropped"] > 0 {
					findings = append(findings, fmt.Sprintf("%d events dropped", counters["eventsDropped"]))
					verdict = "Events were dropped due to channel backpressure."
				} else {
					findings = append(findings, "No drops detected")
					verdict = "No obvious cause found. Likely a bootstrap race during TUI redraw."
				}
				// Snapshot ring buffer
				var rbSnapshot []byte
				if ringBuf != nil {
					rbSnapshot = ringBuf.Snapshot()
				}
				// Write diagnostic directory
				diagDir := filepath.Join(os.Getenv("HOME"), ".schmux", "diagnostics",
					fmt.Sprintf("%s-%s", time.Now().Format("2006-01-02T15-04-05"), sessionID))
				diag := &DiagnosticCapture{
					Timestamp:  time.Now(),
					SessionID:  sessionID,
					Cols:       tracker.LastTerminalCols,
					Rows:       tracker.LastTerminalRows,
					Counters:   counters,
					TmuxScreen: tmuxScreen,
					RingBuffer: rbSnapshot,
					Findings:   findings,
					Verdict:    verdict,
				}
				if err := diag.WriteToDir(diagDir); err != nil {
					logging.Sub(s.logger, "diagnostic").Error("write failed", "session_id", sessionID[:8], "err", err)
				}
				// Send response back to client
				resp := map[string]interface{}{
					"type":       "diagnostic",
					"diagDir":    diagDir,
					"counters":   counters,
					"findings":   findings,
					"verdict":    verdict,
					"tmuxScreen": tmuxScreen,
				}
				data, _ := json.Marshal(resp)
				conn.WriteMessage(websocket.TextMessage, data)
			}
		}
	}
}

// handleCRTerminalWebSocket handles WebSocket connections for ephemeral conflict resolution
// tmux sessions. Read-only (client input is ignored), uses control mode API and binary frames.
func (s *Server) handleCRTerminalWebSocket(w http.ResponseWriter, r *http.Request,
	tmuxName string, tracker *session.SessionTracker) {

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 8192,
		CheckOrigin:     s.checkWSOrigin,
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	// Wait briefly for tracker to attach before subscribing
	deadline := time.Now().Add(2 * time.Second)
	for !tracker.IsAttached() && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}

	// Bootstrap with scrollback — send as binary frame
	// Fall back to tmux CLI capture if control mode not attached
	capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
	bootstrap, err := tracker.CaptureLastLines(capCtx, bootstrapCaptureLines)
	if err != nil {
		bootstrap, _ = tmux.CaptureLastLines(capCtx, tmuxName, bootstrapCaptureLines, true)
	}
	capCancel()
	if bootstrap != "" {
		conn.WriteMessage(websocket.BinaryMessage, []byte(bootstrap))
	}

	// Subscribe to output — after capture to avoid TOCTOU double-delivery
	outputCh := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(outputCh)

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

	// Escape-sequence holdback for CR terminal
	var escHoldback []byte

	// Stream output until connection closes or tracker stops
	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				if len(escHoldback) > 0 {
					conn.WriteMessage(websocket.BinaryMessage, escHoldback)
				}
				return
			}
			if len(event.Data) == 0 {
				continue
			}
			send, hb := escbuf.SplitClean(escHoldback, []byte(event.Data))
			escHoldback = hb
			if len(send) > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, send); err != nil {
					return
				}
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
		logging.Sub(s.logger, "signal").Error("failed to serialize nudge", "session_id", sessionID, "err", err)
		return
	}
	if err := s.state.UpdateSessionNudge(sessionID, string(payload)); err != nil {
		logging.Sub(s.logger, "signal").Error("failed to update nudge", "session_id", sessionID, "err", err)
		return
	}

	// Update last signal time
	s.state.UpdateSessionLastSignal(sessionID, sig.Timestamp)

	seq := s.state.IncrementNudgeSeq(sessionID)

	if err := s.state.Save(); err != nil {
		logging.Sub(s.logger, "signal").Error("failed to save state", "session_id", sessionID, "err", err)
		return
	}

	logging.Sub(s.logger, "signal").Info("received signal", "session_id", sessionID, "state", sig.State, "seq", seq, "message", sig.Message)

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
		logging.Sub(s.logger, "ws").Error("failed to capture initial pane content", "session_id", sessionID[:8], "err", err)
		// Send empty full message as fallback
		if err := sendOutput("full", ""); err != nil {
			return
		}
	} else {
		// Restore cursor state (position + visibility)
		curCtx, curCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		curState, curErr := conn.GetCursorState(curCtx, sess.RemotePaneID)
		curCancel()
		if curErr == nil {
			history += fmt.Sprintf("\033[%d;%dH", curState.Y+1, curState.X+1)
			if curState.Visible {
				history += "\033[?25h"
			} else {
				history += "\033[?25l"
			}
		}
		// Send captured history as initial full content
		if err := sendOutput("full", history); err != nil {
			return
		}
	}

	// Subscribe to output — after capture to avoid TOCTOU double-delivery
	outputChan := conn.SubscribeOutput(sess.RemotePaneID)
	defer conn.UnsubscribeOutput(sess.RemotePaneID, outputChan)

	paused := false
	checkTicker := time.NewTicker(5 * time.Second) // Periodic health check
	defer checkTicker.Stop()

	// Escape-sequence holdback for remote terminal
	var escHoldback []byte

	for {
		select {
		case outputEvent, ok := <-outputChan:
			if !ok {
				// Flush holdback before disconnect message
				if len(escHoldback) > 0 {
					sendOutput("append", string(escHoldback))
				}
				// Channel closed, connection lost
				sendOutput("append", "\n[Remote connection lost]")
				return
			}
			if !paused && outputEvent.Data != "" {
				// Update last output time for session activity tracking
				s.state.UpdateSessionLastOutput(sessionID, time.Now())
				send, hb := escbuf.SplitClean(escHoldback, []byte(outputEvent.Data))
				escHoldback = hb
				if len(send) > 0 {
					if err := sendOutput("append", string(send)); err != nil {
						return
					}
				}
			}

		case <-checkTicker.C:
			// Check if remote connection is still active
			if conn == nil || !conn.IsConnected() {
				if len(escHoldback) > 0 {
					sendOutput("append", string(escHoldback))
				}
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
					logging.Sub(s.logger, "ws").Error("failed to send keys", "session_id", sessionID[:8], "err", err)
				}
				cancel()

				// Clear nudge atomically — avoid using stale sess pointer.
				// Escape (\x1b alone) also clears nudge so the spinner stops
				// immediately when the user presses Esc to interrupt an agent.
				if strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z") || msg.Data == "\x1b" {
					if s.state.ClearSessionNudge(sessionID) {
						if err := s.state.Save(); err != nil {
							logging.Sub(s.logger, "nudgenik").Error("failed to save nudge clear", "err", err)
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
		logging.Sub(s.logger, "ws").Error("connection not found", "host_id", hostID, "provision_id", provisionID)
		http.Error(w, "remote host connection not found", http.StatusNotFound)
		return
	}

	logging.Sub(s.logger, "ws").Info("connection found, waiting for PTY", "host_id", hostID[:8])

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
			logging.Sub(s.logger, "ws").Error("PTY not available after timeout", "host_id", hostID[:8])
			http.Error(w, "provisioning terminal not available", http.StatusServiceUnavailable)
			return
		}
	}

	logging.Sub(s.logger, "ws").Info("PTY available, upgrading to WebSocket", "host_id", hostID[:8])

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
									logging.Sub(s.logger, "ws").Error("PTY resize error", "host_id", hostID[:8], "err", err)
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
				logging.Sub(s.logger, "ws").Error("PTY write error", "host_id", hostID[:8], "err", err)
				return
			}

		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}
