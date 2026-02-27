package controlmode

import (
	"context"
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
		t.Error("expected non-nil parser")
	}
	if p1.connectionID != "" {
		t.Error("expected empty connection ID")
	}

	// With connection ID
	p2 := NewParser(input, nil, "test-conn-123")
	if p2 == nil {
		t.Error("expected non-nil parser")
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

	_, err := client.Execute(ctx, "test command")
	if err == nil {
		t.Error("expected timeout error")
	}

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
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
		_, err := client.Execute(ctx, "test command")
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

	if err := client.SendKeys(context.Background(), "%1", "\x1b\r"); err != nil {
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

	if err := client.SendKeys(context.Background(), "%7", "abc\x1b\rd"); err != nil {
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

			if err := client.SendKeys(context.Background(), "%2", tt.in); err != nil {
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
	_, err := client.Execute(ctx, "test")
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
