package signal

import (
	"bytes"
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
			name:       "completed with BEL terminator",
			data:       []byte("\x1b]777;notify;completed;Task done\x07"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Task done"},
		},
		{
			name:       "completed with ST terminator",
			data:       []byte("\x1b]777;notify;completed;Task done\x1b\\"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Task done"},
		},
		{
			name:       "needs_input with message",
			data:       []byte("\x1b]777;notify;needs_input;Approve file deletion?\x07"),
			wantCount:  1,
			wantStates: []string{"needs_input"},
			wantMsgs:   []string{"Approve file deletion?"},
		},
		{
			name:       "error with message",
			data:       []byte("\x1b]777;notify;error;Build failed\x07"),
			wantCount:  1,
			wantStates: []string{"error"},
			wantMsgs:   []string{"Build failed"},
		},
		{
			name:       "needs_testing",
			data:       []byte("\x1b]777;notify;needs_testing;Please test the new feature\x07"),
			wantCount:  1,
			wantStates: []string{"needs_testing"},
			wantMsgs:   []string{"Please test the new feature"},
		},
		{
			name:       "working clears status",
			data:       []byte("\x1b]777;notify;working;\x07"),
			wantCount:  1,
			wantStates: []string{"working"},
			wantMsgs:   []string{""},
		},
		{
			name:       "multiple signals",
			data:       []byte("output\x1b]777;notify;working;\x07more output\x1b]777;notify;completed;Done\x07end"),
			wantCount:  2,
			wantStates: []string{"working", "completed"},
			wantMsgs:   []string{"", "Done"},
		},
		{
			name:       "non-schmux OSC 777 ignored",
			data:       []byte("\x1b]777;notify;random_title;some message\x07"),
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "mixed schmux and non-schmux",
			data:       []byte("\x1b]777;notify;random;msg\x07\x1b]777;notify;completed;Done\x07"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Done"},
		},
		{
			name:       "empty data",
			data:       []byte{},
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "no signals in data",
			data:       []byte("regular terminal output with no OSC sequences"),
			wantCount:  0,
			wantStates: nil,
			wantMsgs:   nil,
		},
		{
			name:       "signal embedded in output",
			data:       []byte("Building project...\n\x1b]777;notify;completed;Build successful\x07\n$"),
			wantCount:  1,
			wantStates: []string{"completed"},
			wantMsgs:   []string{"Build successful"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := ParseSignals(tt.data)

			if len(signals) != tt.wantCount {
				t.Errorf("ParseSignals() returned %d signals, want %d", len(signals), tt.wantCount)
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

func TestExtractAndStripSignals(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantCount  int
		wantClean  []byte
		wantStates []string
	}{
		{
			name:       "strip single signal",
			data:       []byte("output\x1b]777;notify;completed;Done\x07more"),
			wantCount:  1,
			wantClean:  []byte("outputmore"),
			wantStates: []string{"completed"},
		},
		{
			name:       "strip multiple signals",
			data:       []byte("start\x1b]777;notify;working;\x07middle\x1b]777;notify;completed;Done\x07end"),
			wantCount:  2,
			wantClean:  []byte("startmiddleend"),
			wantStates: []string{"working", "completed"},
		},
		{
			name:       "preserve non-schmux OSC 777",
			data:       []byte("start\x1b]777;notify;random;msg\x07end"),
			wantCount:  0,
			wantClean:  []byte("start\x1b]777;notify;random;msg\x07end"),
			wantStates: nil,
		},
		{
			name:       "strip schmux but preserve non-schmux",
			data:       []byte("a\x1b]777;notify;random;x\x07b\x1b]777;notify;completed;y\x07c"),
			wantCount:  1,
			wantClean:  []byte("a\x1b]777;notify;random;x\x07bc"),
			wantStates: []string{"completed"},
		},
		{
			name:       "no signals returns original",
			data:       []byte("just regular output"),
			wantCount:  0,
			wantClean:  []byte("just regular output"),
			wantStates: nil,
		},
		{
			name:       "empty data",
			data:       []byte{},
			wantCount:  0,
			wantClean:  []byte{},
			wantStates: nil,
		},
		{
			name:       "signal with ST terminator stripped",
			data:       []byte("before\x1b]777;notify;error;Failed\x1b\\after"),
			wantCount:  1,
			wantClean:  []byte("beforeafter"),
			wantStates: []string{"error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals, clean := ExtractAndStripSignals(tt.data)

			if len(signals) != tt.wantCount {
				t.Errorf("ExtractAndStripSignals() returned %d signals, want %d", len(signals), tt.wantCount)
			}

			if !bytes.Equal(clean, tt.wantClean) {
				t.Errorf("ExtractAndStripSignals() clean = %q, want %q", clean, tt.wantClean)
			}

			for i, sig := range signals {
				if i >= len(tt.wantStates) {
					break
				}
				if sig.State != tt.wantStates[i] {
					t.Errorf("signals[%d].State = %q, want %q", i, sig.State, tt.wantStates[i])
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

// TestOSCPatternEdgeCases tests edge cases in OSC sequence parsing.
func TestOSCPatternEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantCount int
	}{
		{
			name:      "incomplete OSC sequence",
			data:      []byte("\x1b]777;notify;completed;msg"),
			wantCount: 0, // Missing terminator
		},
		{
			name:      "wrong OSC number",
			data:      []byte("\x1b]999;notify;completed;msg\x07"),
			wantCount: 0,
		},
		{
			name:      "missing notify keyword",
			data:      []byte("\x1b]777;completed;msg\x07"),
			wantCount: 0,
		},
		{
			name:      "message with special chars",
			data:      []byte("\x1b]777;notify;completed;Message with \"quotes\" and 'apostrophes'\x07"),
			wantCount: 1,
		},
		{
			name:      "empty message",
			data:      []byte("\x1b]777;notify;completed;\x07"),
			wantCount: 1,
		},
		{
			name:      "message with unicode",
			data:      []byte("\x1b]777;notify;completed;完成 ✓\x07"),
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := ParseSignals(tt.data)
			if len(signals) != tt.wantCount {
				t.Errorf("ParseSignals() returned %d signals, want %d", len(signals), tt.wantCount)
			}
		})
	}
}
