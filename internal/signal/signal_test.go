package signal

import (
	"testing"
)

func TestIsValidState(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{"needs_input", true},
		{"needs_testing", true},
		{"completed", true},
		{"error", true},
		{"working", true},
		{"invalid", false},
		{"", false},
		{"COMPLETED", false}, // case-sensitive
		{"random_title", false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := IsValidState(tt.state); got != tt.want {
				t.Errorf("IsValidState(%q) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestParseSignals(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantCount  int
		wantStates []string
		wantMsgs   []string
	}{
		{
			name:       "empty data",
			data:       []byte{},
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "no signals in data",
			data:       []byte("regular terminal output with no signals"),
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "bracket-based completed signal on own line",
			data:       []byte("--<[schmux:completed:Task finished successfully]>--"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Task finished successfully"},
		},
		{
			name:       "bracket-based needs_input signal on own line",
			data:       []byte("--<[schmux:needs_input:Should I delete these 5 files?]>--"),
			wantCount:  1,
			wantStates: []string{"needs_input"},
			wantMsgs:   []string{"Should I delete these 5 files?"},
		},
		{
			name:       "bracket-based working signal with empty message",
			data:       []byte("--<[schmux:working:]>--"),
			wantCount:  1,
			wantStates: []string{"working"},
			wantMsgs:   []string{""},
		},
		{
			name:       "bracket-based error signal",
			data:       []byte("--<[schmux:error:Build failed]>--"),
			wantCount:  1,
			wantStates: []string{"error"},
			wantMsgs:   []string{"Build failed"},
		},
		{
			name:       "bracket-based needs_testing signal",
			data:       []byte("--<[schmux:needs_testing:Please test the new feature]>--"),
			wantCount:  1,
			wantStates: []string{"needs_testing"},
			wantMsgs:   []string{"Please test the new feature"},
		},
		{
			name:       "bracket signals inline with text - not matched",
			data:       []byte("output--<[schmux:working:]>--more--<[schmux:completed:Done]>--end"),
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "bracket signal with special characters on own line",
			data:       []byte("--<[schmux:error:Build failed with \"errors\" and 'warnings']>--"),
			wantCount:  1,
			wantStates: []string{"error"},
			wantMsgs:   []string{"Build failed with \"errors\" and 'warnings'"},
		},
		{
			name:       "invalid state in bracket signal",
			data:       []byte("--<[schmux:invalid_state:some message]>--"),
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "malformed bracket signal - missing closing",
			data:       []byte("--<[schmux:completed:message"),
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "bracket signal on own line with surrounding content",
			data:       []byte("# Header\n\nSome content\n\n--<[schmux:completed:Analysis complete]>--\n\n## More content"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Analysis complete"},
		},
		{
			name:       "bracket signal with leading whitespace",
			data:       []byte("  --<[schmux:completed:Done]>--"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Done"},
		},
		{
			name:       "bracket signal with bullet prefix (Claude Code style)",
			data:       []byte("⏺ --<[schmux:completed:Task done]>--"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Task done"},
		},
		{
			name:       "bracket signal with trailing whitespace",
			data:       []byte("--<[schmux:completed:Done]>--  "),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Done"},
		},
		{
			name:       "multiple bracket signals each on own line",
			data:       []byte("--<[schmux:working:]>--\n--<[schmux:completed:Done]>--"),
			wantCount:  2,
			wantStates: []string{"working", "completed"},
			wantMsgs:   []string{"", "Done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := parseSignals(tt.data)

			if len(signals) != tt.wantCount {
				t.Errorf("parseSignals() returned %d signals, want %d", len(signals), tt.wantCount)
				return
			}

			for i, sig := range signals {
				if i >= len(tt.wantStates) {
					break
				}
				if sig.State != tt.wantStates[i] {
					t.Errorf("signals[%d].State = %q, want %q", i, sig.State, tt.wantStates[i])
				}
				if sig.Message != tt.wantMsgs[i] {
					t.Errorf("signals[%d].Message = %q, want %q", i, sig.Message, tt.wantMsgs[i])
				}
				if sig.Timestamp.IsZero() {
					t.Errorf("signals[%d].Timestamp should not be zero", i)
				}
			}
		})
	}
}

func TestMapStateToNudge(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"needs_input", "Needs Authorization"},
		{"needs_testing", "Needs User Testing"},
		{"completed", "Completed"},
		{"error", "Error"},
		{"working", "Working"},
		{"unknown", "unknown"}, // passthrough for unknown states
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := MapStateToNudge(tt.state); got != tt.want {
				t.Errorf("MapStateToNudge(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// TestStripANSI tests ANSI escape sequence stripping from signal messages.
func TestStripANSI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no ANSI sequences",
			in:   "Task finished successfully",
			want: "Task finished successfully",
		},
		{
			name: "cursor forward sequences replace spaces",
			in:   "Task\x1b[1Cfinished\x1b[1Csuccessfully",
			want: "Task finished successfully",
		},
		{
			name: "color sequences",
			in:   "\x1b[32mSuccess\x1b[0m: done",
			want: "Success: done",
		},
		{
			name: "cursor forward with count",
			in:   "Hello\x1b[2CWorld",
			want: "Hello  World",
		},
		{
			name: "mixed cursor movements - forward becomes spaces, down becomes newlines, others removed",
			in:   "\x1b[2AUp\x1b[3BDown\x1b[4CRight\x1b[5DLeft",
			want: "Up\n\n\nDown    RightLeft",
		},
		{
			name: "mixed sequences",
			in:   "\x1b[1;31mError\x1b[0m:\x1b[1Cfailed\x1b[K",
			want: "Error: failed",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.in)
			if got != tt.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestParseSignalsWithANSI tests that ANSI sequences are stripped from signal messages.
func TestParseSignalsWithANSI(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantMsg string
	}{
		{
			name:    "bracket signal with cursor forward on own line",
			data:    []byte("--<[schmux:completed:Task\x1b[1Cfinished\x1b[1Csuccessfully]>--"),
			wantMsg: "Task finished successfully",
		},
		{
			name:    "bracket signal with color codes on own line",
			data:    []byte("--<[schmux:error:\x1b[31mBuild failed\x1b[0m]>--"),
			wantMsg: "Build failed",
		},
		{
			name:    "bracket signal with non-forward cursor movements on own line",
			data:    []byte("--<[schmux:completed:Test\x1b[2Apassed\x1b[3Bsuccessfully]>--"),
			wantMsg: "Testpassed\n\n\nsuccessfully",
		},
		{
			name:    "bracket signal with DEC Private Mode sequences",
			data:    []byte("\r\n\x1b[?2026l\x1b[?2026h\r\x1b[8A\x1b[38;2;255;255;255m\xe2\x8f\xba\x1b[1C\x1b[39m--<[schmux:needs_input:How\x1b[1Ccan\x1b[1CI\x1b[1Chelp]>--\r\x1b[2B"),
			wantMsg: "How can I help",
		},
		{
			name:    "bracket signal with OSC window title sequence",
			data:    []byte("\r\n\x1b]0;Claude Code\x07\r\x1b[6A\x1b[38;2;255;255;255m\xe2\x8f\xba\x1b[1C\x1b[39m--<[schmux:completed:Done]>--\r\x1b[2B"),
			wantMsg: "Done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := parseSignals(tt.data)
			if len(signals) != 1 {
				t.Fatalf("parseSignals() returned %d signals, want 1", len(signals))
			}
			if signals[0].Message != tt.wantMsg {
				t.Errorf("signals[0].Message = %q, want %q", signals[0].Message, tt.wantMsg)
			}
		})
	}
}

// TestStripANSIStateMachine tests the state machine ANSI stripper with advanced sequences.
func TestStripANSIStateMachine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no ANSI sequences",
			in:   "Task finished successfully",
			want: "Task finished successfully",
		},
		{
			name: "cursor forward sequences replace with spaces",
			in:   "Task\x1b[1Cfinished\x1b[1Csuccessfully",
			want: "Task finished successfully",
		},
		{
			name: "cursor forward with count",
			in:   "Hello\x1b[2CWorld",
			want: "Hello  World",
		},
		{
			name: "cursor down sequences replace with newlines",
			in:   "line1\x1b[1Bline2",
			want: "line1\nline2",
		},
		{
			name: "cursor down with count",
			in:   "line1\x1b[3Bline2",
			want: "line1\n\n\nline2",
		},
		{
			name: "color sequences stripped",
			in:   "\x1b[32mSuccess\x1b[0m: done",
			want: "Success: done",
		},
		{
			name: "DEC Private Mode sequences stripped",
			in:   "\x1b[?2026l\x1b[?2026hHello",
			want: "Hello",
		},
		{
			name: "OSC sequences stripped",
			in:   "\x1b]0;Window Title\x07Hello",
			want: "Hello",
		},
		{
			name: "OSC with ST terminator stripped",
			in:   "\x1b]0;Window Title\x1b\\Hello",
			want: "Hello",
		},
		{
			name: "DCS sequences stripped",
			in:   "\x1bPsome DCS content\x1b\\Hello",
			want: "Hello",
		},
		{
			name: "APC sequences stripped",
			in:   "\x1b_some APC content\x1b\\Hello",
			want: "Hello",
		},
		{
			name: "mixed cursor movements",
			in:   "\x1b[2AUp\x1b[3BDown\x1b[4CRight\x1b[5DLeft",
			want: "Up\n\n\nDown    RightLeft",
		},
		{
			name: "cursor forward without explicit count defaults to 1",
			in:   "A\x1b[CB",
			want: "A B",
		},
		{
			name: "cursor down without explicit count defaults to 1",
			in:   "A\x1b[BB",
			want: "A\nB",
		},
		{
			name: "real world Claude Code signal with DEC sequences",
			in:   "\r\n\x1b[?2026l\x1b[?2026h\r\x1b[8A\x1b[38;2;255;255;255m\xe2\x8f\xba\x1b[1C\x1b[39m--<[schmux:needs_input:How\x1b[1Ccan\x1b[1CI\x1b[1Chelp]>--\r\x1b[2B",
			want: "\r\n\r⏺ --<[schmux:needs_input:How can I help]>--\r\n\n",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(stripANSIBytes(nil, []byte(tt.in)))
			if got != tt.want {
				t.Errorf("stripANSIBytes(%q) =\n  %q\nwant:\n  %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"abcdefghijklmnop", "abcdefgh"},
		{"abcdefgh", "abcdefgh"},
		{"short", "short"},
		{"ab", "ab"},
		{"", ""},
		{"12345678x", "12345678"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := ShortID(tt.id); got != tt.want {
				t.Errorf("ShortID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestParseSignalsCarriageReturn(t *testing.T) {
	// Windows-style \r\n line endings
	signals := parseSignals([]byte("--<[schmux:completed:Done]>--\r\n"))
	if len(signals) != 1 {
		t.Fatalf("parseSignals with \\r\\n returned %d signals, want 1", len(signals))
	}
	if signals[0].State != "completed" || signals[0].Message != "Done" {
		t.Errorf("signal = {%q, %q}, want {completed, Done}", signals[0].State, signals[0].Message)
	}
}

func TestParseSignalsWhitespaceOnlyMessage(t *testing.T) {
	// Signal with whitespace-only message — should be parsed (whitespace is valid content)
	signals := parseSignals([]byte("--<[schmux:completed: ]>--\n"))
	if len(signals) != 1 {
		t.Fatalf("parseSignals with whitespace message returned %d signals, want 1", len(signals))
	}
	if signals[0].Message != " " {
		t.Errorf("message = %q, want %q", signals[0].Message, " ")
	}
}
