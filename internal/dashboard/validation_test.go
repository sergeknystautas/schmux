package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaseSensitiveFileExists(t *testing.T) {
	dir := t.TempDir()

	// Create files with specific casing
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Guide.md"), []byte("guide"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact match", "README.md", true},
		{"different case", "readme.md", false},
		{"mixed case mismatch", "GUIDE.md", false},
		{"exact match second file", "Guide.md", true},
		{"nonexistent file", "missing.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := caseSensitiveFileExists(dir, tt.filename)
			if got != tt.want {
				t.Errorf("caseSensitiveFileExists(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}

	t.Run("nonexistent directory returns false", func(t *testing.T) {
		got := caseSensitiveFileExists("/nonexistent/dir/12345", "file.txt")
		if got {
			t.Error("expected false for nonexistent directory")
		}
	})
}

func TestIsValidRepoName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"foo", true},
		{"foo.bar", true},
		{"foo-bar_baz", true},
		{"owner.repo", true},
		{"corp.org", true},
		{"", false},
		{"..", false},
		{".foo", false},
		{".", false},
		{"foo/bar", false},
		{"foo\\bar", false},
		{"foo\x00bar", false},
		{"foo:bar", false}, // colon not allowed (HTTP-perimeter validator only; see spec §2.3)
		{"foo+bar", false}, // plus not allowed
		{"foo@bar", false}, // at not allowed
	}
	for _, c := range cases {
		if got := isValidRepoName(c.in); got != c.want {
			t.Errorf("isValidRepoName(%q) = %v, want %v", c.in, got, c.want)
		}
	}

	// Length cap: 128 chars OK, 129 chars rejected
	long := make([]byte, 128)
	for i := range long {
		long[i] = 'a'
	}
	if !isValidRepoName(string(long)) {
		t.Errorf("128-char name rejected, want accepted")
	}
	if isValidRepoName(string(long) + "a") {
		t.Errorf("129-char name accepted, want rejected")
	}
}

func TestIsValidResourceID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"abc-123", true},
		{"session_id_42", true},
		{"", false},
		{".", false},
		{"..", false},
		{"foo.bar", false}, // dots rejected (different from isValidRepoName)
		{"foo/bar", false},
		{"foo\\bar", false},
		{"foo\x00bar", false},
	}
	for _, c := range cases {
		if got := isValidResourceID(c.in); got != c.want {
			t.Errorf("isValidResourceID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSplitWorkspaceFileParam(t *testing.T) {
	tests := []struct {
		param, wantWS, wantPath, wantErr string
	}{
		{"ws-1/docs%2Fnotes.md", "ws-1", "docs/notes.md", ""},
		{"ws-1/percent%252Fname.md", "ws-1", "percent%2Fname.md", ""},
		{"ws-1/with%20space.md", "ws-1", "with space.md", ""},
		{"", "", "", "workspace ID is required"},
		{"ws-1", "", "", "invalid path format"},
		{"/notes.md", "", "", "invalid path format"},
		{"ws-1/%zz", "", "", "invalid file path"},
	}
	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			ws, p, errMsg := splitWorkspaceFileParam(tt.param)
			if ws != tt.wantWS || p != tt.wantPath || errMsg != tt.wantErr {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", ws, p, errMsg, tt.wantWS, tt.wantPath, tt.wantErr)
			}
		})
	}
}

func TestIsMarkdownPath(t *testing.T) {
	for _, p := range []string{"a.md", "A.MD", "docs/x.mdx", "x.Mdx"} {
		if !isMarkdownPath(p) {
			t.Errorf("%q should be markdown", p)
		}
	}
	for _, p := range []string{"a.mmd", "a.markdown", "md", "a.md.txt", ""} {
		if isMarkdownPath(p) {
			t.Errorf("%q should not be markdown", p)
		}
	}
}
