package controlmode

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParser_NewParser(t *testing.T) {
	input := strings.NewReader("test input")

	// Without connection ID
	p1 := NewParser(input)
	if p1 == nil {
		t.Error("expected non-nil parser")
	}
	if p1.connectionID != "" {
		t.Error("expected empty connection ID")
	}

	// With connection ID
	p2 := NewParser(input, "test-conn-123")
	if p2 == nil {
		t.Error("expected non-nil parser")
	}
	if p2.connectionID != "test-conn-123" {
		t.Errorf("expected connection ID 'test-conn-123', got '%s'", p2.connectionID)
	}
}

func TestParser_ParseBeginEnd(t *testing.T) {
	input := strings.NewReader("%begin 1234 0 0\nresponse line\n%end 1234 0 0\n")
	parser := NewParser(input)

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
	parser := NewParser(input)

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
	parser := NewParser(input)

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
	parser := NewParser(input)

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
	parser := NewParser(input)

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
	parser := NewParser(input)

	// Create a mock stdin
	var stdin strings.Builder
	client := NewClient(&stdin, parser)
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
	parser := NewParser(input)
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
	parser := NewParser(input)

	// Create a mock stdin
	var stdin strings.Builder
	client := NewClient(&stdin, parser)
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

// TestCloseOrphanedChannels verifies that Close() properly cleans up
// the channel registry (Issue 2 fix).
func TestCloseOrphanedChannels(t *testing.T) {
	// Create a mock parser
	input := strings.NewReader("")
	parser := NewParser(input)

	var stdin strings.Builder
	client := NewClient(&stdin, parser)
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
