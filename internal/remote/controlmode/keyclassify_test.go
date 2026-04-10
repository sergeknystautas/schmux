package controlmode

import (
	"testing"
)

func TestClassifyKeyRuns_UTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []KeyRun
	}{
		{
			name:  "ASCII only",
			input: "hello",
			expected: []KeyRun{
				{Text: "hello", Literal: true},
			},
		},
		{
			name:  "accented characters",
			input: "café",
			expected: []KeyRun{
				{Text: "café", Literal: true},
			},
		},
		{
			name:  "emoji",
			input: "hello 🚀 world",
			expected: []KeyRun{
				{Text: "hello 🚀 world", Literal: true},
			},
		},
		{
			name:  "CJK characters",
			input: "你好世界",
			expected: []KeyRun{
				{Text: "你好世界", Literal: true},
			},
		},
		{
			name:  "mixed UTF-8 and special keys",
			input: "café\r",
			expected: []KeyRun{
				{Text: "café", Literal: true},
				{Text: "Enter", Literal: false},
			},
		},
		{
			name:  "UTF-8 between control characters",
			input: "\tcafé\t",
			expected: []KeyRun{
				{Text: "Tab", Literal: false},
				{Text: "café", Literal: true},
				{Text: "Tab", Literal: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyKeyRuns(nil, tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("got %d runs, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.expected), got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("run[%d] = %+v, want %+v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestClassifyKeyRuns(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []KeyRun
	}{
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:  "plain ASCII text",
			input: "hello",
			expected: []KeyRun{
				{Text: "hello", Literal: true},
			},
		},
		{
			name:  "Enter key CR",
			input: "\r",
			expected: []KeyRun{
				{Text: "Enter", Literal: false},
			},
		},
		{
			name:  "Enter key LF",
			input: "\n",
			expected: []KeyRun{
				{Text: "Enter", Literal: false},
			},
		},
		{
			name:  "Tab",
			input: "\t",
			expected: []KeyRun{
				{Text: "Tab", Literal: false},
			},
		},
		{
			name:  "Backspace",
			input: "\x7f",
			expected: []KeyRun{
				{Text: "BSpace", Literal: false},
			},
		},
		{
			name:  "mixed text and specials",
			input: "abc\rdef",
			expected: []KeyRun{
				{Text: "abc", Literal: true},
				{Text: "Enter", Literal: false},
				{Text: "def", Literal: true},
			},
		},

		// Arrow keys (CSI sequences)
		{
			name:  "arrow Up",
			input: "\x1b[A",
			expected: []KeyRun{
				{Text: "Up", Literal: false},
			},
		},
		{
			name:  "arrow Down",
			input: "\x1b[B",
			expected: []KeyRun{
				{Text: "Down", Literal: false},
			},
		},
		{
			name:  "arrow Right",
			input: "\x1b[C",
			expected: []KeyRun{
				{Text: "Right", Literal: false},
			},
		},
		{
			name:  "arrow Left",
			input: "\x1b[D",
			expected: []KeyRun{
				{Text: "Left", Literal: false},
			},
		},

		// Control characters
		{
			name:  "Ctrl-a",
			input: "\x01",
			expected: []KeyRun{
				{Text: "C-a", Literal: false},
			},
		},
		{
			name:  "Ctrl-c",
			input: "\x03",
			expected: []KeyRun{
				{Text: "C-c", Literal: false},
			},
		},
		{
			name:  "Ctrl-z",
			input: "\x1a",
			expected: []KeyRun{
				{Text: "C-z", Literal: false},
			},
		},

		// Meta/Alt combinations
		{
			name:  "Meta-Enter CR",
			input: "\x1b\r",
			expected: []KeyRun{
				{Text: "M-Enter", Literal: false},
			},
		},
		{
			name:  "Meta-Enter LF",
			input: "\x1b\n",
			expected: []KeyRun{
				{Text: "M-Enter", Literal: false},
			},
		},
		{
			name:  "Meta-Backspace DEL",
			input: "\x1b\x7f",
			expected: []KeyRun{
				{Text: "M-BSpace", Literal: false},
			},
		},
		{
			name:  "Meta-Backspace BS",
			input: "\x1b\b",
			expected: []KeyRun{
				{Text: "M-BSpace", Literal: false},
			},
		},

		// CSI special keys
		{
			name:  "Back Tab",
			input: "\x1b[Z",
			expected: []KeyRun{
				{Text: "BTab", Literal: false},
			},
		},
		{
			name:  "Home",
			input: "\x1b[H",
			expected: []KeyRun{
				{Text: "Home", Literal: false},
			},
		},
		{
			name:  "End",
			input: "\x1b[F",
			expected: []KeyRun{
				{Text: "End", Literal: false},
			},
		},
		{
			name:  "Insert",
			input: "\x1b[2~",
			expected: []KeyRun{
				{Text: "Insert", Literal: false},
			},
		},
		{
			name:  "Delete",
			input: "\x1b[3~",
			expected: []KeyRun{
				{Text: "DC", Literal: false},
			},
		},
		{
			name:  "PageUp",
			input: "\x1b[5~",
			expected: []KeyRun{
				{Text: "PageUp", Literal: false},
			},
		},
		{
			name:  "PageDown",
			input: "\x1b[6~",
			expected: []KeyRun{
				{Text: "PageDown", Literal: false},
			},
		},

		// SS3 function keys
		{
			name:  "F1",
			input: "\x1bOP",
			expected: []KeyRun{
				{Text: "F1", Literal: false},
			},
		},
		{
			name:  "F2",
			input: "\x1bOQ",
			expected: []KeyRun{
				{Text: "F2", Literal: false},
			},
		},
		{
			name:  "F3",
			input: "\x1bOR",
			expected: []KeyRun{
				{Text: "F3", Literal: false},
			},
		},
		{
			name:  "F4",
			input: "\x1bOS",
			expected: []KeyRun{
				{Text: "F4", Literal: false},
			},
		},

		// Unknown SS3 falls back to Escape
		{
			name:  "unknown SS3 sequence",
			input: "\x1bOX",
			expected: []KeyRun{
				{Text: "Escape", Literal: false},
				{Text: "OX", Literal: true},
			},
		},

		// Unknown CSI sequence is skipped silently
		{
			name:     "unknown CSI sequence skipped",
			input:    "\x1b[99~",
			expected: nil,
		},

		// Bare escape at end of input
		{
			name:  "bare escape at end",
			input: "\x1b",
			expected: []KeyRun{
				{Text: "Escape", Literal: false},
			},
		},

		// Bare escape with only one char following (not [ or O)
		{
			name:  "escape followed by non-sequence char",
			input: "\x1bx",
			expected: []KeyRun{
				{Text: "Escape", Literal: false},
				{Text: "x", Literal: true},
			},
		},

		// UTF-8 text as literal
		{
			name:  "UTF-8 accented",
			input: "héllo",
			expected: []KeyRun{
				{Text: "héllo", Literal: true},
			},
		},

		// Complex mixed input
		{
			name:  "text then arrows then text",
			input: "hi\x1b[Abye",
			expected: []KeyRun{
				{Text: "hi", Literal: true},
				{Text: "Up", Literal: false},
				{Text: "bye", Literal: true},
			},
		},
		{
			name:  "multiple specials in sequence",
			input: "\r\t\x7f",
			expected: []KeyRun{
				{Text: "Enter", Literal: false},
				{Text: "Tab", Literal: false},
				{Text: "BSpace", Literal: false},
			},
		},

		// Unknown CSI between text - text on both sides preserved
		{
			name:  "unknown CSI between text",
			input: "abc\x1b[99~def",
			expected: []KeyRun{
				{Text: "abc", Literal: true},
				{Text: "def", Literal: true},
			},
		},

		// Incomplete CSI sequence (no final byte): escape consumed,
		// then '[' is printable ASCII and becomes a literal run.
		{
			name:  "incomplete CSI at end of input",
			input: "\x1b[",
			expected: []KeyRun{
				{Text: "Escape", Literal: false},
				{Text: "[", Literal: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyKeyRuns(nil, tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("got %d runs, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.expected), got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("run[%d] = %+v, want %+v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestClassifyKeyRuns_PreallocatedDst(t *testing.T) {
	dst := make([]KeyRun, 0, 8)
	got := ClassifyKeyRuns(dst, "abc\rdef")

	if len(got) != 3 {
		t.Fatalf("expected 3 runs, got %d: %+v", len(got), got)
	}

	expected := []KeyRun{
		{Text: "abc", Literal: true},
		{Text: "Enter", Literal: false},
		{Text: "def", Literal: true},
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Errorf("run[%d] = %+v, want %+v", i, got[i], expected[i])
		}
	}

	// Verify the returned slice reuses the pre-allocated backing array.
	if cap(got) != cap(dst) {
		t.Errorf("expected dst backing array reuse (cap %d), got cap %d", cap(dst), cap(got))
	}
}
