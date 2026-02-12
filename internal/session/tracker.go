package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

const trackerRestartDelay = 500 * time.Millisecond
const trackerActivityDebounce = 500 * time.Millisecond
const trackerRetryLogInterval = 15 * time.Second

var trackerIgnorePrefixes = [][]byte{
	[]byte("\x1b[?"),
	[]byte("\x1b[>"),
	[]byte("\x1b]10;"),
	[]byte("\x1b]11;"),
}

// SessionTracker maintains a long-lived PTY attachment for a tmux session.
// It tracks output activity and forwards terminal output to one active websocket client.
type SessionTracker struct {
	sessionID      string
	tmuxSession    string
	state          state.StateStore
	signalDetector *signal.SignalDetector

	mu               sync.RWMutex
	clientCh         chan []byte
	ptmx             *os.File
	attachCmd        *exec.Cmd
	lastEvent        time.Time
	scrollbackParsed bool // true after first scrollback parse (prevents re-parsing on PTY reconnect)

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}

	lastRetryLog time.Time
}

// IsAttached reports whether the tracker currently has an active PTY attachment.
func (t *SessionTracker) IsAttached() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ptmx != nil
}

// NewSessionTracker creates a tracker for a session.
func NewSessionTracker(sessionID, tmuxSession string, st state.StateStore, signalCallback func(signal.Signal)) *SessionTracker {
	t := &SessionTracker{
		sessionID:   sessionID,
		tmuxSession: tmuxSession,
		state:       st,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
	if signalCallback != nil {
		t.signalDetector = signal.NewSignalDetector(sessionID, signalCallback)
		t.signalDetector.SetNearMissCallback(func(line string) {
			fmt.Printf("[signal] %s - potential missed signal: %q\n", signal.ShortID(sessionID), line)
		})
	}
	return t
}

// Start launches the tracker loop in a background goroutine.
func (t *SessionTracker) Start() {
	go t.run()
}

// Stop terminates the tracker and closes the active websocket output channel.
func (t *SessionTracker) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
		t.closePTY()
		<-t.doneCh
	})
}

// SetTmuxSession updates the target tmux session name.
func (t *SessionTracker) SetTmuxSession(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tmuxSession = name
}

// AttachWebSocket registers the active websocket stream and returns its output channel.
// If a client is already attached, it is replaced and its channel is closed.
func (t *SessionTracker) AttachWebSocket() chan []byte {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.clientCh != nil {
		close(t.clientCh)
	}
	t.clientCh = make(chan []byte, 64)
	return t.clientCh
}

// DetachWebSocket clears the websocket stream if it matches the currently registered one.
func (t *SessionTracker) DetachWebSocket(ch chan []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.clientCh == ch {
		close(t.clientCh)
		t.clientCh = nil
	}
}

// SendInput writes terminal input bytes to the tracker PTY.
func (t *SessionTracker) SendInput(data string) error {
	ptmx := t.currentPTY()
	if ptmx == nil {
		deadline := time.Now().Add(2 * time.Second)
		for ptmx == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			ptmx = t.currentPTY()
		}
	}
	if ptmx == nil {
		return fmt.Errorf("terminal not attached")
	}
	_, err := io.WriteString(ptmx, data)
	return err
}

func (t *SessionTracker) currentPTY() *os.File {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ptmx
}

// Resize updates the tracker PTY dimensions.
func (t *SessionTracker) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid size %dx%d", cols, rows)
	}

	t.mu.RLock()
	ptmx := t.ptmx
	t.mu.RUnlock()
	if ptmx == nil {
		return fmt.Errorf("terminal not attached")
	}

	return pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (t *SessionTracker) run() {
	defer close(t.doneCh)

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		if err := t.attachAndRead(); err != nil && err != io.EOF {
			now := time.Now()
			if t.shouldLogRetry(now) {
				fmt.Printf("[tracker] %s attach/read failed: %v\n", t.sessionID, err)
			}
		}

		// Flush any buffered signal data on disconnect
		if t.signalDetector != nil {
			t.signalDetector.Flush()
		}

		if t.waitOrStop(trackerRestartDelay) {
			return
		}
	}
}

func (t *SessionTracker) attachAndRead() error {
	t.mu.RLock()
	target := t.tmuxSession
	t.mu.RUnlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !tmux.SessionExists(ctx, target) {
		return fmt.Errorf("tmux session does not exist: %s", target)
	}

	// Query tmux window size to initialize PTY with correct dimensions.
	// Retry a few times to handle a timing condition where a freshly spawned
	// session hasn't fully initialized its window yet.
	width, height, err := t.getWindowSizeWithRetry(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to get window size: %w", err)
	}

	// Parse scrollback for signals missed while daemon was down (first attach only).
	// Suppress the callback during scrollback parsing — signals detected here are
	// from before the daemon restart. The agent's current state is already persisted
	// in sess.Nudge, so re-firing would cause duplicate notifications.
	if t.signalDetector != nil && !t.scrollbackParsed {
		t.scrollbackParsed = true
		t.signalDetector.Suppress(true)
		capCtx, capCancel := context.WithTimeout(ctx, 2*time.Second)
		scrollback, capErr := tmux.CaptureLastLines(capCtx, target, 200, false)
		capCancel()
		if capErr == nil && scrollback != "" {
			t.signalDetector.Feed([]byte(scrollback))
			t.signalDetector.Flush()
		}
		t.signalDetector.Suppress(false)
	}

	attachCmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", "="+target)
	ptmx, err := pty.StartWithSize(attachCmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.ptmx = ptmx
	t.attachCmd = attachCmd
	t.mu.Unlock()

	defer t.closePTY()

	buf := make([]byte, 8192)
	var pending []byte // Holds incomplete UTF-8 sequence from previous read

	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			// Prepend any pending bytes from previous read
			var data []byte
			if len(pending) > 0 {
				data = make([]byte, len(pending)+n)
				copy(data, pending)
				copy(data[len(pending):], buf[:n])
				pending = nil
			} else {
				data = buf[:n]
			}

			// Find the last valid UTF-8 boundary
			validLen := findValidUTF8Boundary(data)

			// Keep incomplete sequence for next read
			if validLen < len(data) {
				pending = make([]byte, len(data)-validLen)
				copy(pending, data[validLen:])
				data = data[:validLen]
			}

			// Only process if we have complete UTF-8 sequences
			if len(data) > 0 {
				chunk := make([]byte, len(data))
				copy(chunk, data)

				// Feed to signal detector (line accumulation handles chunk splits)
				if t.signalDetector != nil {
					t.signalDetector.Feed(chunk)
				}

				meaningful := isMeaningfulTerminalChunk(chunk)
				now := time.Now()

				t.mu.Lock()
				shouldUpdate := meaningful && (t.lastEvent.IsZero() || now.Sub(t.lastEvent) >= trackerActivityDebounce)
				if shouldUpdate {
					t.lastEvent = now
				}
				clientCh := t.clientCh
				t.mu.Unlock()

				if shouldUpdate {
					t.state.UpdateSessionLastOutput(t.sessionID, now)
				}
				if clientCh != nil {
					select {
					case clientCh <- chunk:
					default:
					}
				}
			}
		}

		if err != nil {
			// Flush any remaining pending bytes on error/EOF
			if len(pending) > 0 {
				t.mu.RLock()
				clientCh := t.clientCh
				t.mu.RUnlock()
				if clientCh != nil {
					select {
					case clientCh <- pending:
					default:
					}
				}
			}
			return err
		}

		select {
		case <-t.stopCh:
			if t.signalDetector != nil {
				t.signalDetector.Flush()
			}
			return io.EOF
		default:
			if t.signalDetector != nil && t.signalDetector.ShouldFlush() {
				t.signalDetector.Flush()
			}
		}
	}
}

// getWindowSizeWithRetry retries GetWindowSize to handle timing issues with freshly spawned sessions.
func (t *SessionTracker) getWindowSizeWithRetry(ctx context.Context, target string) (width, height int, err error) {
	const maxAttempts = 10
	const retryDelay = 100 * time.Millisecond

	for attempt := 0; attempt < maxAttempts; attempt++ {
		width, height, err = tmux.GetWindowSize(ctx, target)
		if err == nil {
			return width, height, nil
		}

		// Check if we should stop retrying
		select {
		case <-t.stopCh:
			return 0, 0, fmt.Errorf("tracker stopped while waiting for session ready")
		case <-ctx.Done():
			return 0, 0, fmt.Errorf("context cancelled while waiting for session ready")
		default:
		}

		// Don't sleep on the last attempt
		if attempt < maxAttempts-1 {
			time.Sleep(retryDelay)
		}
	}

	return 0, 0, fmt.Errorf("session window not ready after %d attempts: %w", maxAttempts, err)
}

// findValidUTF8Boundary returns the length of data up to the last complete UTF-8 character.
// If data ends mid-character, those trailing bytes are excluded.
func findValidUTF8Boundary(data []byte) int {
	if len(data) == 0 {
		return 0
	}

	// If the entire slice is valid UTF-8, return its full length
	if utf8.Valid(data) {
		return len(data)
	}

	// Find where the incomplete sequence starts by checking trailing bytes.
	// UTF-8 continuation bytes are 10xxxxxx (0x80-0xBF).
	// A leading byte indicates the start of a multi-byte sequence.
	//
	// Walk backwards to find the start of an incomplete sequence:
	// - If we find a leading byte, check if enough bytes follow for a complete character
	// - The leading byte pattern tells us how many bytes the character needs

	for i := len(data) - 1; i >= 0 && i >= len(data)-4; i-- {
		b := data[i]

		// Check if this is a leading byte (not a continuation byte)
		if b&0xC0 != 0x80 {
			// This is either ASCII (0xxxxxxx) or a leading byte (11xxxxxx)
			if b < 0x80 {
				// ASCII byte - everything up to and including this is valid
				return i + 1
			}

			// It's a leading byte - determine expected sequence length
			var seqLen int
			switch {
			case b&0xE0 == 0xC0:
				seqLen = 2 // 110xxxxx
			case b&0xF0 == 0xE0:
				seqLen = 3 // 1110xxxx
			case b&0xF8 == 0xF0:
				seqLen = 4 // 11110xxx
			default:
				// Invalid leading byte, skip it
				continue
			}

			// Check if we have enough bytes for this sequence
			remaining := len(data) - i
			if remaining >= seqLen {
				// Sequence is complete, include it
				return i + seqLen
			}
			// Sequence is incomplete, exclude it
			return i
		}
	}

	// If we get here, we only have continuation bytes (shouldn't happen in valid streams)
	// Return 0 to buffer everything
	return 0
}

func (t *SessionTracker) shouldLogRetry(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastRetryLog.IsZero() || now.Sub(t.lastRetryLog) >= trackerRetryLogInterval {
		t.lastRetryLog = now
		return true
	}
	return false
}

func isMeaningfulTerminalChunk(chunk []byte) bool {
	for _, prefix := range trackerIgnorePrefixes {
		if bytes.HasPrefix(chunk, prefix) {
			return false
		}
	}

	clean := stripTerminalControl(chunk)
	if len(clean) == 0 {
		return false
	}
	for _, r := range string(clean) {
		if unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func stripTerminalControl(data []byte) []byte {
	const (
		stNormal = iota
		stEsc
		stCSI
		stOSC
		stDCS
	)

	out := make([]byte, 0, len(data))
	state := stNormal
	oscEsc := false
	dcsEsc := false

	for _, b := range data {
		switch state {
		case stNormal:
			if b == 0x1b {
				state = stEsc
				continue
			}
			if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
				continue
			}
			if b == 0x7f {
				continue
			}
			out = append(out, b)
		case stEsc:
			switch b {
			case '[':
				state = stCSI
			case ']':
				state = stOSC
				oscEsc = false
			case 'P':
				state = stDCS
				dcsEsc = false
			default:
				state = stNormal
			}
		case stCSI:
			if b >= 0x40 && b <= 0x7e {
				state = stNormal
			}
		case stOSC:
			if oscEsc {
				if b == '\\' {
					state = stNormal
				}
				oscEsc = false
				continue
			}
			if b == 0x07 {
				state = stNormal
				continue
			}
			oscEsc = b == 0x1b
		case stDCS:
			if dcsEsc {
				if b == '\\' {
					state = stNormal
				}
				dcsEsc = false
				continue
			}
			dcsEsc = b == 0x1b
		}
	}

	return out
}

func (t *SessionTracker) closePTY() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ptmx != nil {
		_ = t.ptmx.Close()
		t.ptmx = nil
	}
	if t.attachCmd != nil {
		if t.attachCmd.Process != nil {
			_ = t.attachCmd.Process.Kill()
		}
		_ = t.attachCmd.Wait()
		t.attachCmd = nil
	}
}

func (t *SessionTracker) waitOrStop(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return false
	case <-t.stopCh:
		return true
	}
}
