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
