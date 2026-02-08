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

	"github.com/creack/pty"
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
	sessionID   string
	tmuxSession string
	state       state.StateStore

	mu        sync.RWMutex
	clientCh  chan []byte
	ptmx      *os.File
	attachCmd *exec.Cmd
	lastEvent time.Time

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
func NewSessionTracker(sessionID, tmuxSession string, st state.StateStore) *SessionTracker {
	return &SessionTracker{
		sessionID:   sessionID,
		tmuxSession: tmuxSession,
		state:       st,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
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

	attachCmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", "="+target)
	ptmx, err := pty.Start(attachCmd)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.ptmx = ptmx
	t.attachCmd = attachCmd
	t.mu.Unlock()

	defer t.closePTY()

	buf := make([]byte, 8192)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
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

		if err != nil {
			return err
		}

		select {
		case <-t.stopCh:
			return io.EOF
		default:
		}
	}
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
