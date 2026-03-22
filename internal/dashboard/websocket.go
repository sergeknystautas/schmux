package dashboard

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/escbuf"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// ioWorkspaceTelemetryProvider is implemented by workspace.Manager when IO telemetry is enabled.
type ioWorkspaceTelemetryProvider interface {
	IOWorkspaceTelemetrySnapshot(reset bool) workspace.IOWorkspaceTelemetrySnapshot
}

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

// appendSequencedFrame appends a binary WebSocket frame (8-byte big-endian
// sequence header + terminal data) to dst and returns the extended slice.
// When dst has sufficient capacity, this avoids heap allocation entirely.
func appendSequencedFrame(dst []byte, seq uint64, data []byte) []byte {
	needed := 8 + len(data)
	dst = grow(dst, needed)
	binary.BigEndian.PutUint64(dst[:8], seq)
	copy(dst[8:], data)
	return dst[:needed]
}

// grow returns a byte slice of length 0 with at least cap n, reusing dst's
// backing array when possible.
func grow(dst []byte, n int) []byte {
	if cap(dst) >= n {
		return dst[:n]
	}
	return make([]byte, n)
}

// bootstrapFrameSeq reserves a sequence number for the bootstrap frame by
// appending a zero-length entry to the output log. This ensures the bootstrap
// frame's seq is strictly less than the first live output event's seq,
// preventing the frontend's dedup logic from dropping the first keystroke echo.
func bootstrapFrameSeq(log *session.OutputLog) uint64 {
	return log.Append(nil)
}

// buildGapReplayFrames replays missing events from the output log as sequenced frames.
// Each entry is sent as its own frame tagged with its original sequence number,
// ensuring the frontend's per-seq dedup correctly skips already-received entries.
// Returns nil if the requested data has been evicted from the log.
func buildGapReplayFrames(log *session.OutputLog, fromSeq uint64) [][]byte {
	entries := log.ReplayFrom(fromSeq)
	if entries == nil {
		return nil // data evicted
	}
	var frames [][]byte
	for _, e := range entries {
		// Each frame must be independently owned (cold path, not pooled).
		frames = append(frames, appendSequencedFrame(nil, e.Seq, e.Data))
	}
	return frames
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
	Type              string              `json:"type"`
	EventsDelivered   int64               `json:"eventsDelivered"`
	EventsDropped     int64               `json:"eventsDropped"`
	BytesDelivered    int64               `json:"bytesDelivered"`
	BytesPerSec       int64               `json:"bytesPerSec"`
	Reconnects        int64               `json:"controlModeReconnects"`
	SyncChecksSent    int64               `json:"syncChecksSent"`
	SyncCorrections   int64               `json:"syncCorrections"`
	SyncSkippedActive int64               `json:"syncSkippedActive"`
	ClientFanOutDrops int64               `json:"clientFanOutDrops"`
	FanOutDrops       int64               `json:"fanOutDrops"`
	CurrentSeq        uint64              `json:"currentSeq"`
	LogOldestSeq      uint64              `json:"logOldestSeq"`
	LogTotalBytes     int64               `json:"logTotalBytes"`
	SyncDisabled      bool                `json:"syncDisabled"`
	InputLatency      *LatencyPercentiles `json:"inputLatency,omitempty"`
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
	Forced bool         `json:"forced,omitempty"`
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
	sessionID := chi.URLParam(r, "id")
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

	// Check if this is the floor manager session
	if s.floorManager != nil && s.floorManager.TmuxSession() == sessionID {
		if tracker := s.floorManager.Tracker(); tracker != nil {
			s.handleFMTerminalWebSocket(w, r, sessionID, tracker)
			return
		}
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

	rawConn, err := s.upgradeWebSocket(w, r, 4096, 8192)
	if err != nil {
		return
	}
	conn := &wsConn{conn: rawConn}
	tracker.Counters.WsConnections.Add(1)
	s.RegisterWebSocket(sessionID, conn)
	defer func() {
		s.UnregisterWebSocket(sessionID, conn)
		conn.Close()
	}()

	// Wait for tracker to attach before subscribing
	waitForTrackerAttach(tracker, 2*time.Second)

	// Start reading client messages early so we can process resize before bootstrap
	controlChan := startWSMessageReader(conn)

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
					s.clearNudgeOnInput(sessionID, msg.Data)
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

	// Bootstrap: capture-pane snapshot for the initial screen content.
	// We use capture-pane (not output log replay) because tmux server-side
	// operations like clear-history are not recorded in the output log,
	// which would cause scrollback divergence between xterm.js and tmux.
	// The output log is used only for gap detection and live event replay.
	outputLog := tracker.OutputLog()

	// Escape-sequence holdback: prevents partial ANSI sequences at frame boundaries
	var escHoldback []byte
	var escScratch []byte

	// Reusable frame buffer for appendSequencedFrame (amortizes allocation to 0 after warmup)
	var frameBuf []byte

	// Subscribe BEFORE bootstrap capture to prevent TOCTOU: events arriving
	// between capture and subscribe would otherwise be lost. We drain any
	// events with seq <= bootstrapSeq after bootstrap to avoid double-delivery.
	outputCh := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(outputCh)

	capCtx, capCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	bootstrap, err := tracker.CaptureLastLines(capCtx, bootstrapCaptureLines)
	if err != nil {
		bootstrap, err = tmux.CaptureLastLines(capCtx, sess.TmuxSession, bootstrapCaptureLines, true)
		if err != nil {
			logging.Sub(s.logger, "ws").Error("bootstrap capture failed", "session_id", sessionID[:8], "err", err)
			bootstrap = ""
		}
	}
	capCancel()

	// Reserve a sequence number for the bootstrap frame. This ensures the
	// bootstrap frame's seq is strictly less than the first live event's seq,
	// preventing the frontend's dedup logic from dropping the first keystroke echo.
	// The drain boundary (CurrentSeq after reservation) identifies which events
	// are already reflected in the capture-pane snapshot.
	bootstrapSeq := bootstrapFrameSeq(outputLog)
	drainBoundary := outputLog.CurrentSeq()
	if bootstrap != "" {
		frameBuf = appendSequencedFrame(frameBuf, bootstrapSeq, []byte(bootstrap))
		if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
			return
		}
	}

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
		// Cursor restoration is ephemeral (not logged), sent as a separate unsequenced frame
		cursorRestore := fmt.Sprintf("\033[%d;%dH", curY+1, curX+1)
		if curVisible {
			cursorRestore += "\033[?25h"
		} else {
			cursorRestore += "\033[?25l"
		}
		// Use bootstrap seq for the cursor restore frame
		frameBuf = appendSequencedFrame(frameBuf, bootstrapSeq, []byte(cursorRestore))
		if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
			return
		}
	}

	// Drain any events that arrived during bootstrap (seq < drainBoundary).
	// These are already reflected in the capture-pane snapshot.
drainBootstrap:
	for {
		select {
		case ev, ok := <-outputCh:
			if !ok {
				return
			}
			if ev.Seq >= drainBoundary {
				// This event is post-bootstrap — process it normally below.
				// Push it back by handling it inline before entering the main loop.
				if len(ev.Data) > 0 {
					send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(ev.Data))
					escHoldback = hb
					escScratch = so
					// Always send a frame to preserve seq continuity (see main loop comment).
					frameBuf = appendSequencedFrame(frameBuf, ev.Seq, send)
					if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
						return
					}
				}
				break drainBootstrap
			}
			// seq < drainBoundary — already in bootstrap snapshot, skip
		default:
			break drainBootstrap
		}
	}

	// Signal bootstrap complete — the frontend uses this to enable gap detection.
	// Without this, the frontend would detect false gaps between bootstrap chunks
	// (each chunk's seq is its last entry, so there are apparent gaps between chunks).
	bootstrapMsg, _ := json.Marshal(map[string]string{"type": "bootstrapComplete"})
	if err := conn.WriteMessage(websocket.TextMessage, bootstrapMsg); err != nil {
		return
	}

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
	}
	_, hasIOTelemetry := s.workspace.(ioWorkspaceTelemetryProvider)
	if s.devMode || hasIOTelemetry {
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

	// Periodic defense-in-depth sync — sends screen snapshots for paranoia desync detection.
	// Also triggers immediately when tmux pauses output delivery (pause-after).
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()

		interval := 60 * time.Second
		var lastDropsSeen int64

		doSync := func(reason string) {
			if conn.IsClosed() {
				return
			}

			syncStart := time.Now()
			capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
			screen, err := tracker.CapturePane(capCtx)
			capCancel()
			capDur := time.Since(syncStart)
			if err != nil {
				return
			}

			cursorStart := time.Now()
			cursorCtx, cursorCancel := context.WithTimeout(context.Background(), 2*time.Second)
			cursor, err := tracker.GetCursorState(cursorCtx)
			cursorCancel()
			cursorDur := time.Since(cursorStart)
			if err != nil {
				return
			}

			totalDur := time.Since(syncStart)
			if s.devMode {
				logging.Sub(s.logger, "sync").Info("sync commands completed",
					"session_id", sessionID[:8],
					"reason", reason,
					"capture_ms", capDur.Milliseconds(),
					"cursor_ms", cursorDur.Milliseconds(),
					"total_ms", totalDur.Milliseconds(),
					"screen_len", len(screen),
				)
			}

			counters := tracker.DiagnosticCounters()
			currentDrops := counters["eventsDropped"] + counters["clientFanOutDrops"] + counters["fanOutDrops"]
			forced := currentDrops > lastDropsSeen || reason == "pause"
			lastDropsSeen = currentDrops

			msg := buildSyncMessage(screen, cursor)
			msg.Forced = forced
			data, _ := json.Marshal(msg)
			syncChecksSent.Add(1)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				tracker.Counters.WsWriteErrors.Add(1)
			}
		}

		for {
			select {
			case <-timer.C:
				doSync("timer")
				timer.Reset(interval)
			case <-tracker.SyncTrigger():
				doSync("pause")
				timer.Reset(interval)
			case <-sessionDead:
				return
			}
		}
	}()

	// Latency instrumentation: track per-keystroke timing segments.
	latencyCollector := NewLatencyCollector()
	type pendingInputTiming struct {
		dispatch      time.Duration
		sendKeys      time.Duration
		t3            time.Time // SendKeys return time — echo timer starts here
		outputChDepth int       // len(outputCh) when input case fired
	}
	// FIFO queue: each keystroke pushes its timing; each echo event pops the
	// oldest. This replaces a singleton pointer that silently discarded all
	// but the last keystroke in a burst (survivorship bias).
	var pendingInputQueue []pendingInputTiming

	// Async input sender: moves tracker.SendInput() off the select loop so
	// that the ~77ms tmux Execute() round-trip doesn't block echo delivery.
	type inputBatch struct {
		data          string
		t1            time.Time // controlChan receive
		t2            time.Time // pre-dispatch
		outputChDepth int
	}
	type inputResult struct {
		sendKeysDur   time.Duration
		t3            time.Time // post-SendKeys
		dispatch      time.Duration
		outputChDepth int
	}
	inputBatchCh := make(chan inputBatch, 10)
	inputDoneCh := make(chan inputResult, 10)
	go func() {
		defer close(inputDoneCh)
		for batch := range inputBatchCh {
			t2 := time.Now()
			err := tracker.SendInput(batch.data)
			t3 := time.Now()
			if err != nil {
				logging.Sub(s.logger, "terminal").Error("failed to send input", "err", err)
				continue
			}
			inputDoneCh <- inputResult{
				sendKeysDur:   t3.Sub(t2),
				t3:            t3,
				dispatch:      batch.t2.Sub(batch.t1),
				outputChDepth: batch.outputChDepth,
			}
		}
	}()
	defer close(inputBatchCh)

	var lastEventTime time.Time
	for {
		select {
		case event, ok := <-outputCh:
			t4 := time.Now() // latency: output event arrival
			if s.devMode && !lastEventTime.IsZero() {
				gap := t4.Sub(lastEventTime)
				if gap > 500*time.Millisecond {
					logging.Sub(s.logger, "ws").Info("output gap",
						"session_id", sessionID[:8],
						"gap_ms", gap.Milliseconds(),
						"ch_depth", len(outputCh),
						"data_len", len(event.Data),
					)
				}
			}
			lastEventTime = t4
			if !ok {
				// Flush any held-back bytes before closing
				if len(escHoldback) > 0 {
					seq := outputLog.Append(escHoldback)
					conn.WriteMessage(websocket.BinaryMessage, appendSequencedFrame(frameBuf, seq, escHoldback))
				}
				serverCloseMsg, _ := json.Marshal(map[string]string{"type": "serverClose", "reason": "trackerStopped"})
				conn.WriteMessage(websocket.TextMessage, serverCloseMsg) // best-effort; informs frontend before WS close
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(1000, "session ended"))
				return
			}
			if len(event.Data) > 0 {
				send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(event.Data))
				escHoldback = hb
				escScratch = so
				if ringBuf != nil && len(send) > 0 {
					ts := []byte(fmt.Sprintf("\n--- %s ---\n", time.Now().Format("15:04:05.000000")))
					ringBuf.Write(ts)
					ringBuf.Write(send)
				}
				// Always send a frame to preserve sequence continuity, even when
				// escbuf holds back the entire event (send is empty). Without this,
				// the skipped seq creates a phantom gap on the frontend, triggering
				// a gap replay whose chunked data can duplicate already-delivered
				// bytes and corrupt the terminal state (e.g. cursor jumps).
				frameBuf = appendSequencedFrame(frameBuf, event.Seq, send)
				if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
					tracker.Counters.WsWriteErrors.Add(1)
					return
				}
				// Latency: record sample if we have a pending input timing
				if len(pendingInputQueue) > 0 {
					pending := pendingInputQueue[0]
					pendingInputQueue = pendingInputQueue[1:]
					t5 := time.Now()
					serverTotalMs := float64(pending.dispatch+pending.sendKeys+t4.Sub(pending.t3)+t5.Sub(t4)) / float64(time.Millisecond)
					latencyCollector.Add(LatencySample{
						Dispatch:      pending.dispatch,
						SendKeys:      pending.sendKeys,
						Echo:          t4.Sub(pending.t3),
						FrameSend:     t5.Sub(t4),
						OutputChDepth: pending.outputChDepth,
						EchoDataLen:   len(event.Data),
					})
					// Per-keystroke sideband: send the server-side total for this
					// specific keystroke so the frontend can compute a paired
					// residual instead of combining independent percentiles.
					if s.devMode {
						sideband, _ := json.Marshal(map[string]interface{}{
							"type":        "inputEcho",
							"serverMs":    serverTotalMs,
							"dispatchMs":  float64(pending.dispatch) / float64(time.Millisecond),
							"sendKeysMs":  float64(pending.sendKeys) / float64(time.Millisecond),
							"echoMs":      float64(t4.Sub(pending.t3)) / float64(time.Millisecond),
							"frameSendMs": float64(t5.Sub(t4)) / float64(time.Millisecond),
						})
						conn.WriteMessage(websocket.TextMessage, sideband)
					}
				}
			} else if len(pendingInputQueue) > 0 {
				pendingInputQueue = pendingInputQueue[1:] // discard stale timing — echo had no data
			}
		case <-statsTickerC:
			if s.devMode {
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
					ClientFanOutDrops: counters["clientFanOutDrops"],
					FanOutDrops:       counters["fanOutDrops"],
					CurrentSeq:        tracker.OutputLog().CurrentSeq(),
					LogOldestSeq:      tracker.OutputLog().OldestSeq(),
					LogTotalBytes:     tracker.OutputLog().TotalBytes(),
					SyncDisabled:      true,
					InputLatency:      latencyCollector.Percentiles(),
				}
				data, _ := json.Marshal(statsMsg)
				conn.WriteMessage(websocket.TextMessage, data)
			}
			if ioProvider, ok := s.workspace.(ioWorkspaceTelemetryProvider); ok {
				ioSnap := ioProvider.IOWorkspaceTelemetrySnapshot(false)
				ioStatsMsg := map[string]interface{}{
					"type":            "io-workspace-stats",
					"totalCommands":   ioSnap.TotalCommands,
					"totalDurationMs": ioSnap.TotalDurationMS,
					"triggerCounts":   ioSnap.TriggerCounts,
					"counters":        ioSnap.Counters,
				}
				ioData, _ := json.Marshal(ioStatsMsg)
				conn.WriteMessage(websocket.TextMessage, ioData)
			}
		case result := <-inputDoneCh:
			// Async sender completed — record timing in the queue
			pendingInputQueue = append(pendingInputQueue, pendingInputTiming{
				dispatch:      result.dispatch,
				sendKeys:      result.sendKeysDur,
				t3:            result.t3,
				outputChDepth: result.outputChDepth,
			})
		case <-sessionDead:
			// Flush any held-back bytes before closing
			if len(escHoldback) > 0 {
				seq := outputLog.Append(escHoldback)
				conn.WriteMessage(websocket.BinaryMessage, appendSequencedFrame(frameBuf, seq, escHoldback))
			}
			endMsg := []byte("\n[Session ended]")
			endSeq := outputLog.Append(endMsg)
			conn.WriteMessage(websocket.BinaryMessage, appendSequencedFrame(frameBuf, endSeq, endMsg))
			sessionDeadMsg, _ := json.Marshal(map[string]string{"type": "serverClose", "reason": "sessionDead"})
			conn.WriteMessage(websocket.TextMessage, sessionDeadMsg) // best-effort; informs frontend before WS close
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(1000, "session ended"))
			return
		case msg, ok := <-controlChan:
			if !ok {
				return
			}
			switch msg.Type {
			case "input":
				t1 := time.Now() // latency: controlChan receive
				outputChDepth := len(outputCh)
				combined := msg.Data

				// Non-blocking drain: coalesce queued keystrokes (Fix 3).
				// Reduces N serial Execute() calls to 1 for burst typing.
				var deferredMsgs []WSMessage
			drain:
				for {
					select {
					case extra, ok := <-controlChan:
						if !ok {
							return
						}
						if extra.Type == "input" {
							if !isTerminalResponse(extra.Data) {
								combined += extra.Data
							}
						} else if extra.Type == "resize" {
							// Handle resize inline during drain — it is the only
							// plausible concurrent control message during typing.
							var rd struct {
								Cols int `json:"cols"`
								Rows int `json:"rows"`
							}
							if err := json.Unmarshal([]byte(extra.Data), &rd); err == nil && rd.Cols > 0 && rd.Rows > 0 {
								tracker.Resize(rd.Cols, rd.Rows)
							}
						} else {
							// Other message types (gap, syncResult, diagnostic) —
							// defer to the next main-loop iteration.
							deferredMsgs = append(deferredMsgs, extra)
						}
					default:
						break drain
					}
				}

				// Re-queue deferred non-input messages for the main switch.
				for _, d := range deferredMsgs {
					select {
					case controlChan <- d:
					default:
						// Channel full — unlikely, but log and drop.
						logging.Sub(s.logger, "terminal").Error("dropped deferred message during drain", "type", d.Type)
					}
				}

				if isTerminalResponse(combined) {
					continue
				}
				s.clearNudgeOnInput(sessionID, combined)
				inputBatchCh <- inputBatch{
					data:          combined,
					t1:            t1,
					t2:            time.Now(),
					outputChDepth: outputChDepth,
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
			case "gap":
				var gapData struct {
					FromSeq string `json:"fromSeq"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &gapData); err != nil {
					break
				}
				fromSeq, err := strconv.ParseUint(gapData.FromSeq, 10, 64)
				if err != nil {
					break
				}
				frames := buildGapReplayFrames(tracker.OutputLog(), fromSeq)
				for _, frame := range frames {
					if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
						tracker.Counters.WsWriteErrors.Add(1)
						return
					}
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
				// Capture cursor state
				curCtx, curCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				cursorState, curErr := tracker.GetCursorState(curCtx)
				curCancel()
				counters := tracker.DiagnosticCounters()
				// Enrich counters with output log metrics for findings analysis
				counters["currentSeq"] = int64(tracker.OutputLog().CurrentSeq())
				counters["logOldestSeq"] = int64(tracker.OutputLog().OldestSeq())
				// Build findings from automated checks
				findings, verdict := buildDiagnosticFindings(counters)
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
					Cols:       int(tracker.LastTerminalCols.Load()),
					Rows:       int(tracker.LastTerminalRows.Load()),
					Counters:   counters,
					TmuxScreen: tmuxScreen,
					RingBuffer: rbSnapshot,
					Findings:   findings,
					Verdict:    verdict,
				}
				if curErr == nil {
					diag.CursorTmuxX = cursorState.X
					diag.CursorTmuxY = cursorState.Y
					diag.CursorTmuxVisible = cursorState.Visible
				} else {
					diag.CursorTmuxErr = curErr.Error()
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
			case "io-workspace-diagnostic":
				ioProvider, ok := s.workspace.(ioWorkspaceTelemetryProvider)
				if !ok {
					break
				}
				ioSnap := ioProvider.IOWorkspaceTelemetrySnapshot(false)
				ioDiag := workspace.NewIOWorkspaceDiagnosticCapture(ioSnap, time.Now())
				ioDiagDir := filepath.Join(os.Getenv("HOME"), ".schmux", "diagnostics",
					fmt.Sprintf("%s-io-workspace", time.Now().Format("2006-01-02T15-04-05")))
				if err := ioDiag.WriteToDir(ioDiagDir); err != nil {
					logging.Sub(s.logger, "io-workspace-diagnostic").Error("write failed", "err", err)
					break
				}
				ioResp := map[string]interface{}{
					"type":     "io-workspace-diagnostic",
					"diagDir":  ioDiagDir,
					"counters": ioSnap.Counters,
					"findings": ioDiag.Findings,
					"verdict":  ioDiag.Verdict,
				}
				ioData, _ := json.Marshal(ioResp)
				conn.WriteMessage(websocket.TextMessage, ioData)
			}
		}
	}
}

// handleCRTerminalWebSocket handles WebSocket connections for ephemeral conflict resolution
// tmux sessions. Read-only (client input is ignored), uses control mode API and binary frames.
func (s *Server) handleCRTerminalWebSocket(w http.ResponseWriter, r *http.Request,
	tmuxName string, tracker *session.SessionTracker) {

	rawConn, err := s.upgradeWebSocket(w, r, 4096, 8192)
	if err != nil {
		return
	}
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	// Wait briefly for tracker to attach before subscribing
	waitForTrackerAttach(tracker, 2*time.Second)

	// Bootstrap with scrollback — send as binary frame
	// Fall back to tmux CLI capture if control mode not attached
	capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
	bootstrap, err := tracker.CaptureLastLines(capCtx, bootstrapCaptureLines)
	if err != nil {
		bootstrap, _ = tmux.CaptureLastLines(capCtx, tmuxName, bootstrapCaptureLines, true)
	}
	capCancel()
	if bootstrap != "" {
		conn.WriteMessage(websocket.BinaryMessage, appendSequencedFrame(nil, 0, []byte(bootstrap)))
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
	var escScratch []byte
	var lastSeq uint64
	var frameBuf []byte

	// Stream output until connection closes or tracker stops
	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				if len(escHoldback) > 0 {
					conn.WriteMessage(websocket.BinaryMessage, appendSequencedFrame(frameBuf, lastSeq, escHoldback))
				}
				return
			}
			if len(event.Data) == 0 {
				continue
			}
			lastSeq = event.Seq
			send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(event.Data))
			escHoldback = hb
			escScratch = so
			if len(send) > 0 {
				frameBuf = appendSequencedFrame(frameBuf, event.Seq, send)
				if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
					return
				}
			}
		case <-controlChan:
			return
		}
	}
}

// handleFMTerminalWebSocket handles WebSocket connections for the floor manager
// tmux session. Supports bidirectional I/O (input + output) since the operator
// types commands into the FM terminal.
func (s *Server) handleFMTerminalWebSocket(w http.ResponseWriter, r *http.Request,
	tmuxName string, tracker *session.SessionTracker) {

	rawConn, err := s.upgradeWebSocket(w, r, 4096, 8192)
	if err != nil {
		return
	}
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	// Wait briefly for tracker to attach before subscribing
	waitForTrackerAttach(tracker, 2*time.Second)

	// Start reading client messages
	controlChan := startWSMessageReader(rawConn)

	// Wait for initial resize from frontend
	resizeDeadline := time.Now().Add(100 * time.Millisecond)
resizeWait:
	for time.Now().Before(resizeDeadline) {
		select {
		case msg, ok := <-controlChan:
			if !ok {
				return
			}
			if msg.Type == "resize" {
				var rd struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &rd); err == nil && rd.Cols > 0 && rd.Rows > 0 {
					tracker.Resize(rd.Cols, rd.Rows)
					break resizeWait
				}
			}
		case <-time.After(time.Until(resizeDeadline)):
			break resizeWait
		}
	}

	// Bootstrap with scrollback
	capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
	bootstrap, err := tracker.CaptureLastLines(capCtx, bootstrapCaptureLines)
	if err != nil {
		bootstrap, _ = tmux.CaptureLastLines(capCtx, tmuxName, bootstrapCaptureLines, true)
	}
	capCancel()
	if bootstrap != "" {
		conn.WriteMessage(websocket.BinaryMessage, appendSequencedFrame(nil, 0, []byte(bootstrap)))
	}

	// Subscribe to output after bootstrap
	outputCh := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(outputCh)

	// Escape-sequence holdback
	var escHoldback []byte
	var escScratch []byte
	var lastSeq uint64
	var frameBuf []byte

	// Stream output and process input
	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				if len(escHoldback) > 0 {
					conn.WriteMessage(websocket.BinaryMessage, appendSequencedFrame(frameBuf, lastSeq, escHoldback))
				}
				return
			}
			if len(event.Data) == 0 {
				continue
			}
			lastSeq = event.Seq
			send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(event.Data))
			escHoldback = hb
			escScratch = so
			if len(send) > 0 {
				frameBuf = appendSequencedFrame(frameBuf, event.Seq, send)
				if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
					return
				}
			}
		case msg, ok := <-controlChan:
			if !ok {
				return
			}
			switch msg.Type {
			case "input":
				if err := tracker.SendInput(msg.Data); err != nil {
					s.logger.Error("fm terminal: failed to send input", "err", err)
				}
			case "resize":
				var rd struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &rd); err == nil && rd.Cols > 0 && rd.Rows > 0 {
					tracker.Resize(rd.Cols, rd.Rows)
				}
			}
		}
	}
}

// HandleStatusEvent processes a status event from the unified event system.
// Maps event fields to nudge format for frontend compatibility.
//
// State priority prevents transient states from overwriting terminal/blocking ones:
//   - Tier 0 (transient): Working, Idle
//   - Tier 1 (blocking):  Needs Input, Needs Attention, Needs Feature Clarification
//   - Tier 2 (terminal):  Completed, Error
//
// "Working" is special — it always overwrites (means a new turn started).
// All other states can only overwrite states at the same or lower tier.
func (s *Server) HandleStatusEvent(sessionID, state, message, intent, blockers string) {
	// Map event state to nudge format for frontend compatibility
	nudgeState := mapEventStateToNudge(state)

	// State priority: check if the incoming state is allowed to overwrite the current one.
	// "Working" is the universal reset (new turn started) and always overwrites.
	currentSession, _ := s.state.GetSession(sessionID)
	if nudgeState != "Working" && currentSession.Nudge != "" {
		var currentNudge nudgenik.Result
		if err := json.Unmarshal([]byte(currentSession.Nudge), &currentNudge); err == nil {
			if nudgeStateTier(nudgeState) < nudgeStateTier(currentNudge.State) {
				logging.Sub(s.logger, "events").Debug("skipping lower-priority state",
					"session_id", sessionID, "incoming", nudgeState, "current", currentNudge.State)
				return
			}
		}
	}

	summary := message
	if summary == "" && intent != "" {
		summary = intent
	}

	nudgeResult := nudgenik.Result{
		State:   nudgeState,
		Summary: summary,
		Source:  "agent",
	}

	// Update nudge atomically
	payload, err := json.Marshal(nudgeResult)
	if err != nil {
		logging.Sub(s.logger, "events").Error("failed to serialize nudge", "session_id", sessionID, "err", err)
		return
	}

	// Skip nudge seq increment if the nudge payload hasn't changed.
	// This prevents duplicate sounds when hooks fire multiple times
	// for the same permission prompt (e.g., permission_prompt + elicitation_dialog).
	nudgeChanged := currentSession.Nudge != string(payload)

	if err := s.state.UpdateSessionNudge(sessionID, string(payload)); err != nil {
		logging.Sub(s.logger, "events").Error("failed to update nudge", "session_id", sessionID, "err", err)
		return
	}

	// Update last signal time
	s.state.UpdateSessionLastSignal(sessionID, time.Now())

	var seq uint64
	if nudgeChanged {
		seq = s.state.IncrementNudgeSeq(sessionID)
	} else {
		seq = s.state.GetNudgeSeq(sessionID)
	}

	if err := s.state.Save(); err != nil {
		logging.Sub(s.logger, "events").Error("failed to save state", "session_id", sessionID, "err", err)
		return
	}

	logging.Sub(s.logger, "events").Info("received status event", "session_id", sessionID, "state", state, "seq", seq, "message", message)

	// Broadcast via debouncer
	go s.BroadcastSessions()
}

// mapEventStateToNudge maps event state strings to nudge display states.
func mapEventStateToNudge(state string) string {
	switch state {
	case "needs_input":
		return "Needs Input"
	case "needs_testing":
		return "Needs Attention"
	case "completed":
		return "Completed"
	case "error":
		return "Error"
	case "working":
		return "Working"
	case "idle":
		return "Idle"
	default:
		return state
	}
}

// nudgeStateTier returns the priority tier of a nudge display state.
// Higher tiers represent more "important" states that shouldn't be
// overwritten by lower-tier transient states.
//
//	Tier 0: Working, Idle (transient activity indicators)
//	Tier 1: Needs Input, Needs Attention, Needs Feature Clarification (blocking)
//	Tier 2: Completed, Error (terminal)
func nudgeStateTier(displayState string) int {
	switch displayState {
	case "Needs Input", "Needs Attention", "Needs Feature Clarification":
		return 1
	case "Completed", "Error":
		return 2
	default:
		return 0
	}
}
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

	rawConn, err := s.upgradeWebSocket(w, r, 1024, 1024)
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
	controlChan := startWSMessageReader(rawConn)

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
	var escScratch []byte

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
				send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(outputEvent.Data))
				escHoldback = hb
				escScratch = so
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
				s.clearNudgeOnInput(sessionID, msg.Data)
			}
		}
	}
}

// handleProvisionWebSocket streams PTY I/O for remote host provisioning.
func (s *Server) handleProvisionWebSocket(w http.ResponseWriter, r *http.Request) {
	provisionID := chi.URLParam(r, "id")
	if provisionID == "" {
		http.Error(w, "provision ID is required", http.StatusBadRequest)
		return
	}

	if s.requiresAuth() {
		// Local requests bypass tunnel-only auth (consistent with authMiddleware)
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

	rawConn, err := s.upgradeWebSocket(w, r, 1024, 1024)
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

// buildDiagnosticFindings analyzes counters from the session tracker and
// produces a list of human-readable findings and an overall verdict.
func buildDiagnosticFindings(counters map[string]int64) (findings []string, verdict string) {
	findings = []string{}
	hasIssue := false

	// Check for drops across the pipeline
	totalDrops := counters["eventsDropped"] + counters["clientFanOutDrops"] + counters["fanOutDrops"]
	if totalDrops > 0 {
		hasIssue = true
		if counters["eventsDropped"] > 0 {
			findings = append(findings, fmt.Sprintf("%d events dropped at parser level", counters["eventsDropped"]))
		}
		if counters["clientFanOutDrops"] > 0 {
			findings = append(findings, fmt.Sprintf("%d events dropped at client fan-out", counters["clientFanOutDrops"]))
		}
		if counters["fanOutDrops"] > 0 {
			findings = append(findings, fmt.Sprintf("%d events dropped at tracker fan-out", counters["fanOutDrops"]))
		}
		verdict = fmt.Sprintf("%d total events dropped due to channel backpressure across pipeline.", totalDrops)
	}

	// Check for control mode reconnects
	if counters["controlModeReconnects"] > 0 {
		hasIssue = true
		findings = append(findings, fmt.Sprintf("%d control mode reconnect(s) — possible output gaps during reconnection", counters["controlModeReconnects"]))
	}

	// Check if output log is near capacity (>80% full: 40000 out of 50000)
	currentSeq := counters["currentSeq"]
	logOldestSeq := counters["logOldestSeq"]
	if currentSeq > 0 && logOldestSeq > 0 {
		logSize := currentSeq - logOldestSeq
		if logSize > 40000 {
			hasIssue = true
			findings = append(findings, fmt.Sprintf("output log near capacity (%d/%d entries used)", logSize, 50000))
		}
	}

	// Check for repeated WS reconnects (wsConnections tracks cumulative opens per session tracker)
	if counters["wsConnections"] > 1 {
		hasIssue = true
		findings = append(findings, fmt.Sprintf("terminal was reconnected %d time(s) — each reconnect triggers a full bootstrap replay", counters["wsConnections"]))
	}

	// Check for WS write errors (write failure causes immediate disconnect → reconnect loop)
	if counters["wsWriteErrors"] > 0 {
		hasIssue = true
		findings = append(findings, fmt.Sprintf("%d WS write error(s) caused disconnect(s)", counters["wsWriteErrors"]))
	}

	// Check if sync is disabled
	if counters["syncDisabled"] == 1 {
		findings = append(findings, "Periodic sync is disabled (gap detection is the primary consistency mechanism)")
	}

	if !hasIssue {
		findings = append([]string{"No drops or anomalies detected"}, findings...)
		verdict = "No obvious backend cause found. Check frontend gap stats and screen diff."
	}

	return findings, verdict
}
