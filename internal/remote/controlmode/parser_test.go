package controlmode

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestUnescapeOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no escapes",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "newline escape",
			input:    "line1\\012line2",
			expected: "line1\nline2",
		},
		{
			name:     "carriage return escape",
			input:    "text\\015\\012",
			expected: "text\r\n",
		},
		{
			name:     "backslash escape",
			input:    "path\\134subdir",
			expected: "path\\subdir",
		},
		{
			name:     "escape sequence",
			input:    "\\033[32mgreen\\033[0m",
			expected: "\033[32mgreen\033[0m",
		},
		{
			name:     "mixed content",
			input:    "nicholas@yelena:~$ ls\\012file1  file2\\012",
			expected: "nicholas@yelena:~$ ls\nfile1  file2\n",
		},
		{
			name:     "partial escape at end",
			input:    "text\\01",
			expected: "text\\01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnescapeOutput(tt.input)
			if result != tt.expected {
				t.Errorf("UnescapeOutput(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParserOutputEvent(t *testing.T) {
	input := "%output %0 hello\\012world\n"
	reader := strings.NewReader(input)
	parser := NewParser(reader)

	go parser.Run()

	event := <-parser.Output()
	if event.PaneID != "%0" {
		t.Errorf("PaneID = %q, want %%0", event.PaneID)
	}
	if event.Data != "hello\nworld" {
		t.Errorf("Data = %q, want %q", event.Data, "hello\nworld")
	}
}

func TestParserCommandResponse(t *testing.T) {
	input := `%begin 1234 42 1
session info here
%end 1234 42 1
`
	reader := strings.NewReader(input)
	parser := NewParser(reader)

	go parser.Run()

	resp := <-parser.Responses()
	if resp.CommandID != 42 {
		t.Errorf("CommandID = %d, want 42", resp.CommandID)
	}
	if !resp.Success {
		t.Error("Expected Success to be true")
	}
	if resp.Content != "session info here" {
		t.Errorf("Content = %q, want %q", resp.Content, "session info here")
	}
}

func TestParserErrorResponse(t *testing.T) {
	input := `%begin 1234 99 1
parse error: unknown command: badcmd
%error 1234 99 1
`
	reader := strings.NewReader(input)
	parser := NewParser(reader)

	go parser.Run()

	resp := <-parser.Responses()
	if resp.CommandID != 99 {
		t.Errorf("CommandID = %d, want 99", resp.CommandID)
	}
	if resp.Success {
		t.Error("Expected Success to be false")
	}
	if !strings.Contains(resp.Content, "unknown command") {
		t.Errorf("Content should contain 'unknown command': %q", resp.Content)
	}
}

func TestParserNotification(t *testing.T) {
	input := "%window-add @5\n"
	reader := strings.NewReader(input)
	parser := NewParser(reader)

	go parser.Run()

	event := <-parser.Events()
	if event.Type != "window-add" {
		t.Errorf("Type = %q, want window-add", event.Type)
	}
	if len(event.Args) != 1 || event.Args[0] != "@5" {
		t.Errorf("Args = %v, want [@5]", event.Args)
	}
}

// TestParserHighThroughput verifies the parser can handle 10K rapid commands
// without dropping responses (tests Issue 1 fix: increased buffer size).
func TestParserHighThroughput(t *testing.T) {
	const numCommands = 10000
	var input strings.Builder

	// Generate 10K rapid commands
	for i := 0; i < numCommands; i++ {
		input.WriteString(fmt.Sprintf("%%begin 1000 %d 1\n", i))
		input.WriteString(fmt.Sprintf("response %d\n", i))
		input.WriteString(fmt.Sprintf("%%end 1000 %d 1\n", i))
	}

	reader := strings.NewReader(input.String())
	parser := NewParser(reader, "high-throughput-test")

	go parser.Run()

	// Drain responses and verify all are received
	received := make(map[int]bool)
	timeout := time.After(10 * time.Second)

	for i := 0; i < numCommands; i++ {
		select {
		case resp := <-parser.Responses():
			if !resp.Success {
				t.Errorf("Response %d failed unexpectedly", resp.CommandID)
			}
			if received[resp.CommandID] {
				t.Errorf("Duplicate response for command %d", resp.CommandID)
			}
			received[resp.CommandID] = true
		case <-timeout:
			t.Fatalf("Timeout waiting for responses, got %d/%d", len(received), numCommands)
		}
	}

	// Verify no responses were dropped
	dropped := parser.droppedResponses.Load()
	if dropped > 0 {
		t.Errorf("Parser dropped %d responses, expected 0", dropped)
	}

	if len(received) != numCommands {
		t.Errorf("Received %d responses, expected %d", len(received), numCommands)
	}
}
