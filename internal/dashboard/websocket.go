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
	"github.com/sergek/schmux/internal/tmux"
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

// extractANSISequences scans the file and extracts all ANSI CSI sequences
// to prime terminal state before sending bootstrapped content.
// Uses "last seen wins" deduplication to minimize data transfer.
func extractANSISequences(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Read the entire file (could be optimized with streaming)
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

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

	// Check if session is already dead before doing anything else
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetTmuxQueryTimeoutSeconds())*time.Second)
	if !s.session.IsRunning(ctx, sessionID) {
		cancel()
		http.Error(w, "session not running", http.StatusGone)
		return
	}
	cancel()

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

	// Extract ANSI sequences from full file to prime terminal state
	// This helps with colors and formatting after bootstrapping
	// Uses deduplication (last seen wins) to minimize data transfer
	ansiSequences, err := extractANSISequences(logPath)
	if err != nil {
		fmt.Printf("[ws %s] failed to extract ANSI sequences: %v\n", sessionID[:8], err)
	} else if len(ansiSequences) > 0 {
		fmt.Printf("[ws %s] extracted %d unique ANSI sequences for state priming (%.2f MB)\n",
			sessionID[:8], len(ansiSequences), float64(len(ansiSequences))/(1024*1024))
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
			offset = int64(len(data))
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetTmuxQueryTimeoutSeconds())*time.Second)
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
						fmt.Printf("Error clearing nudge: %v\n", err)
					} else if err := s.state.Save(); err != nil {
						fmt.Printf("Error saving nudge clear: %v\n", err)
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetTmuxOperationTimeoutSeconds())*time.Second)
				if err := tmux.SendKeys(ctx, sess.TmuxSession, msg.Data); err != nil {
					cancel()
					fmt.Printf("Error sending keys to tmux: %v\n", err)
				}
				cancel()
			}
		}
	}
}
