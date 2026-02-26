package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string truncated with ellipsis",
			input:  "hello world this is a long string",
			maxLen: 10,
			want:   "hello w...",
		},
		{
			name:   "empty string unchanged",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "maxLen 3 keeps ellipsis",
			input:  "abcdef",
			maxLen: 3,
			want:   "abc",
		},
		{
			name:   "maxLen 4 truncates with ellipsis",
			input:  "abcdef",
			maxLen: 4,
			want:   "a...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestExtractConflictHunks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		content   string
		wantCount int
		wantCheck func(t *testing.T, hunks []string)
	}{
		{
			name:      "no conflicts",
			content:   "line one\nline two\nline three\n",
			wantCount: 0,
		},
		{
			name:      "single conflict",
			content:   "before 1\nbefore 2\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\nafter 1\nafter 2\n",
			wantCount: 1,
			wantCheck: func(t *testing.T, hunks []string) {
				// Should include 2 context lines before and after
				if !strings.Contains(hunks[0], "before 1") {
					t.Error("expected context line 'before 1'")
				}
				if !strings.Contains(hunks[0], "<<<<<<< HEAD") {
					t.Error("expected conflict start marker")
				}
				if !strings.Contains(hunks[0], ">>>>>>> branch") {
					t.Error("expected conflict end marker")
				}
				if !strings.Contains(hunks[0], "after 1") {
					t.Error("expected context line 'after 1'")
				}
				if !strings.Contains(hunks[0], "after 2") {
					t.Error("expected context line 'after 2'")
				}
			},
		},
		{
			name:      "conflict at file start (no context before)",
			content:   "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\nafter\n",
			wantCount: 1,
			wantCheck: func(t *testing.T, hunks []string) {
				if !strings.HasPrefix(hunks[0], "<<<<<<< HEAD") {
					t.Errorf("hunk should start with conflict marker, got: %q", hunks[0][:30])
				}
			},
		},
		{
			name:      "conflict at file end (limited context after)",
			content:   "before\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n",
			wantCount: 1,
			wantCheck: func(t *testing.T, hunks []string) {
				if !strings.Contains(hunks[0], "before") {
					t.Error("expected context line 'before'")
				}
				if !strings.Contains(hunks[0], ">>>>>>> branch") {
					t.Error("expected conflict end marker")
				}
			},
		},
		{
			name:      "multiple conflicts",
			content:   "a\nb\n<<<<<<< HEAD\nfirst ours\n=======\nfirst theirs\n>>>>>>> b\nc\nd\ne\nf\n<<<<<<< HEAD\nsecond ours\n=======\nsecond theirs\n>>>>>>> b\ng\nh\n",
			wantCount: 2,
			wantCheck: func(t *testing.T, hunks []string) {
				if !strings.Contains(hunks[0], "first ours") {
					t.Error("first hunk should contain 'first ours'")
				}
				if !strings.Contains(hunks[1], "second ours") {
					t.Error("second hunk should contain 'second ours'")
				}
			},
		},
		{
			name:      "empty file",
			content:   "",
			wantCount: 0,
		},
		{
			name:      "unclosed conflict marker (no end marker)",
			content:   "<<<<<<< HEAD\nours\n=======\ntheirs\n",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			hunks, err := extractConflictHunks(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(hunks) != tt.wantCount {
				t.Fatalf("got %d hunks, want %d", len(hunks), tt.wantCount)
			}
			if tt.wantCheck != nil {
				tt.wantCheck(t, hunks)
			}
		})
	}
}

func TestExtractConflictHunks_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := extractConflictHunks("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestTruncateOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short string unchanged",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "whitespace trimmed",
			input: "  hello  \n",
			want:  "hello",
		},
		{
			name:  "exactly 300 chars unchanged",
			input: strings.Repeat("a", 300),
			want:  strings.Repeat("a", 300),
		},
		{
			name:  "over 300 chars truncated",
			input: strings.Repeat("b", 400),
			want:  strings.Repeat("b", 297) + "...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateOutput(tt.input)
			if got != tt.want {
				t.Errorf("truncateOutput() length = %d, want %d", len(got), len(tt.want))
			}
		})
	}
}

// TestLinearSyncFromDefault_RejectsOrphanDefaultBranch is superseded by
// TestLinearSyncFromDefault_OrphanDefaultBranch in linear_sync_from_default_test.go
// which uses the fixture builder. Removed to avoid duplication.
