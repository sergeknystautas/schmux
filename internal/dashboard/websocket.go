package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

// findLastNLinesOffset finds the byte offset for the last N lines in a file.
// Reads the file backwards in chunks to efficiently find the position.
// Returns the byte offset where the last N lines start, or 0 if the file has fewer than N lines.
func findLastNLinesOffset(path string, n int) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat file: %w", err)
	}

	pos := stat.Size()
	newlineCount := 0
	chunkSize := int64(4096)

	buf := make([]byte, chunkSize)

	for newlineCount <= n && pos > 0 {
		readSize := chunkSize
		if pos < chunkSize {
			readSize = pos
		}
		pos -= readSize

		_, err := f.Seek(pos, io.SeekStart)
		if err != nil {
			return 0, fmt.Errorf("seek to %d: %w", pos, err)
		}

		nRead, err := f.Read(buf[:readSize])
		if err != nil {
			return 0, fmt.Errorf("read chunk: %w", err)
		}

		// Count newlines backwards
		for i := nRead - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				newlineCount++
				if newlineCount > n {
					// Found our position - return byte after this newline
					return pos + int64(i) + 1, nil
				}
			}
		}
	}

	// Found fewer than N newlines, return start of file
	return 0, nil
}

// findSafeStartPoint finds a safe byte offset to start reading from.
// Starting from the given offset, scans forward to find a position that's
// safe to start rendering (after a newline or carriage return).
// This avoids starting in the middle of an ANSI escape sequence.
func findSafeStartPoint(path string, startOffset int64, maxScan int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return startOffset, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return startOffset, fmt.Errorf("stat file: %w", err)
	}

	fileSize := stat.Size()
	if startOffset >= fileSize {
		return startOffset, nil
	}

	// Limit how far we scan forward
	maxPos := startOffset + maxScan
	if maxPos > fileSize {
		maxPos = fileSize
	}

	_, err = f.Seek(startOffset, io.SeekStart)
	if err != nil {
		return startOffset, fmt.Errorf("seek: %w", err)
	}

	// Read in chunks and look for safe start points
	buf := make([]byte, 4096)
	pos := startOffset

	for pos < maxPos {
		readSize := int64(len(buf))
		if pos+readSize > maxPos {
			readSize = maxPos - pos
		}

		n, err := f.Read(buf[:readSize])
		if err != nil {
			return startOffset, fmt.Errorf("read: %w", err)
		}
		if n == 0 {
			break
		}

		// Look for newline or carriage return - these are safe boundaries
		for i := 0; i < n; i++ {
			if buf[i] == '\n' || buf[i] == '\r' {
				return pos + int64(i) + 1, nil
			}
		}

		pos += int64(n)
	}

	// Didn't find a safe boundary, return original offset
	return startOffset, nil
}

// extractANSISequences scans the file up to endOffset and extracts all ANSI CSI sequences
// to prime terminal state before sending bootstrapped content.
// Uses "last seen wins" deduplication to minimize data transfer.
// If endOffset is 0, scans the entire file.
func extractANSISequences(path string, endOffset int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Read up to endOffset (or entire file if endOffset is 0)
	var data []byte
	data = make([]byte, endOffset)
	n, err := f.Read(data)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read file: %w", err)
	}
	data = data[:n]

	// Use map for deduplication - key is sequence string, value is (position, bytes)
	// Track last position where each unique sequence appeared
	type seqInfo struct {
		pos     int
		content []byte
	}
	uniqueSeqs := make(map[string]seqInfo)
	skipList := []byte{'H', 'f', 'A', 'B', 'C', 'D', 'J', 'K', 's', 'u', 'E', 'G', 'L', 'M', 'P', 'Z', '@', '`'}

	i := 0

	// Scan for ESC [ (CSI sequences start with \033[)
	for i < len(data)-1 {
		if data[i] == '\033' && i+1 < len(data) && data[i+1] == '[' {
			// Found CSI sequence start, find the end
			startPos := i
			j := i + 2
			for j < len(data) {
				// CSI sequences end with a character in range 0x40-0x7E
				if data[j] >= 0x40 && data[j] <= 0x7E {
					// Found the sequence terminator
					seq := data[i : j+1]

					// Filter out cursor movement sequences
					terminator := data[j]
					if !contains(skipList, terminator) {
						// Store/overwrite with this sequence and position (last seen wins)
						uniqueSeqs[string(seq)] = seqInfo{pos: startPos, content: seq}
					}
					break
				}
				j++
			}
			i = j + 1
		} else {
			i++
		}
	}

	// Sort unique sequences by their last position in the file
	type seqWithPos struct {
		key     string
		pos     int
		content []byte
	}
	sorted := make([]seqWithPos, 0, len(uniqueSeqs))
	for k, v := range uniqueSeqs {
		sorted = append(sorted, seqWithPos{key: k, pos: v.pos, content: v.content})
	}

	// Simple sort by position
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].pos > sorted[j].pos {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var sequences []byte
	for _, s := range sorted {
		sequences = append(sequences, s.content...)
	}

	return sequences, nil
}

// contains checks if a byte slice contains a specific byte
func contains(slice []byte, b byte) bool {
	for _, v := range slice {
		if v == b {
			return true
		}
	}
	return false
}

// rotateLogFile rotates a log file that has exceeded the size threshold.
// Keeps approximately the last 1MB of data by copying the tail to a temp file,
// replacing the original, and restarting pipe-pane.
func (s *Server) rotateLogFile(ctx context.Context, sessionID, logPath string) error {
	fmt.Printf("[ws %s] rotating log file (size > threshold)\n", sessionID[:8])

	// Get the session to find the tmux session name
	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// 1. Stop pipe-pane
	if err := tmux.StopPipePane(ctx, sess.TmuxSession); err != nil {
		return fmt.Errorf("failed to stop pipe-pane: %w", err)
	}

	// 2. Copy the last ~N MB to a temp file
	keepSize := s.config.GetXtermRotatedLogSizeMB() * 1024 * 1024 // Convert MB to bytes
	srcFile, err := os.Open(logPath)
	if err != nil {
		// If we can't open the file, try restarting pipe-pane anyway
		tmux.StartPipePane(ctx, sess.TmuxSession, logPath)
		return fmt.Errorf("failed to open log file for rotation: %w", err)
	}

	srcInfo, err := srcFile.Stat()
	if err != nil {
		srcFile.Close()
		tmux.StartPipePane(ctx, sess.TmuxSession, logPath)
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	fileSize := srcInfo.Size()
	var offset int64 = 0
	if fileSize > keepSize {
		offset = fileSize - keepSize
	}

	// Find a safe start point (after a newline) to avoid mid-line corruption
	safeOffset, err := findSafeStartPoint(logPath, offset, 4096)
	if err != nil {
		fmt.Printf("[ws %s] warning: failed to find safe start point, using offset %d: %v\n", sessionID[:8], offset, err)
		safeOffset = offset
	}

	// Copy from safe offset to end
	tmpPath := logPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		srcFile.Close()
		tmux.StartPipePane(ctx, sess.TmuxSession, logPath)
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := srcFile.Seek(safeOffset, io.SeekStart); err != nil {
		srcFile.Close()
		tmpFile.Close()
		os.Remove(tmpPath)
		tmux.StartPipePane(ctx, sess.TmuxSession, logPath)
		return fmt.Errorf("failed to seek in log file: %w", err)
	}

	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		srcFile.Close()
		tmpFile.Close()
		os.Remove(tmpPath)
		tmux.StartPipePane(ctx, sess.TmuxSession, logPath)
		return fmt.Errorf("failed to copy log tail: %w", err)
	}

	srcFile.Close()
	tmpFile.Close()

	// 3. Replace original with temp file
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		os.Remove(tmpPath)
		tmux.StartPipePane(ctx, sess.TmuxSession, logPath)
		return fmt.Errorf("failed to remove original log file: %w", err)
	}

	if err := os.Rename(tmpPath, logPath); err != nil {
		tmux.StartPipePane(ctx, sess.TmuxSession, logPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// 4. Restart pipe-pane
	if err := tmux.StartPipePane(ctx, sess.TmuxSession, logPath); err != nil {
		return fmt.Errorf("failed to restart pipe-pane: %w", err)
	}

	newInfo, _ := os.Stat(logPath)
	newSize := int64(0)
	if newInfo != nil {
		newSize = newInfo.Size()
	}
	fmt.Printf("[ws %s] log rotation complete: %.2f MB -> %.2f MB\n",
		sessionID[:8], float64(fileSize)/(1024*1024), float64(newSize)/(1024*1024))

	return nil
}

// WSMessage represents a WebSocket message from the client
type WSMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// WSOutputMessage represents a WebSocket message to the client
type WSOutputMessage struct {
	Type    string `json:"type"` // "full", "append"
	Content string `json:"content"`
}

// handleTerminalWebSocket streams log file using byte-offset tracking.
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

	// Check if session is already dead before doing anything else
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
	if !s.session.IsRunning(ctx, sessionID) {
		cancel()
		http.Error(w, "session not running", http.StatusGone)
		return
	}
	cancel()

	// Check if this is a remote session
	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("session not found: %v", err), http.StatusNotFound)
		return
	}
	if sess.IsRemoteSession() {
		s.handleRemoteTerminalWebSocket(w, r, sess)
		return
	}

	// Get log file path
	logPath, err := s.session.GetLogPath(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get log path: %v", err), http.StatusInternalServerError)
		return
	}

	// Ensure log file exists (create empty if it was wiped)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		// Create empty log file so session can still connect
		if err := os.WriteFile(logPath, []byte{}, 0644); err != nil {
			http.Error(w, fmt.Sprintf("failed to create log file: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Acquire rotation lock for this session to prevent concurrent rotations
	rotationLock := s.getRotationLock(sessionID)
	rotationLock.Lock()

	// Check log file size and rotate if over threshold
	maxLogSize := s.config.GetXtermMaxLogSizeMB() * 1024 * 1024 // Convert MB to bytes
	if fileInfo, err := os.Stat(logPath); err == nil && fileInfo.Size() > maxLogSize {
		fmt.Printf("[ws %s] log file size %.2f MB exceeds threshold %.2f MB, rotating\n",
			sessionID[:8], float64(fileInfo.Size())/(1024*1024), float64(maxLogSize)/(1024*1024))

		// Disconnect existing connections for this session
		count := s.BroadcastToSession(sessionID, "reconnect", "Log rotated, please reconnect")
		if count > 0 {
			fmt.Printf("[ws %s] disconnected %d existing connection(s) for rotation\n", sessionID[:8], count)
		}

		// Rotate the log file
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
		if err := s.rotateLogFile(ctx, sessionID, logPath); err != nil {
			cancel()
			fmt.Printf("[ws %s] log rotation failed: %v\n", sessionID[:8], err)
			// Continue anyway - the session is still usable
		} else {
			cancel()
		}
	}

	// Release rotation lock before blocking on WebSocket upgrade
	rotationLock.Unlock()

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
	conn := &wsConn{conn: rawConn}

	// Register this connection
	s.RegisterWebSocket(sessionID, conn)
	defer func() {
		s.UnregisterWebSocket(sessionID, conn)
		conn.Close()
	}()

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

	// Get file size for log message
	fileInfo, _ := os.Stat(logPath)
	fileSize := int64(0)
	if fileInfo != nil {
		fileSize = fileInfo.Size()
	}

	// Find offset for last N lines to send on initial connect
	bootstrapLines := s.config.GetTerminalBootstrapLines()
	checkpointOffset, err := findLastNLinesOffset(logPath, bootstrapLines)
	if err != nil {
		fmt.Printf("[ws %s] failed to find checkpoint offset: %v\n", sessionID[:8], err)
		// Fall back to sending full file
		checkpointOffset = 0
	}
	if checkpointOffset > 0 {
		offset = checkpointOffset
		// Find a safer start point to avoid starting mid-escape-sequence
		safeOffset, err := findSafeStartPoint(logPath, offset, 4096)
		if err != nil {
			fmt.Printf("[ws %s] failed to find safe start point: %v\n", sessionID[:8], err)
		} else if safeOffset != offset {
			fmt.Printf("[ws %s] adjusted offset from %d to %d for safe start\n", sessionID[:8], offset, safeOffset)
			offset = safeOffset
		}
		bytesToSend := fileSize - offset
		fmt.Printf("[ws %s] bootstrap: sending last %d lines from offset %d (%.2f MB / %.2f MB total)\n",
			sessionID[:8], bootstrapLines, offset, float64(bytesToSend)/(1024*1024), float64(fileSize)/(1024*1024))
	}

	// Extract ANSI sequences from content BEFORE the bootstrapped content to prime terminal state
	// This helps with colors and formatting after bootstrapping
	// Uses deduplication (last seen wins) to minimize data transfer
	// Only extracts from file beginning up to the bootstrap offset to avoid duplication
	ansiSequences, err := extractANSISequences(logPath, offset)
	if err != nil {
		fmt.Printf("[ws %s] failed to extract ANSI sequences: %v\n", sessionID[:8], err)
	} else if len(ansiSequences) > 0 {
		fmt.Printf("[ws %s] extracted %d unique ANSI sequences from pre-bootstrap content (offset 0-%d, %.2f KB)\n",
			sessionID[:8], len(ansiSequences), offset, float64(len(ansiSequences))/1024)
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
			// Prepend ANSI sequences for terminal state priming (only on first full send)
			content := string(ansiSequences) + string(data)
			if err := sendOutput("full", content); err != nil {
				return err
			}
			offset += int64(len(data))
			// Clear ansiSequences after first use so we don't send it again
			ansiSequences = []byte{}
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
			if !s.session.IsRunning(ctx, sessionID) {
				cancel()
				sendOutput("append", "\n[Session ended]")
				return
			}
			cancel()
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
				sess, err := s.session.GetSession(sessionID)
				if err != nil {
					break
				}
				// Clear nudge on enter, tab, or shift-tab
				if sess.Nudge != "" && (strings.Contains(msg.Data, "\r") || strings.Contains(msg.Data, "\t") || strings.Contains(msg.Data, "\x1b[Z")) {
					sess.Nudge = ""
					if err := s.state.UpdateSession(*sess); err != nil {
						fmt.Printf("[nudgenik] error clearing nudge: %v\n", err)
					} else if err := s.state.Save(); err != nil {
						fmt.Printf("[nudgenik] error saving nudge clear: %v\n", err)
					} else {
						// Broadcast update to WebSocket clients
						go s.BroadcastSessions()
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
				if err := tmux.SendKeys(ctx, sess.TmuxSession, msg.Data); err != nil {
					cancel()
					fmt.Printf("[nudgenik] error sending keys to tmux: %v\n", err)
				}
				cancel()
			case "resize":
				sess, err := s.session.GetSession(sessionID)
				if err != nil {
					break
				}
				// Parse resize data: {"cols": 120, "rows": 40}
				var resizeData struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal([]byte(msg.Data), &resizeData); err != nil {
					fmt.Printf("[terminal] error parsing resize data: %v\n", err)
					break
				}
				if resizeData.Cols <= 0 || resizeData.Rows <= 0 {
					break
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
				if err := tmux.ResizeWindow(ctx, sess.TmuxSession, resizeData.Cols, resizeData.Rows); err != nil {
					cancel()
					fmt.Printf("[terminal] error resizing tmux window: %v\n", err)
				}
				cancel()
			}
		}
	}
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

				// Clear nudge on enter, tab, or shift-tab
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
