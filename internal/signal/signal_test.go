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
			got := string(StripANSIBytes(nil, []byte(tt.in)))
			if got != tt.want {
				t.Errorf("StripANSIBytes(%q) = %q, want %q", tt.in, got, tt.want)
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
			// This test uses raw terminal output that contained a bracket-marker signal.
			// It tests ANSI stripping (cursor movements, DEC private modes, colors),
			// not the signal detection protocol itself.
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
			got := string(StripANSIBytes(nil, []byte(tt.in)))
			if got != tt.want {
				t.Errorf("StripANSIBytes(%q) =\n  %q\nwant:\n  %q", tt.in, got, tt.want)
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

func TestParseSignalFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *Signal
	}{
		{"completed with message", "completed Implemented login\n", &Signal{State: "completed", Message: "Implemented login"}},
		{"completed no trailing newline", "completed Implemented login", &Signal{State: "completed", Message: "Implemented login"}},
		{"needs_input", "needs_input Should I delete these files?", &Signal{State: "needs_input", Message: "Should I delete these files?"}},
		{"working no message", "working", &Signal{State: "working", Message: ""}},
		{"working with trailing newline", "working\n", &Signal{State: "working", Message: ""}},
		{"error with message", "error Build failed", &Signal{State: "error", Message: "Build failed"}},
		{"invalid state", "banana something", nil},
		{"empty string", "", nil},
		{"whitespace only", "  \n  ", nil},
		{"multiple lines uses first", "completed Done\nneeds_input Wait", &Signal{State: "completed", Message: "Done"}},
		{"message with spaces", "completed This is a long message with spaces", &Signal{State: "completed", Message: "This is a long message with spaces"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSignalFile(tt.content)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseSignalFile(%q) = %+v, want nil", tt.content, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseSignalFile(%q) = nil, want %+v", tt.content, tt.want)
			}
			if got.State != tt.want.State {
				t.Errorf("State = %q, want %q", got.State, tt.want.State)
			}
			if got.Message != tt.want.Message {
				t.Errorf("Message = %q, want %q", got.Message, tt.want.Message)
			}
		})
	}
}
