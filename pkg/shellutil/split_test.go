package shellutil

import (
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "simple args",
			input: "echo hello world",
			want:  []string{"echo", "hello", "world"},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   \t  \n  ",
			want:  nil,
		},
		{
			name:  "single arg",
			input: "ls",
			want:  []string{"ls"},
		},
		{
			name:  "double quoted arg with spaces",
			input: `echo "hello world"`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "single quoted arg with spaces",
			input: `echo 'hello world'`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "backslash escape outside quotes",
			input: `echo hello\ world`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "backslash literal inside single quotes",
			input: `echo 'hello\nworld'`,
			want:  []string{"echo", `hello\nworld`},
		},
		{
			name:  "backslash escape inside double quotes",
			input: `echo "hello\"world"`,
			want:  []string{"echo", `hello"world`},
		},
		{
			name:  "single quote inside double quotes",
			input: `echo "it's fine"`,
			want:  []string{"echo", "it's fine"},
		},
		{
			name:  "double quote inside single quotes",
			input: `echo 'say "hi"'`,
			want:  []string{"echo", `say "hi"`},
		},
		{
			name:  "multiple spaces between args",
			input: "echo   hello   world",
			want:  []string{"echo", "hello", "world"},
		},
		{
			name:  "tabs as separators",
			input: "echo\thello\tworld",
			want:  []string{"echo", "hello", "world"},
		},
		{
			name:  "path with spaces",
			input: `cat "/Users/john doe/my file.txt"`,
			want:  []string{"cat", "/Users/john doe/my file.txt"},
		},
		{
			name:  "mixed quoting styles",
			input: `echo "hello" 'world' plain`,
			want:  []string{"echo", "hello", "world", "plain"},
		},
		{
			name:  "adjacent quotes concatenate",
			input: `echo "hello"'world'`,
			want:  []string{"echo", "helloworld"},
		},
		{
			name:    "unterminated double quote",
			input:   `echo "hello`,
			wantErr: true,
		},
		{
			name:    "unterminated single quote",
			input:   `echo 'hello`,
			wantErr: true,
		},
		{
			name:  "trailing backslash escape",
			input: `echo hello\`,
			want:  []string{"echo", "hello"},
		},
		{
			name:  "empty quoted strings",
			input: `echo "" ''`,
			want:  []string{"echo"},
		},
		{
			name:  "newline as separator",
			input: "echo\nhello",
			want:  []string{"echo", "hello"},
		},
		{
			name:  "quoted newline is literal",
			input: "echo \"hello\nworld\"",
			want:  []string{"echo", "hello\nworld"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Split(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Split(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Split(%q) unexpected error: %v", tt.input, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Split(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Split(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
