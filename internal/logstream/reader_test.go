package logstream

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertLines(t *testing.T, got [][]byte, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d (%q)", len(got), len(want), got)
	}
	for i, w := range want {
		if string(got[i]) != w {
			t.Fatalf("line %d = %q, want %q", i, got[i], w)
		}
	}
}

func TestReadPageBefore_PagesNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	// Includes a blank line and has no final newline.
	if err := os.WriteFile(path, []byte("zero\n\none\ntwo\nthree\nfour"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	first, err := readPageBefore(path, info.Size(), 3)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, first.Lines, "four", "three", "two")
	if !first.HasMore {
		t.Fatal("first page should report older history")
	}

	second, err := readPageBefore(path, first.Before, 3)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, second.Lines, "one", "zero")
	if second.HasMore {
		t.Fatal("second page should exhaust history")
	}
}

func TestReadPageBefore_TableCases(t *testing.T) {
	cases := []struct {
		name       string
		contents   string
		startAt    int64 // boundary offset; -1 = use file size, 0 = literal zero
		limit      int
		want       []string
		wantMore   bool
		wantBefore int64
	}{
		{
			name:       "small file at EOF returns all lines newest-first with no more",
			contents:   "a\nb\nc\n",
			startAt:    -1,
			limit:      3,
			want:       []string{"c", "b", "a"},
			wantMore:   false,
			wantBefore: 0,
		},
		{
			name:       "boundary zero returns empty page",
			contents:   "a\nb\nc\n",
			startAt:    0,
			limit:      3,
			want:       nil,
			wantMore:   false,
			wantBefore: 0,
		},
		{
			name:     "ignored blank lines and missing final newline",
			contents: "zero\n\none\ntwo",
			startAt:  -1,
			limit:    2,
			want:     []string{"two", "one"},
			wantMore: true,
		},
		{
			name:     "page boundary splits a partial leading line",
			contents: "alpha\nbeta\ngamma\n",
			startAt:  -1,
			limit:    1,
			want:     []string{"gamma"},
			wantMore: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.log")
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			before := tc.startAt
			if before == -1 {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				before = info.Size()
			}
			page, err := readPageBefore(path, before, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.want) == 0 {
				if len(page.Lines) != 0 {
					t.Fatalf("expected empty page, got %q", page.Lines)
				}
			} else {
				assertLines(t, page.Lines, tc.want...)
			}
			if page.HasMore != tc.wantMore {
				t.Fatalf("HasMore = %v, want %v", page.HasMore, tc.wantMore)
			}
			if tc.wantBefore != 0 && page.Before != tc.wantBefore {
				t.Fatalf("Before = %d, want %d", page.Before, tc.wantBefore)
			}
		})
	}
}

func TestReadPageBefore_RejectsOverlongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.log")
	huge := strings.Repeat("x", maxLineSize+1)
	contents := "ok\n" + huge + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	_, err := readPageBefore(path, info.Size(), 3)
	if err == nil {
		t.Fatal("expected error for overlong line, got nil")
	}
	if !errors.Is(err, errLineTooLong) {
		t.Fatalf("err = %v, want errLineTooLong", err)
	}
}

func TestReadPageBefore_MissingFileReturnsEmptyPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.log")
	page, err := readPageBefore(path, 1024, 3)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(page.Lines) != 0 || page.HasMore || page.Before != 0 {
		t.Fatalf("missing file should return empty terminal page, got %+v", page)
	}
}

func TestReadPageBefore_BoundaryZeroReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := readPageBefore(path, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Lines) != 0 || page.HasMore || page.Before != 0 {
		t.Fatalf("zero boundary should return empty terminal page, got %+v", page)
	}
}

func TestReadPageBefore_DuplicateLinesKeepTheirOwnOffsets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicates.log")
	if err := os.WriteFile(path, []byte("dup\ndup\ndup\ndup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	first, err := readPageBefore(path, info.Size(), 2)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, first.Lines, "dup", "dup")
	if !first.HasMore {
		t.Fatal("first page should report older duplicate lines")
	}

	second, err := readPageBefore(path, first.Before, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, second.Lines, "dup", "dup")
	if second.HasMore {
		t.Fatal("second page should exhaust duplicate lines")
	}
}

func TestReadPageBefore_TracksOffsetForLineCrossingChunkBoundary(t *testing.T) {
	longLine := strings.Repeat("x", readChunkSize+17)
	path := filepath.Join(t.TempDir(), "chunk-boundary.log")
	if err := os.WriteFile(path, []byte("old\n"+longLine+"\nnew\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	first, err := readPageBefore(path, info.Size(), 1)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, first.Lines, "new")

	second, err := readPageBefore(path, first.Before, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, second.Lines, longLine)

	third, err := readPageBefore(path, second.Before, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, third.Lines, "old")
	if third.HasMore {
		t.Fatal("third page should exhaust history")
	}
}

func TestReadPageBefore_PreservesWhitespaceOnlyLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whitespace.log")
	if err := os.WriteFile(path, []byte("old\n   \nnew\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	page, err := readPageBefore(path, info.Size(), 3)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, page.Lines, "new", "   ", "old")
}
