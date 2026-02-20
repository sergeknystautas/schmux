# Control Mode Terminal Streaming — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace the PTY-attachment terminal streaming model with tmux control mode to eliminate scrollback gaps during high-throughput agent output, switch to binary WebSocket frames, and support multiple concurrent viewers.

**Architecture:** The `SessionTracker` drops its PTY (`tmux attach-session` via `creack/pty`) and uses `tmux -CC attach-session` via the existing `controlmode.Client`/`controlmode.Parser` infrastructure. Output flows as `%output` events (every byte, no screen-snapshot loss) through a per-subscriber fan-out. Input goes through `controlmode.Client.SendKeys()` (pipe write, no process spawn). The WebSocket transport switches from JSON text frames to raw binary frames. Multiple browser clients can connect to the same session simultaneously.

**Tech Stack:** Go (tmux control mode protocol), gorilla/websocket (binary frames), xterm.js (ArrayBuffer), existing `internal/remote/controlmode/` package

**Design Spec:** `docs/specs/control-mode-streaming.md`

---

### Task 1: Increase scrollback configuration

Zero-risk change that provides immediate benefit on reconnect. Applies independently of the control mode migration.

**Files:**

- Modify: `internal/tmux/tmux.go:50-66` (CreateSession)
- Modify: `internal/dashboard/websocket.go:20` (bootstrapCaptureLines)
- Modify: `assets/dashboard/src/lib/terminalStream.ts:146` (scrollback)

**Step 1: Set tmux history-limit on session creation**

In `internal/tmux/tmux.go`, modify `CreateSession` to set `history-limit` after creating the session:

```go
func CreateSession(ctx context.Context, name, dir, command string) error {
	args := []string{
		"new-session",
		"-d",       // detached
		"-s", name, // session name
		"-c", dir,  // working directory
		command,    // command to run
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %w: %s", err, string(output))
	}

	// Set scrollback to 10000 lines (tmux default is 2000)
	if err := SetOption(ctx, name, "history-limit", "10000"); err != nil {
		fmt.Printf("[tmux] warning: failed to set history-limit for %s: %v\n", name, err)
	}

	return nil
}
```

**Step 2: Increase bootstrap capture lines**

In `internal/dashboard/websocket.go`, change line 20:

```go
const bootstrapCaptureLines = 5000
```

**Step 3: Increase xterm.js scrollback**

In `assets/dashboard/src/lib/terminalStream.ts`, change line 146:

```typescript
scrollback: 5000,
```

**Step 4: Run tests**

```bash
go test ./internal/tmux/ ./internal/dashboard/ ./internal/session/
```

Expected: all pass.

**Step 5: Manual test**

Start schmux, spawn a session, run `seq 1 2000`, scroll up — verify you can see all 2000 lines. Reconnect the WebSocket (refresh the page) — verify the bootstrap fills in scrollback.

**Step 6: Commit**

```bash
git commit -m "feat: increase scrollback to 10000/5000 lines for tmux, bootstrap, and xterm.js"
```

---

### Task 2: Refactor SessionTracker to use control mode for output

This is the core change. Replace the PTY read loop with control mode `%output` event streaming.

**Files:**

- Modify: `internal/session/tracker.go` (major rewrite)
- Modify: `internal/session/tracker_test.go` (update tests)

**Step 1: Update imports**

Replace the tracker imports. Remove `creack/pty`, `unicode`, `unicode/utf8`, `bytes`, `os`. Add `controlmode` and `io`:

```go
import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/state"
)
```

**Step 2: Replace the struct fields**

Replace the PTY-related fields with control mode fields:

```go
type SessionTracker struct {
	sessionID      string
	tmuxSession    string
	paneID         string
	state          state.StateStore
	fileWatcher    *signal.FileWatcher
	outputCallback func([]byte)

	mu        sync.RWMutex
	cmClient  *controlmode.Client
	cmParser  *controlmode.Parser
	cmCmd     *exec.Cmd
	cmStdin   io.WriteCloser
	lastEvent time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}

	lastRetryLog time.Time
}
```

**Step 3: Update IsAttached**

```go
func (t *SessionTracker) IsAttached() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cmClient != nil
}
```

**Step 4: Replace AttachWebSocket/DetachWebSocket with SubscribeOutput/UnsubscribeOutput**

Remove `AttachWebSocket()` and `DetachWebSocket()`. Add delegation methods:

```go
// SubscribeOutput returns a channel that receives output events for this session's pane.
// Multiple subscribers are supported — each gets an independent buffered channel.
func (t *SessionTracker) SubscribeOutput() <-chan controlmode.OutputEvent {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		ch := make(chan controlmode.OutputEvent)
		close(ch)
		return ch
	}
	return client.SubscribeOutput(paneID)
}

// UnsubscribeOutput removes an output subscription.
func (t *SessionTracker) UnsubscribeOutput(ch <-chan controlmode.OutputEvent) {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client != nil {
		client.UnsubscribeOutput(paneID, ch)
	}
}
```

**Step 5: Replace SendInput**

```go
func (t *SessionTracker) SendInput(data string) error {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.SendKeys(ctx, paneID, data)
}
```

**Step 6: Replace Resize**

```go
func (t *SessionTracker) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid size %dx%d", cols, rows)
	}
	t.mu.RLock()
	client := t.cmClient
	t.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// ResizeWindow takes a window ID, but for single-pane sessions
	// we can target the pane's window via the pane ID
	return client.ResizeWindow(ctx, t.paneID, cols, rows)
}
```

**Step 7: Add CaptureLastLines delegation**

```go
// CaptureLastLines captures scrollback from tmux via control mode.
func (t *SessionTracker) CaptureLastLines(ctx context.Context, lines int) (string, error) {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		return "", fmt.Errorf("not attached")
	}
	return client.CapturePaneLines(ctx, paneID, lines)
}
```

**Step 8: Replace attachAndRead with attachControlMode**

```go
func (t *SessionTracker) attachControlMode() error {
	t.mu.RLock()
	target := t.tmuxSession
	t.mu.RUnlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start tmux in control mode (double -C disables echo)
	cmd := exec.CommandContext(ctx, "tmux", "-CC", "attach-session", "-t", "="+target)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to start control mode: %w", err)
	}

	// Create parser and client
	parser := controlmode.NewParser(stdout, t.sessionID)
	client := controlmode.NewClient(stdin, parser)
	client.Start()

	// Wait for control mode to be ready
	readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
	select {
	case <-parser.ControlModeReady():
		readyCancel()
	case <-readyCtx.Done():
		readyCancel()
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("control mode not ready within timeout")
	}

	// Verify control mode is responsive
	if err := client.WaitForReady(ctx, 5*time.Second); err != nil {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("control mode not responsive: %w", err)
	}

	// Discover pane ID
	paneID, err := t.discoverPaneID(ctx, client)
	if err != nil {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("failed to discover pane ID: %w", err)
	}

	// Store references
	t.mu.Lock()
	t.cmClient = client
	t.cmParser = parser
	t.cmCmd = cmd
	t.cmStdin = stdin
	t.paneID = paneID
	t.mu.Unlock()

	defer t.closeControlMode()

	// Subscribe to output for the outputCallback (preview autodetect, activity tracking)
	if t.outputCallback != nil {
		outputCh := client.SubscribeOutput(paneID)
		defer client.UnsubscribeOutput(paneID, outputCh)

		for {
			select {
			case event, ok := <-outputCh:
				if !ok {
					return io.EOF
				}
				chunk := []byte(event.Data)

				// Activity tracking (same debounce as before)
				now := time.Now()
				shouldUpdate := t.lastEvent.IsZero() || now.Sub(t.lastEvent) >= trackerActivityDebounce
				if shouldUpdate {
					t.lastEvent = now
					if t.state != nil {
						t.state.UpdateSessionLastOutput(t.sessionID, now)
					}
				}

				t.outputCallback(chunk)

			case <-t.stopCh:
				return io.EOF
			}
		}
	}

	// No output callback — just wait for stop
	<-t.stopCh
	return io.EOF
}

func (t *SessionTracker) discoverPaneID(ctx context.Context, client *controlmode.Client) (string, error) {
	output, err := client.Execute(ctx, "list-panes -F '#{pane_id}'")
	if err != nil {
		return "", err
	}
	paneID := strings.TrimSpace(output)
	if paneID == "" {
		return "", fmt.Errorf("no pane found")
	}
	// Return first pane if multiple
	if idx := strings.Index(paneID, "\n"); idx > 0 {
		paneID = paneID[:idx]
	}
	return paneID, nil
}
```

Note: add `"strings"` to the imports for `discoverPaneID`.

**Step 9: Replace closeControlMode**

```go
func (t *SessionTracker) closeControlMode() {
	t.mu.Lock()
	stdin := t.cmStdin
	cmd := t.cmCmd
	client := t.cmClient
	t.cmClient = nil
	t.cmParser = nil
	t.cmCmd = nil
	t.cmStdin = nil
	t.mu.Unlock()

	if client != nil {
		client.Close()
	}
	if stdin != nil {
		stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
	}
}
```

**Step 10: Update run() to call attachControlMode**

```go
func (t *SessionTracker) run() {
	defer close(t.doneCh)

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		if err := t.attachControlMode(); err != nil && err != io.EOF {
			now := time.Now()
			if t.shouldLogRetry(now) {
				fmt.Printf("[tracker] %s control mode failed: %v\n", t.sessionID, err)
			}
		}

		if t.waitOrStop(trackerRestartDelay) {
			return
		}
	}
}
```

**Step 11: Update Stop()**

```go
func (t *SessionTracker) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
		t.closeControlMode()
		if t.fileWatcher != nil {
			t.fileWatcher.Close()
		}
		<-t.doneCh
	})
}
```

**Step 12: Remove dead code**

Delete the following functions that are no longer needed:

- `findValidUTF8Boundary()`
- `isMeaningfulTerminalChunk()`
- `getWindowSizeWithRetry()`
- `currentPTY()`
- `closePTY()`
- `AttachWebSocket()`
- `DetachWebSocket()`
- The `trackerIgnorePrefixes` variable
- The drop tracking fields (`droppedBytes`, `droppedCount`, `lastDropLog`) — drops now happen in the controlmode fan-out which has its own logging

**Step 13: Update tracker tests**

In `internal/session/tracker_test.go`:

- `TestSessionTrackerAttachDetach` — this tests `AttachWebSocket`/`DetachWebSocket` which no longer exist. Remove this test.
- `TestSessionTrackerInputResizeWithoutPTY` — update to test `SendInput`/`Resize` return errors when `cmClient` is nil.
- `TestFindValidUTF8Boundary` — remove (function deleted).
- `TestIsMeaningfulTerminalChunk` — remove (function deleted).
- `TestSendInputFallbackComment` — update to test that `SendInput` returns error when not attached.

```go
func TestSessionTrackerInputResizeWithoutControlMode(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil)

	if err := tracker.SendInput("abc"); err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
	err := tracker.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
}

func TestSubscribeOutputWithoutControlMode(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil)

	ch := tracker.SubscribeOutput()
	// Should return a closed channel when not attached
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel when not attached")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected channel to be closed immediately")
	}
}
```

**Step 14: Run tests**

```bash
go test ./internal/session/ -v -count=1
```

Expected: all pass.

**Step 15: Commit**

```bash
git commit -m "refactor(tracker): replace PTY attachment with tmux control mode for output streaming"
```

---

### Task 3: Switch WebSocket transport to binary frames

Replace JSON text frames (server→client) with raw binary frames. Keep JSON for client→server input/resize messages.

**Files:**

- Modify: `internal/dashboard/websocket.go` (handleTerminalWebSocket)
- Modify: `assets/dashboard/src/lib/terminalStream.ts` (handleOutput, connect)

**Step 1: Update handleTerminalWebSocket**

Rewrite the handler to use the new tracker API and binary frames. The full function replacement:

```go
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

	// Subscribe to output — multiple clients can subscribe simultaneously
	outputCh := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(outputCh)

	// Wait for tracker to attach
	attachDeadline := time.Now().Add(2 * time.Second)
	for !tracker.IsAttached() && time.Now().Before(attachDeadline) {
		time.Sleep(25 * time.Millisecond)
	}

	// Bootstrap with scrollback — send as binary frame
	capCtx, capCancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	bootstrap, err := tracker.CaptureLastLines(capCtx, bootstrapCaptureLines)
	capCancel()
	if err != nil {
		fmt.Printf("[ws %s] bootstrap capture failed: %v\n", sessionID[:8], err)
		bootstrap = ""
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(bootstrap)); err != nil {
		return
	}

	// Flush any output that arrived during bootstrap
	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				return
			}
			if len(event.Data) > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, []byte(event.Data)); err != nil {
					return
				}
			}
		default:
			goto drained
		}
	}
drained:

	// Read client messages (input, resize)
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

	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(1000, "session ended"))
				return
			}
			if len(event.Data) > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, []byte(event.Data)); err != nil {
					return
				}
			}
		case <-sessionDead:
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
								fmt.Printf("[nudgenik] error saving nudge clear: %v\n", err)
							} else {
								s.BroadcastSessions()
							}
						}()
					}
				}
				if err := tracker.SendInput(msg.Data); err != nil {
					fmt.Printf("[terminal] error sending input: %v\n", err)
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
					fmt.Printf("[terminal] error resizing: %v\n", err)
				}
			}
		}
	}
}
```

**Step 2: Remove dead code from websocket.go**

Delete:

- `WSOutputMessage` struct (lines 68-71) — no longer used for local sessions (keep if CR handler still uses it, otherwise delete)
- `filterMouseMode()` function and `filterSequences` variable
- `bootstrapCaptureLines` can stay but update the value (already done in Task 1)

Keep:

- `WSMessage` struct — still used for client→server
- `isTerminalResponse()` — still filters input
- `inputFilterPrefixes` — still used

**Step 3: Update terminalStream.ts**

Replace the `connect()` method's WebSocket setup:

```typescript
connect() {
    if (!this.terminal || this.disposed) return;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/terminal/${this.sessionId}`;

    this.ws = new WebSocket(wsUrl);
    this.ws.binaryType = 'arraybuffer';

    this.ws.onopen = () => {
      this.connected = true;
      this.reconnectAttempt = 0;
      this.terminal.reset();
      this.onStatusChange('connected');

      if (this.tmuxCols && this.tmuxRows) {
        this.sendResize(this.tmuxCols, this.tmuxRows);
      }
    };

    this.ws.onmessage = (event) => {
      if (!this.terminal) return;
      const data = new TextDecoder().decode(event.data as ArrayBuffer);
      inputLatency.markReceived();
      const renderStart = performance.now();
      this.terminal.write(data);
      if (this.followTail) {
        this.terminal.scrollToBottom();
      }
      inputLatency.markRenderTime(performance.now() - renderStart);
    };

    this.ws.onclose = (event) => {
      this.connected = false;
      if (this.disposed) return;

      if (event.code === 1000) {
        // Normal close — session ended
        if (this.terminal) {
          this.terminal.writeln('\r\n\x1b[90m[Session ended]\x1b[0m');
        }
        this.onStatusChange('disconnected');
        return;
      }

      this.onStatusChange('disconnected');
      if (this.reconnectAttempt < this.maxReconnectAttempt) {
        const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempt), 30000);
        this.reconnectAttempt++;
        if (this.terminal) {
          this.terminal.writeln(
            `\r\n\x1b[33m[Connection lost, reconnecting in ${delay / 1000}s...]\x1b[0m`
          );
        }
        this.reconnectTimer = setTimeout(() => {
          this.connect();
        }, delay);
      } else {
        if (this.terminal) {
          this.terminal.writeln('\r\n\x1b[31m[Connection lost. Refresh to reconnect.]\x1b[0m');
        }
      }
    };

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error);
      this.onStatusChange('error');
    };
}
```

**Step 4: Remove dead code from terminalStream.ts**

Delete:

- `handleOutput()` method — replaced by inline `onmessage` handler
- `TerminalOutputMessage` type (lines 14-17)
- `wasDisplaced` field and all displacement handling

**Step 5: Run tests**

```bash
go test ./internal/dashboard/ ./internal/session/ -v -count=1
```

For frontend:

```bash
cd assets/dashboard && npx vitest run
```

Expected: all pass.

**Step 6: Commit**

```bash
git commit -m "feat: switch to binary WebSocket frames, remove JSON terminal transport"
```

---

### Task 4: Multi-client WebSocket support

Change WebSocket registration from single-client-with-displacement to multi-client.

**Files:**

- Modify: `internal/dashboard/server.go:108-109` (wsConns type)
- Modify: `internal/dashboard/server.go:741-764` (Register/Unregister)

**Step 1: Change wsConns map type**

In `server.go`, change the field (line 108):

```go
wsConns   map[string][]*wsConn
```

**Step 2: Update initialization**

Find where `wsConns` is initialized in the Server constructor and change:

```go
wsConns: make(map[string][]*wsConn),
```

**Step 3: Update RegisterWebSocket**

```go
func (s *Server) RegisterWebSocket(sessionID string, conn *wsConn) {
	s.wsConnsMu.Lock()
	defer s.wsConnsMu.Unlock()
	s.wsConns[sessionID] = append(s.wsConns[sessionID], conn)
}
```

**Step 4: Update UnregisterWebSocket**

```go
func (s *Server) UnregisterWebSocket(sessionID string, conn *wsConn) {
	s.wsConnsMu.Lock()
	defer s.wsConnsMu.Unlock()
	conns := s.wsConns[sessionID]
	for i, c := range conns {
		if c == conn {
			s.wsConns[sessionID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(s.wsConns[sessionID]) == 0 {
		delete(s.wsConns, sessionID)
	}
}
```

**Step 5: Find and update any code that reads from wsConns**

Search for all uses of `s.wsConns[` in `server.go` — any code that assumed a single `*wsConn` value needs to handle a slice. Common patterns:

- Checking if a session has active WebSocket connections: `len(s.wsConns[sessionID]) > 0`
- Sending to a specific session's connections: iterate the slice

**Step 6: Run tests**

```bash
go test ./internal/dashboard/ -v -count=1
```

Expected: all pass.

**Step 7: Manual test**

Open the same session in two browser tabs. Verify both receive output. Type in one tab — verify output appears in both. Close one tab — verify the other continues working.

**Step 8: Commit**

```bash
git commit -m "feat: support multiple concurrent WebSocket clients per session"
```

---

### Task 5: Clean up dead code and update CR handler

Remove code that is no longer referenced after the migration.

**Files:**

- Modify: `internal/dashboard/websocket.go` (CR handler still uses old patterns)
- Modify: `internal/session/tracker.go` (final cleanup)

**Step 1: Update handleCRTerminalWebSocket**

The conflict resolution handler (`handleCRTerminalWebSocket`, websocket.go:338-404) still uses the old `AttachWebSocket`/`DetachWebSocket` API and JSON framing. Update it to use the new control mode API and binary frames, following the same pattern as the updated `handleTerminalWebSocket`.

**Step 2: Remove WSOutputMessage if no longer used**

If no remaining code references `WSOutputMessage`, delete it.

**Step 3: Remove filterMouseMode and filterSequences**

Delete `filterSequences` and `filterMouseMode()` — control mode output doesn't include tmux client rendering commands.

**Step 4: Run full test suite**

```bash
./test.sh
```

Expected: all pass.

**Step 5: Build dashboard**

```bash
go run ./cmd/build-dashboard
```

Expected: builds successfully.

**Step 6: Build binary**

```bash
go build ./cmd/schmux
```

Expected: compiles with no errors.

**Step 7: Commit**

```bash
git commit -m "chore: remove PTY attachment dead code, update CR handler to control mode"
```

---

### Task 6: End-to-end manual verification

Run through the full verification plan from the design spec.

**Step 1: Start schmux**

```bash
./schmux start
```

**Step 2: Scrollback integrity**

Spawn a session, run `seq 1 2000`. Scroll up in the web terminal. Verify all 2000 lines are present with no gaps.

**Step 3: Input latency**

Type commands in the web terminal. Verify no perceptible delay. Try rapid typing.

**Step 4: Special keys**

Test: Enter, Tab, Backspace, arrow keys, Ctrl-C, Ctrl-D, Escape, Shift-Tab.

**Step 5: Resize**

Resize the browser window. Verify the terminal reflows correctly.

**Step 6: Multi-client**

Open the same session in two browser tabs. Verify both receive output. Type in one — verify output appears in both.

**Step 7: Reconnect**

Refresh the page. Verify the terminal reconnects and shows scrollback.

**Step 8: Session end**

Kill the tmux session (`tmux kill-session -t <name>`). Verify `[Session ended]` appears.

**Step 9: Large paste**

Paste a large block of text (100+ characters). Verify it arrives correctly.

**Step 10: Full-screen programs**

Run `less` or `vim` inside a session. Verify alternate screen mode works. Exit — verify scrollback resumes.

**Step 11: Commit (if any fixes were needed)**

```bash
git commit -m "fix: address issues found during manual verification"
```
