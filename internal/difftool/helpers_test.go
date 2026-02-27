package difftool

import (
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty string", input: "", want: nil},
		{name: "single line no newline", input: "hello", want: []string{"hello"}},
		{name: "single line with newline", input: "hello\n", want: []string{"hello"}},
		{name: "two lines", input: "a\nb\n", want: []string{"a", "b"}},
		{name: "two lines no trailing newline", input: "a\nb", want: []string{"a", "b"}},
		{name: "just a newline", input: "\n", want: []string{""}},
		{name: "multiple newlines", input: "\n\n\n", want: []string{"", "", ""}},
		{name: "blank line in middle", input: "a\n\nb\n", want: []string{"a", "", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitLines(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCountTrailingContext(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int
	}{
		{name: "empty slice", lines: []string{}, want: 0},
		{name: "all context", lines: []string{" a", " b", " c"}, want: 3},
		{name: "no context", lines: []string{"+a", "-b"}, want: 0},
		{name: "trailing context after changes", lines: []string{"+a", "-b", " c", " d"}, want: 2},
		{name: "single context line", lines: []string{"-x", " y"}, want: 1},
		{name: "context interrupted", lines: []string{" a", "+b", " c"}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countTrailingContext(tt.lines)
			if got != tt.want {
				t.Errorf("countTrailingContext(%v) = %d, want %d", tt.lines, got, tt.want)
			}
		})
	}
}

func TestComputeLCS(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{name: "both empty", a: nil, b: nil, want: []string{}},
		{name: "identical slices", a: []string{"a", "b", "c"}, b: []string{"a", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "completely different", a: []string{"a", "b"}, b: []string{"x", "y"}, want: []string{}},
		{name: "classic LCS", a: []string{"a", "b", "c", "d"}, b: []string{"b", "d", "e"}, want: []string{"b", "d"}},
		{name: "first empty", a: []string{}, b: []string{"a", "b"}, want: []string{}},
		{name: "second empty", a: []string{"a", "b"}, b: []string{}, want: []string{}},
		{name: "single common element", a: []string{"a", "x", "b"}, b: []string{"c", "x", "d"}, want: []string{"x"}},
		{name: "subsequence not substring", a: []string{"a", "b", "c", "d"}, b: []string{"a", "c"}, want: []string{"a", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeLCS(tt.a, tt.b)
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("computeLCS(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestComputeLCS_MemoryGuard(t *testing.T) {
	// Create large inputs that exceed the maxCells threshold (10_000_000)
	// We need (len(a)+1) * (len(b)+1) > 10_000_000
	// 3163 * 3163 = ~10_003_769 > 10_000_000
	large := make([]string, 3163)
	for i := range large {
		large[i] = "line"
	}
	got := computeLCS(large, large)
	if got != nil {
		t.Errorf("computeLCS with large inputs should return nil (memory guard), got %d elements", len(got))
	}
}

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "empty", data: []byte{}, want: false},
		{name: "text content", data: []byte("hello world\n"), want: false},
		{name: "binary with null byte", data: []byte{0x48, 0x65, 0x00, 0x6c}, want: true},
		{name: "null byte at start", data: []byte{0x00, 0x41}, want: true},
		{name: "all nulls", data: []byte{0x00, 0x00, 0x00}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBinaryContent(tt.data)
			if got != tt.want {
				t.Errorf("isBinaryContent(%v) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}
