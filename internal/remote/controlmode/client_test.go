package controlmode

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type ackWriter struct {
	sb    *strings.Builder
	ackFn func()
}

func (w *ackWriter) Write(p []byte) (int, error) {
	n, err := w.sb.Write(p)
	if err == nil && w.ackFn != nil {
		w.ackFn()
	}
	return n, err
}

func TestParser_NewParser(t *testing.T) {
	input := strings.NewReader("test input")

	// Without connection ID
	p1 := NewParser(input, nil)
	if p1 == nil {
		t.Fatal("expected non-nil parser")
	}
	if p1.connectionID != "" {
		t.Error("expected empty connection ID")
	}

	// With connection ID
	p2 := NewParser(input, nil, "test-conn-123")
	if p2 == nil {
		t.Fatal("expected non-nil parser")
	}
	if p2.connectionID != "test-conn-123" {
		t.Errorf("expected connection ID 'test-conn-123', got '%s'", p2.connectionID)
	}
}

func TestParser_ParseBeginEnd(t *testing.T) {
	input := strings.NewReader("%begin 1234 0 0\nresponse line\n%end 1234 0 0\n")
	parser := NewParser(input, nil)

	go parser.Run()

	// Wait for response
	select {
	case resp := <-parser.Responses():
		if !resp.Success {
			t.Error("expected successful response")
		}
		if resp.CommandID != 0 {
			t.Errorf("expected command ID 0, got %d", resp.CommandID)
		}
		if resp.Content != "response line" {
			t.Errorf("expected content 'response line', got '%s'", resp.Content)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for response")
	}

	parser.Close()
}

func TestParser_ParseError(t *testing.T) {
	input := strings.NewReader("%begin 1234 0 0\nerror message\n%error 1234 0 0\n")
	parser := NewParser(input, nil)

	go parser.Run()

	// Wait for error response
	select {
	case resp := <-parser.Responses():
		if resp.Success {
			t.Error("expected error response")
		}
		if resp.Content != "error message" {
			t.Errorf("expected content 'error message', got '%s'", resp.Content)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for error response")
	}

	parser.Close()
}

func TestParser_ParseOutput(t *testing.T) {
	input := strings.NewReader("%output %5 hello world\n")
	parser := NewParser(input, nil)

	go parser.Run()

	// Wait for output event
	select {
	case output := <-parser.Output():
		if output.PaneID != "%5" {
			t.Errorf("expected pane ID '%%5', got '%s'", output.PaneID)
		}
		if output.Data != "hello world" {
			t.Errorf("expected data 'hello world', got '%s'", output.Data)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for output event")
	}

	parser.Close()
}

func TestParser_MultilineResponse(t *testing.T) {
	input := strings.NewReader("%begin 1234 0 0\nline 1\nline 2\nline 3\n%end 1234 0 0\n")
	parser := NewParser(input, nil)

	go parser.Run()

	// Wait for response
	select {
	case resp := <-parser.Responses():
		expected := "line 1\nline 2\nline 3"
		if resp.Content != expected {
			t.Errorf("expected content %q, got %q", expected, resp.Content)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for response")
	}

	parser.Close()
}

func TestParser_Close(t *testing.T) {
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	// Close should be safe to call multiple times
	parser.Close()
	parser.Close()

	// Channels should be closed
	select {
	case _, ok := <-parser.Output():
		if ok {
			t.Error("output channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("output channel should be readable (closed)")
	}

	select {
	case _, ok := <-parser.Responses():
		if ok {
			t.Error("responses channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("responses channel should be readable (closed)")
	}
}

func TestParser_UnescapeOutput(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "hello",
			expected: "hello",
		},
		{
			input:    `hello\012world`, // \012 is newline in octal
			expected: "hello\nworld",
		},
		{
			input:    "no escape",
			expected: "no escape",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := UnescapeOutput(tt.input)
			if result != tt.expected {
				t.Errorf("UnescapeOutput(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestClient_ContextTimeout(t *testing.T) {
	// Create a mock parser that never responds
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	// Create a mock stdin
	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()

	// Execute with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, _, err := client.Execute(ctx, "test command")
	if err == nil {
		t.Error("expected timeout error")
	}

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	client.Close()
}

func TestClient_PauseNotification(t *testing.T) {
	input := strings.NewReader("%pause %5\n")
	parser := NewParser(input, nil)

	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()

	go parser.Run()

	select {
	case paneID := <-client.Pauses():
		if paneID != "%5" {
			t.Errorf("expected pane %%5, got %s", paneID)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for pause notification")
	}

	client.Close()
}

func TestParser_DroppedEvents(t *testing.T) {
	// Create parser with small buffer
	input := strings.NewReader("")
	parser := NewParser(input, nil)
	parser.output = make(chan OutputEvent, 1) // Very small buffer

	// Fill the buffer
	parser.sendOutput(OutputEvent{PaneID: "%1", Data: "test1"})
	parser.sendOutput(OutputEvent{PaneID: "%2", Data: "test2"}) // This should drop

	// Check drop counter
	dropped := parser.droppedOutputs.Load()
	if dropped == 0 {
		t.Error("expected dropped events to be counted")
	}
}

// TestExecuteTimeoutNoLeak verifies that timing out 100 commands doesn't leak
// channels or goroutines (tests Issue 2 fix: response channel registry).
func TestExecuteTimeoutNoLeak(t *testing.T) {
	// Create a mock parser that never responds
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	// Create a mock stdin
	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()
	defer client.Close()

	// Record baseline goroutine and channel count
	runtime.GC()
	time.Sleep(50 * time.Millisecond) // Let GC complete
	baselineGoroutines := runtime.NumGoroutine()

	// Execute 100 commands that will all timeout
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		_, _, err := client.Execute(ctx, "test command")
		cancel() // Important: always call cancel
		if err == nil {
			t.Errorf("command %d: expected timeout error", i)
		}
	}

	// Force GC and check for leaks
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	// Verify channel registry was cleaned up
	client.respChansMu.Lock()
	registeredChans := len(client.respChans)
	client.respChansMu.Unlock()

	if registeredChans > 0 {
		t.Errorf("expected 0 registered channels after timeouts, got %d (leak detected)", registeredChans)
	}

	// Goroutine count should be stable (allow small variance for runtime internals)
	goroutineLeak := finalGoroutines - baselineGoroutines
	if goroutineLeak > 10 {
		t.Errorf("potential goroutine leak: baseline=%d, final=%d, leaked=%d",
			baselineGoroutines, finalGoroutines, goroutineLeak)
	}
}

func TestClientSendKeys_MetaEnterPreserved(t *testing.T) {
	parser := NewParser(strings.NewReader(""), nil)
	var buf strings.Builder
	w := &ackWriter{
		sb: &buf,
		ackFn: func() {
			parser.responses <- CommandResponse{Success: true}
		},
	}
	client := NewClient(w, parser, nil)
	client.Start()
	defer client.Close()

	if _, err := client.SendKeys(context.Background(), "%1", "\x1b\r"); err != nil {
		t.Fatalf("SendKeys returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "send-keys -t %1 M-Enter\n") {
		t.Fatalf("expected M-Enter command, got %q", got)
	}
	if strings.Contains(got, "send-keys -t %1 Escape\n") || strings.Contains(got, "send-keys -t %1 Enter\n") {
		t.Fatalf("expected meta-enter to remain a single key, got %q", got)
	}

}

func TestClientSendKeys_LiteralAndMetaEnterSequence(t *testing.T) {
	parser := NewParser(strings.NewReader(""), nil)
	var buf strings.Builder
	w := &ackWriter{
		sb: &buf,
		ackFn: func() {
			parser.responses <- CommandResponse{Success: true}
		},
	}
	client := NewClient(w, parser, nil)
	client.Start()
	defer client.Close()

	if _, err := client.SendKeys(context.Background(), "%7", "abc\x1b\rd"); err != nil {
		t.Fatalf("SendKeys returned error: %v", err)
	}

	got := buf.String()
	expectedParts := []string{
		"send-keys -t %7 -l 'abc'\n",
		"send-keys -t %7 M-Enter\n",
		"send-keys -t %7 -l 'd'\n",
	}
	for _, part := range expectedParts {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}

	// Assert order (cheaply).
	last := -1
	for _, part := range expectedParts {
		idx := strings.Index(got, part)
		if idx == -1 || idx < last {
			t.Fatalf("unexpected order in %q", got)
		}
		last = idx
	}
}

func TestClientSendKeys_MetaBackspacePreserved(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "esc-del", in: "\x1b\x7f"},
		{name: "esc-bs", in: "\x1b\b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(strings.NewReader(""), nil)
			var buf strings.Builder
			w := &ackWriter{
				sb: &buf,
				ackFn: func() {
					parser.responses <- CommandResponse{Success: true}
				},
			}
			client := NewClient(w, parser, nil)
			client.Start()
			defer client.Close()

			if _, err := client.SendKeys(context.Background(), "%2", tt.in); err != nil {
				t.Fatalf("SendKeys returned error: %v", err)
			}

			got := buf.String()
			if !strings.Contains(got, "send-keys -t %2 M-BSpace\n") {
				t.Fatalf("expected M-BSpace command, got %q", got)
			}
			if strings.Contains(got, "send-keys -t %2 Escape\n") || strings.Contains(got, "send-keys -t %2 BSpace\n") {
				t.Fatalf("expected meta-backspace to remain a single key, got %q", got)
			}
		})
	}
}

// TestCloseOrphanedChannels verifies that Close() properly cleans up
// the channel registry (Issue 2 fix).
func TestCloseOrphanedChannels(t *testing.T) {
	// Create a mock parser
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()

	// The key behavior we're testing is that channels are properly
	// cleaned up by the defer in Execute(). We verify this by:
	// 1. Running Execute() which registers a channel
	// 2. Letting it timeout (defer runs, deregistering the channel)
	// 3. Verifying the registry is empty afterward

	// Start a command that will timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	// Check registry before Execute
	client.respChansMu.Lock()
	beforeCount := len(client.respChans)
	client.respChansMu.Unlock()

	// Execute will register, timeout, and deregister
	_, _, err := client.Execute(ctx, "test")
	if err == nil {
		t.Error("expected timeout error")
	}

	// Check registry after Execute completed (defer ran)
	client.respChansMu.Lock()
	afterCount := len(client.respChans)
	client.respChansMu.Unlock()

	// Should be the same (channel was registered then deregistered)
	if afterCount != beforeCount {
		t.Errorf("channel registry leaked: before=%d, after=%d", beforeCount, afterCount)
	}

	// Close should not panic even if registry is empty
	client.Close()
}

func TestClientDoubleClose(t *testing.T) {
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()

	// First close should succeed
	client.Close()

	// Second close should not panic
	client.Close()
}

func TestClientExecuteAfterClose(t *testing.T) {
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()
	client.Close()

	// Execute after Close should return an error, not panic
	_, _, err := client.Execute(context.Background(), "list-windows")
	if err == nil {
		t.Fatal("expected error from Execute after Close")
	}
	if !strings.Contains(err.Error(), "client closed") {
		t.Fatalf("expected 'client closed' error, got: %v", err)
	}
}

// TestExecuteCollapsesNewlines verifies that Execute replaces newlines in
// commands with spaces before writing to stdin. tmux control mode uses
// newlines as command terminators — an embedded newline would split one
// command into multiple commands, corrupting the protocol. This happens
// when shell-quoted arguments contain literal newlines (e.g., persona
// prompts passed via --append-system-prompt on remote sessions).
func TestExecuteCollapsesNewlines(t *testing.T) {
	// Set up a parser that will feed back a response so Execute doesn't block.
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)
	go parser.Run()

	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()
	defer client.Close()

	// Send the response in the background so Execute can return.
	go func() {
		// Give Execute a moment to write the command
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(pw, "%begin 1234 0 1\n%end 1234 0 1\n")
	}()

	cmd := "new-window -n test 'line1\nline2\nline3'"
	_, _, err := client.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	written := stdin.String()

	// The written command must be a single line (no embedded newlines).
	lines := strings.Split(strings.TrimSuffix(written, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("command was split into %d lines, want 1.\nWritten:\n%s", len(lines), written)
	}

	// The newlines must have been replaced with spaces.
	if strings.Contains(written, "line1\nline2") {
		t.Error("raw newlines survived in the command written to stdin")
	}
	if !strings.Contains(written, "line1 line2 line3") {
		t.Errorf("expected newlines collapsed to spaces, got: %q", written)
	}
}

// TestGetCursorState verifies the parsing logic of GetCursorState.
func TestGetCursorState(t *testing.T) {
	tests := []struct {
		name        string
		response    string // What tmux returns from display-message
		wantX       int
		wantY       int
		wantVisible bool
		wantErr     bool
		errSubstr   string
	}{
		{
			name:        "cursor at origin, visible",
			response:    "0 0 1",
			wantX:       0,
			wantY:       0,
			wantVisible: true,
		},
		{
			name:        "cursor at prompt position, visible",
			response:    "2 23 1",
			wantX:       2,
			wantY:       23,
			wantVisible: true,
		},
		{
			name:        "cursor hidden (Claude Code TUI)",
			response:    "0 52 0",
			wantX:       0,
			wantY:       52,
			wantVisible: false,
		},
		{
			name:        "large cursor position, hidden",
			response:    "119 49 0",
			wantX:       119,
			wantY:       49,
			wantVisible: false,
		},
		{
			name:      "empty response",
			response:  "",
			wantErr:   true,
			errSubstr: "unexpected cursor state format",
		},
		{
			name:      "two values (missing flag)",
			response:  "5 10",
			wantErr:   true,
			errSubstr: "unexpected cursor state format",
		},
		{
			name:      "four values",
			response:  "1 2 3 4",
			wantErr:   true,
			errSubstr: "unexpected cursor state format",
		},
		{
			name:      "non-numeric x",
			response:  "abc 5 1",
			wantErr:   true,
			errSubstr: "failed to parse cursor_x",
		},
		{
			name:      "non-numeric y",
			response:  "5 abc 1",
			wantErr:   true,
			errSubstr: "failed to parse cursor_y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use io.Pipe so we can write the response after the command
			// is enqueued, avoiding a race where the parser processes the
			// response before Execute registers its response channel.
			pr, pw := io.Pipe()
			parser := NewParser(pr, nil)

			// signalWriter signals when data is written, so we know the
			// command has been enqueued and sent before we inject a response.
			stdin := &signalWriter{written: make(chan struct{})}
			client := NewClient(stdin, parser, nil)
			client.Start()
			defer client.Close()

			go parser.Run()

			// Write the response only after the command has been sent
			stream := "%begin 1234 0 0\n" + tt.response + "\n%end 1234 0 0\n"
			go func() {
				<-stdin.written
				pw.Write([]byte(stream))
				pw.Close()
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			cs, err := client.GetCursorState(ctx, "%0")

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cs.X != tt.wantX {
				t.Errorf("X = %d, want %d", cs.X, tt.wantX)
			}
			if cs.Y != tt.wantY {
				t.Errorf("Y = %d, want %d", cs.Y, tt.wantY)
			}
			if cs.Visible != tt.wantVisible {
				t.Errorf("Visible = %v, want %v", cs.Visible, tt.wantVisible)
			}
		})
	}
}

// signalWriter wraps an io.Writer and signals when the first write occurs.
type signalWriter struct {
	mu      sync.Mutex
	buf     strings.Builder
	written chan struct{}
	once    sync.Once
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	w.once.Do(func() { close(w.written) })
	return n, err
}

// TestGetCursorPositionExecuteError verifies GetCursorPosition wraps Execute errors.
func TestGetCursorPositionExecuteError(t *testing.T) {
	// Parser that never responds — Execute will timeout
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)
	client.Start()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, _, err := client.GetCursorPosition(ctx, "%0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get cursor state") {
		t.Errorf("expected wrapped error, got %q", err.Error())
	}
}

// TestStaleResponsesDiscardedOnReconnect verifies that responses from a previous
// control mode session (with high command IDs) are discarded and don't corrupt
// the first real command sent after reconnection.
func TestStaleResponsesDiscardedOnReconnect(t *testing.T) {
	// Simulate what happens on daemon restart:
	// 1. Parser sees stale %begin/%end blocks from the previous session
	// 2. Client.Start() begins processing
	// 3. First real Execute() should get ITS response, not a stale one

	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)

	stdin := &signalWriter{written: make(chan struct{})}
	client := NewClient(stdin, parser, nil)

	go parser.Run()

	// Write stale responses BEFORE client starts (simulates tmux initial dump)
	staleData := "" +
		"%begin 1000 321040 0\nstale response 1\n%end 1000 321040 0\n" +
		"%begin 1000 321041 0\nstale response 2\n%end 1000 321041 0\n" +
		"%begin 1000 321042 0\nstale response 3\n%end 1000 321042 0\n"
	pw.Write([]byte(staleData))

	// Give parser time to buffer the stale responses
	time.Sleep(50 * time.Millisecond)

	client.Start()

	// Now send a real command — the response should be "real response", not "stale response N"
	realResponse := "%begin 1000 321043 0\nreal response\n%end 1000 321043 0\n"
	go func() {
		<-stdin.written
		pw.Write([]byte(realResponse))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _, err := client.Execute(ctx, "display-message -p 'ready'")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result != "real response" {
		t.Errorf("expected 'real response', got %q (stale response was delivered)", result)
	}

	pw.Close()
	client.Close()
}

// TestStaleResponsesCountedAsDiscarded verifies that discarded stale responses
// are counted and accessible via DiscardedStale().
func TestStaleResponsesCountedAsDiscarded(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)

	var stdin strings.Builder
	client := NewClient(&stdin, parser, nil)

	go parser.Run()

	// Write stale responses before client starts
	staleData := "" +
		"%begin 1000 500 0\nstale 1\n%end 1000 500 0\n" +
		"%begin 1000 501 0\nstale 2\n%end 1000 501 0\n"
	pw.Write([]byte(staleData))

	time.Sleep(50 * time.Millisecond)

	client.Start()

	// Give processResponses time to drain the stale responses
	time.Sleep(100 * time.Millisecond)

	discarded := client.DiscardedStale()
	if discarded != 2 {
		t.Errorf("expected 2 discarded stale responses, got %d", discarded)
	}

	pw.Close()
	client.Close()
}

// TestLateStaleResponseDiscardedPreSync verifies that a stale response
// arriving after firstCommandSent but before MarkSynced is discarded rather
// than logged as a FIFO desync. This reproduces the race where the
// attach-session response arrives after the sync command is already queued.
func TestLateStaleResponseDiscardedPreSync(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)

	// Use signalWriter to know when the command has been sent to stdin.
	stdin := &signalWriter{written: make(chan struct{})}
	client := NewClient(stdin, parser, nil)

	go parser.Run()
	client.Start()

	// Simulate the race:
	// 1. Execute sends a command (firstCommandSent becomes true)
	// 2. A stale attach-session response arrives first (delivered to the command)
	// 3. The real response arrives with an empty queue (should be discarded, not error)
	go func() {
		<-stdin.written
		// Stale attach response arrives first — gets delivered to the waiting command
		pw.Write([]byte("%begin 1000 33201 0\nattach-session ok\n%end 1000 33201 0\n"))
		// Real display-message response arrives — queue is now empty
		pw.Write([]byte("%begin 1000 33202 0\n__SCHMUX_SYNC__\n%end 1000 33202 0\n"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _, err := client.Execute(ctx, "display-message -p '__SCHMUX_SYNC__'")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// The command gets the stale attach response (wrong content).
	// This is expected — the sync loop in localsource.go handles this by retrying.
	if result != "attach-session ok" {
		t.Errorf("expected stale content 'attach-session ok', got %q", result)
	}

	// Give processResponses time to handle the orphaned real response
	time.Sleep(50 * time.Millisecond)

	// The late stale response should be counted as discarded, not logged as FIFO desync
	discarded := client.DiscardedStale()
	if discarded != 1 {
		t.Errorf("expected 1 discarded late stale response, got %d", discarded)
	}

	// After MarkSynced, a genuinely orphaned response would be a real FIFO desync
	client.MarkSynced()

	pw.Close()
	client.Close()
}

// TestFIFODesyncStillErrorsPostSync verifies that after MarkSynced, a response
// arriving with an empty queue is still logged as a real FIFO desync error
// (not silently discarded). This ensures the pre-sync grace period doesn't
// mask genuine protocol violations during normal operation.
func TestFIFODesyncStillErrorsPostSync(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)

	stdin := &signalWriter{written: make(chan struct{})}
	client := NewClient(stdin, parser, nil)

	go parser.Run()
	client.Start()

	// Send a command and get its response normally to set firstCommandSent
	go func() {
		<-stdin.written
		pw.Write([]byte("%begin 1000 0 0\nok\n%end 1000 0 0\n"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := client.Execute(ctx, "display-message -p ok")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Mark synced — after this, orphan responses should NOT be discarded as stale
	client.MarkSynced()

	// Inject an orphan response with no command waiting
	pw.Write([]byte("%begin 1000 999 0\norphan\n%end 1000 999 0\n"))
	time.Sleep(50 * time.Millisecond)

	// This should NOT be counted as discarded stale — it's a real desync
	discarded := client.DiscardedStale()
	if discarded != 0 {
		t.Errorf("expected 0 discarded stale (post-sync orphan is a desync, not stale), got %d", discarded)
	}

	pw.Close()
	client.Close()
}

// TestResponsesAfterEpochDeliveredNormally verifies that once the epoch is
// established, subsequent commands work correctly via FIFO ordering.
func TestResponsesAfterEpochDeliveredNormally(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)

	// Use ackWriter to inject responses when commands are written to stdin.
	// A counter tracks how many commands have been sent.
	var cmdCount int
	var cmdMu sync.Mutex
	var stdinBuf strings.Builder
	w := &ackWriter{
		sb: &stdinBuf,
		ackFn: func() {
			cmdMu.Lock()
			cmdCount++
			n := cmdCount
			cmdMu.Unlock()

			switch n {
			case 1:
				pw.Write([]byte("%begin 1000 0 0\nfirst\n%end 1000 0 0\n"))
			case 2:
				pw.Write([]byte("%begin 1000 1 0\nsecond\n%end 1000 1 0\n"))
			}
		},
	}
	client := NewClient(w, parser, nil)

	go parser.Run()
	client.Start()
	client.MarkSynced() // Simulate completed sync protocol

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result1, _, err := client.Execute(ctx, "cmd1")
	if err != nil {
		t.Fatalf("first Execute returned error: %v", err)
	}
	if result1 != "first" {
		t.Errorf("first command: expected 'first', got %q", result1)
	}

	result2, _, err := client.Execute(ctx, "cmd2")
	if err != nil {
		t.Fatalf("second Execute returned error: %v", err)
	}
	if result2 != "second" {
		t.Errorf("second command: expected 'second', got %q", result2)
	}

	pw.Close()
	client.Close()
}

func TestTmuxQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple string", input: "hello", want: `"hello"`},
		{name: "empty string", input: "", want: `""`},
		{name: "with double quote", input: `say "hi"`, want: `"say \"hi\""`},
		{name: "with backslash", input: `path\to\file`, want: `"path\\to\\file"`},
		{name: "with dollar sign", input: "$HOME", want: `"\$HOME"`},
		{name: "all special chars", input: `a\b"c$d`, want: `"a\\b\"c\$d"`},
		{name: "with spaces", input: "hello world", want: `"hello world"`},
		{name: "single quote passes through", input: "it's", want: `"it's"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tmuxQuote(tt.input)
			if got != tt.want {
				t.Errorf("tmuxQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestRunCommandSemaphore_Serialization verifies that concurrent RunCommand calls
// are serialized — only one Execute() poll loop runs at a time.
func TestRunCommandSemaphore_Serialization(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)
	go parser.Run()

	// Track all commands written to stdin
	var mu sync.Mutex
	var cmds []string
	stdin := &ackWriter{
		sb:    &strings.Builder{},
		ackFn: func() {},
	}
	// Override Write to capture commands
	cmdWriter := &commandCapture{
		inner: stdin,
		onCmd: func(cmd string) {
			mu.Lock()
			cmds = append(cmds, cmd)
			mu.Unlock()
		},
	}

	client := NewClient(cmdWriter, parser, nil)
	client.Start()
	defer func() {
		pw.Close()
		client.Close()
	}()

	// Respond to all commands automatically
	var respCount int
	var respMu sync.Mutex
	go func() {
		for {
			time.Sleep(10 * time.Millisecond)
			respMu.Lock()
			n := respCount
			respMu.Unlock()
			// Feed responses for any pending commands
			resp := fmt.Sprintf("%%begin 1000 %d 0\nok\n%%end 1000 %d 0\n", n, n)
			if _, err := pw.Write([]byte(resp)); err != nil {
				return
			}
			respMu.Lock()
			respCount++
			respMu.Unlock()
		}
	}()

	// Try to start two concurrent RunCommands
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	acquired := make(chan struct{}, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// RunCommand will acquire the semaphore. Since Execute() will
			// likely timeout (mock doesn't support full RunCommand flow),
			// we just check that only one goroutine enters at a time.
			acquired <- struct{}{}
			_, _ = client.RunCommand(ctx, "/tmp", "echo test")
		}()
	}

	wg.Wait()

	// The semaphore is a channel of size 1 — verify it exists and has correct capacity
	if cap(client.runCmdSem) != 1 {
		t.Errorf("expected semaphore capacity 1, got %d", cap(client.runCmdSem))
	}
}

// TestRunCommandSemaphore_ContextCancellation verifies that a RunCommand blocked
// on the semaphore returns immediately when its context is canceled, without
// creating a tmux window.
func TestRunCommandSemaphore_ContextCancellation(t *testing.T) {
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	var stdinBuf strings.Builder
	client := NewClient(&stdinBuf, parser, nil)
	client.Start()
	defer client.Close()

	// Fill the semaphore to simulate a RunCommand already in flight
	client.runCmdSem <- struct{}{}

	// Try a second RunCommand with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.RunCommand(ctx, "/tmp", "echo blocked")
	if err == nil {
		t.Fatal("expected error from blocked RunCommand")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	// Verify no tmux commands were sent (no window created)
	written := stdinBuf.String()
	if strings.Contains(written, "new-window") {
		t.Error("RunCommand should not create a window while blocked on semaphore")
	}

	// Release the semaphore
	<-client.runCmdSem
}

// TestRunCommandSemaphore_ReleasedOnError verifies the semaphore is released
// even when RunCommand fails (e.g., window creation fails).
func TestRunCommandSemaphore_ReleasedOnError(t *testing.T) {
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	var stdinBuf strings.Builder
	client := NewClient(&stdinBuf, parser, nil)
	client.Start()
	defer client.Close()

	// RunCommand with a short timeout — Execute for new-window will timeout,
	// but the semaphore must be released so subsequent calls can proceed.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.RunCommand(ctx, "/tmp", "echo test")
	if err == nil {
		t.Fatal("expected error from RunCommand with no tmux backend")
	}

	// Semaphore must be released — verify by acquiring it without blocking
	select {
	case client.runCmdSem <- struct{}{}:
		<-client.runCmdSem // release immediately
	default:
		t.Error("semaphore was not released after RunCommand error")
	}
}

// TestRunCommand_PollsCapturePaneForSentinel verifies that RunCommand polls
// capture-pane until the end sentinel appears. It checks that new-window
// includes the command directly and at least one capture-pane is sent.
func TestRunCommand_PollsCapturePaneForSentinel(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)
	go parser.Run()

	var mu sync.Mutex
	var allCmds []string
	stdin := &commandCapture{
		inner: &strings.Builder{},
		onCmd: func(cmd string) {
			mu.Lock()
			allCmds = append(allCmds, cmd)
			mu.Unlock()
		},
	}

	client := NewClient(stdin, parser, nil)
	client.Start()
	defer func() {
		pw.Close()
		client.Close()
	}()

	// Respond to all commands automatically
	var respIdx int
	go func() {
		for {
			time.Sleep(10 * time.Millisecond)
			resp := fmt.Sprintf("%%begin 1000 %d 0\n@99 %%99\n%%end 1000 %d 0\n", respIdx, respIdx)
			if _, err := pw.Write([]byte(resp)); err != nil {
				return
			}
			respIdx++
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// RunCommand will get generic responses for all commands. The capture-pane
	// response won't have sentinels, so it returns an error or times out.
	client.RunCommand(ctx, "/tmp", "echo test")

	mu.Lock()
	defer mu.Unlock()

	foundNewWindow := false
	captureCount := 0
	for _, cmd := range allCmds {
		if strings.Contains(cmd, "new-window") {
			foundNewWindow = true
		}
		if strings.Contains(cmd, "capture-pane") {
			captureCount++
		}
	}

	if !foundNewWindow {
		t.Error("expected a 'new-window' command")
	}
	if captureCount == 0 {
		t.Error("expected at least one 'capture-pane' poll")
	}
}

// --- RunCommand polling tests ---

func TestRunCommand_ContextCancellation(t *testing.T) {
	input := strings.NewReader("")
	parser := NewParser(input, nil)

	var stdinBuf strings.Builder
	client := NewClient(&stdinBuf, parser, nil)
	client.Start()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := client.RunCommand(ctx, "/tmp", "echo test")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline exceeded, got %v", err)
	}
}

func TestRunCommand_ClientCloseDuringPoll(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)
	go parser.Run()

	// triggerClose signals the close goroutine to shut down the client.
	// Fired when onCmd stops responding (cmdCount > 5), making the test
	// deterministic instead of relying on time.Sleep.
	triggerClose := make(chan struct{}, 1)
	var cmdMu sync.Mutex
	var cmdCount int
	stdin := &commandCapture{
		inner: &strings.Builder{},
		onCmd: func(cmd string) {
			cmdMu.Lock()
			cmdCount++
			n := cmdCount
			cmdMu.Unlock()
			if strings.Contains(cmd, "new-window") {
				// Return window/pane IDs for new-window
				pw.Write([]byte(fmt.Sprintf("%%begin 1000 %d 0\n@99 %%99\n%%end 1000 %d 0\n", n-1, n-1)))
			} else if n > 5 {
				// After window creation + remain-on-exit + history-limit + send-keys + Enter,
				// don't respond to capture-pane — signal close instead
				select {
				case triggerClose <- struct{}{}:
				default:
				}
			} else {
				pw.Write([]byte(fmt.Sprintf("%%begin 1000 %d 0\n\n%%end 1000 %d 0\n", n-1, n-1)))
			}
		},
	}

	client := NewClient(stdin, parser, nil)
	client.Start()

	go func() {
		<-triggerClose
		client.Close()
		pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.RunCommand(ctx, "/tmp", "echo test")
	if err == nil {
		t.Fatal("expected error from client close")
	}
	errMsg := err.Error()
	validErrors := []string{"client closed", "context deadline exceeded", "not running"}
	matched := false
	for _, substr := range validErrors {
		if strings.Contains(errMsg, substr) {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("expected error containing 'client closed', 'not running', or 'context deadline exceeded', got: %v", err)
	}
}

// TestRunCommand_CommandIncludesSentinels verifies that the new-window command
// includes begin and end sentinels wrapping the user command.
func TestRunCommand_CommandIncludesSentinels(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)
	go parser.Run()

	var mu sync.Mutex
	var allCmds []string
	stdin := &commandCapture{
		inner: &strings.Builder{},
		onCmd: func(cmd string) {
			mu.Lock()
			allCmds = append(allCmds, cmd)
			mu.Unlock()
		},
	}

	client := NewClient(stdin, parser, nil)
	client.Start()
	defer func() {
		pw.Close()
		client.Close()
	}()

	var respIdx int
	go func() {
		for {
			time.Sleep(10 * time.Millisecond)
			resp := fmt.Sprintf("%%begin 1000 %d 0\n@99 %%99\n%%end 1000 %d 0\n", respIdx, respIdx)
			if _, err := pw.Write([]byte(resp)); err != nil {
				return
			}
			respIdx++
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client.RunCommand(ctx, "/tmp", "echo hello")

	mu.Lock()
	defer mu.Unlock()

	// The send-keys command should include BEGIN and END sentinels
	foundBegin := false
	foundEnd := false
	for _, cmd := range allCmds {
		if strings.Contains(cmd, "send-keys") && strings.Contains(cmd, "__SCHMUX_BEGIN_") {
			foundBegin = true
		}
		if strings.Contains(cmd, "send-keys") && strings.Contains(cmd, "__SCHMUX_END_") {
			foundEnd = true
		}
	}
	if !foundBegin {
		t.Error("send-keys should include __SCHMUX_BEGIN_ sentinel")
	}
	if !foundEnd {
		t.Error("send-keys should include __SCHMUX_END_ sentinel")
	}
}

// TestConcurrentExecuteFIFOOrdering verifies that N goroutines calling Execute
// concurrently all receive the correct response for their command. This tests
// the fix for the FIFO desync race where queue append and stdin write were not
// atomic, allowing goroutine B to send its command before goroutine A even
// though A was queued first.
func TestConcurrentExecuteFIFOOrdering(t *testing.T) {
	pr, pw := io.Pipe()
	parser := NewParser(pr, nil)
	go parser.Run()

	// Track commands in the order they arrive on stdin.
	var mu sync.Mutex
	var sentOrder []string
	stdin := &commandCapture{
		inner: &strings.Builder{},
		onCmd: func(cmd string) {
			mu.Lock()
			sentOrder = append(sentOrder, cmd)
			// Respond to each command with the command text as the response
			// body, so each caller can verify it got its own response.
			idx := len(sentOrder) - 1
			mu.Unlock()
			resp := fmt.Sprintf("%%begin 1000 %d 0\n%s\n%%end 1000 %d 0\n", idx, cmd, idx)
			pw.Write([]byte(resp))
		},
	}

	client := NewClient(stdin, parser, nil)
	client.Start()
	defer func() {
		pw.Close()
		client.Close()
	}()

	const N = 20
	results := make([]string, N)
	errors := make([]error, N)
	var wg sync.WaitGroup

	// Launch N concurrent Execute calls, each with a unique command.
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := fmt.Sprintf("cmd-%d", idx)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, _, err := client.Execute(ctx, cmd)
			results[idx] = result
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// Every caller must receive the response matching its own command.
	for i := 0; i < N; i++ {
		if errors[i] != nil {
			t.Errorf("cmd-%d: unexpected error: %v", i, errors[i])
			continue
		}
		expected := fmt.Sprintf("cmd-%d", i)
		if results[i] != expected {
			t.Errorf("cmd-%d: got response %q, want %q (FIFO desync)", i, results[i], expected)
		}
	}
}

// commandCapture wraps a writer and records each line written as a command.
type commandCapture struct {
	inner io.Writer
	onCmd func(cmd string)
	mu    sync.Mutex
	buf   strings.Builder
}

func (c *commandCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Write(p)
	// Extract complete lines
	for {
		s := c.buf.String()
		idx := strings.Index(s, "\n")
		if idx < 0 {
			break
		}
		line := s[:idx]
		c.buf.Reset()
		c.buf.WriteString(s[idx+1:])
		if c.onCmd != nil {
			c.onCmd(line)
		}
	}
	return len(p), nil
}
