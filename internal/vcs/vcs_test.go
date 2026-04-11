package vcs

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewCommandBuilder(t *testing.T) {
	tests := []struct {
		vcsType  string
		wantType string
	}{
		{"git", "*vcs.GitCommandBuilder"},
		{"", "*vcs.GitCommandBuilder"},
		{"unknown", "*vcs.GitCommandBuilder"},
		{"sapling", "*vcs.SaplingCommandBuilder"},
	}
	for _, tt := range tests {
		cb := NewCommandBuilder(tt.vcsType)
		switch tt.wantType {
		case "*vcs.GitCommandBuilder":
			if _, ok := cb.(*GitCommandBuilder); !ok {
				t.Errorf("NewCommandBuilder(%q) = %T, want *GitCommandBuilder", tt.vcsType, cb)
			}
		case "*vcs.SaplingCommandBuilder":
			if _, ok := cb.(*SaplingCommandBuilder); !ok {
				t.Errorf("NewCommandBuilder(%q) = %T, want *SaplingCommandBuilder", tt.vcsType, cb)
			}
		}
	}
}

func TestGitDiffNumstat(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.DiffNumstat()
	want := "git diff HEAD --numstat --find-renames --diff-filter=ADM"
	if got != want {
		t.Errorf("DiffNumstat() = %q, want %q", got, want)
	}
}

func TestSaplingDiffNumstat(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.DiffNumstat()
	want := "sl status --no-status -mad | while IFS= read -r f; do printf '0\\t0\\t%s\\n' \"$f\"; done"
	if got != want {
		t.Errorf("DiffNumstat() = %q, want %q", got, want)
	}
}

func TestGitShowFile(t *testing.T) {
	tests := []struct {
		path     string
		revision string
		want     string
	}{
		{"main.go", "HEAD", "git show 'HEAD:main.go'"},
		{"src/app.ts", "abc123", "git show 'abc123:src/app.ts'"},
		{"path with spaces/file.go", "HEAD~1", "git show 'HEAD~1:path with spaces/file.go'"},
		{"file.go", "it's-a-ref", "git show 'it'\\''s-a-ref:file.go'"},
	}
	cb := &GitCommandBuilder{}
	for _, tt := range tests {
		got := cb.ShowFile(tt.path, tt.revision)
		if got != tt.want {
			t.Errorf("ShowFile(%q, %q) = %q, want %q", tt.path, tt.revision, got, tt.want)
		}
	}
}

func TestSaplingShowFile(t *testing.T) {
	tests := []struct {
		path     string
		revision string
		want     string
	}{
		// HEAD should be translated to . (working copy parent)
		{"main.go", "HEAD", "sl cat --pager never -r '.' 'main.go' | head -2000"},
		// Non-HEAD revisions pass through
		{"src/app.ts", "abc123", "sl cat --pager never -r 'abc123' 'src/app.ts' | head -2000"},
		{"file.go", "deadbeef", "sl cat --pager never -r 'deadbeef' 'file.go' | head -2000"},
	}
	cb := &SaplingCommandBuilder{}
	for _, tt := range tests {
		got := cb.ShowFile(tt.path, tt.revision)
		if got != tt.want {
			t.Errorf("ShowFile(%q, %q) = %q, want %q", tt.path, tt.revision, got, tt.want)
		}
	}
}

func TestGitFileContent(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.FileContent("src/main.go")
	if got != "head -2000 'src/main.go'" {
		t.Errorf("FileContent() = %q, want %q", got, "head -2000 'src/main.go'")
	}
}

func TestSaplingFileContent(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.FileContent("src/main.go")
	if got != "head -2000 'src/main.go'" {
		t.Errorf("FileContent() = %q, want %q", got, "head -2000 'src/main.go'")
	}
}

func TestGitUntrackedFiles(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.UntrackedFiles()
	if got != "git ls-files --others --exclude-standard" {
		t.Errorf("UntrackedFiles() = %q", got)
	}
}

func TestSaplingUntrackedFiles(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.UntrackedFiles()
	if got != "sl status --unknown --no-status" {
		t.Errorf("UntrackedFiles() = %q", got)
	}
}

func TestGitLog(t *testing.T) {
	tests := []struct {
		name     string
		refs     []string
		maxCount int
		wantPfx  string
		wantRefs []string
	}{
		{
			name:     "single HEAD ref",
			refs:     []string{"HEAD"},
			maxCount: 50,
			wantPfx:  "git log --format=%H|%h|%s|%an|%aI|%P --topo-order --max-count=50",
			wantRefs: []string{"'HEAD'"},
		},
		{
			name:     "multiple refs",
			refs:     []string{"HEAD", "origin/main"},
			maxCount: 100,
			wantPfx:  "git log --format=%H|%h|%s|%an|%aI|%P --topo-order --max-count=100",
			wantRefs: []string{"'HEAD'", "'origin/main'"},
		},
		{
			name:     "no refs",
			refs:     nil,
			maxCount: 10,
			wantPfx:  "git log --format=%H|%h|%s|%an|%aI|%P --topo-order --max-count=10",
			wantRefs: nil,
		},
	}
	cb := &GitCommandBuilder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cb.Log(tt.refs, tt.maxCount)
			if !strings.HasPrefix(got, tt.wantPfx) {
				t.Errorf("Log() prefix mismatch\ngot:  %q\nwant: %q", got, tt.wantPfx)
			}
			for _, ref := range tt.wantRefs {
				if !strings.Contains(got, ref) {
					t.Errorf("Log() missing ref %q in %q", ref, got)
				}
			}
		})
	}
}

func TestSaplingLog(t *testing.T) {
	cb := &SaplingCommandBuilder{}

	t.Run("HEAD ref uses efficient --limit", func(t *testing.T) {
		got := cb.Log([]string{"HEAD"}, 50)
		if !strings.Contains(got, "sl log") {
			t.Errorf("should start with 'sl log': %q", got)
		}
		if !strings.Contains(got, "--limit 50") {
			t.Errorf("HEAD should use --limit, got %q", got)
		}
		if strings.Contains(got, "ancestors") {
			t.Errorf("HEAD should NOT use ancestors() revset (O(n) on large repos), got %q", got)
		}
	})

	t.Run("no refs uses efficient --limit", func(t *testing.T) {
		got := cb.Log(nil, 10)
		if !strings.Contains(got, "--limit 10") {
			t.Errorf("nil refs should use --limit, got %q", got)
		}
	})

	t.Run("single non-HEAD ref uses revset", func(t *testing.T) {
		got := cb.Log([]string{"feature-branch"}, 50)
		wantRev := "ancestors(feature-branch)"
		if !strings.Contains(got, wantRev) {
			t.Errorf("missing revset %q in %q", wantRev, got)
		}
		wantLast := fmt.Sprintf("last(%s, %d)", wantRev, 50)
		if !strings.Contains(got, wantLast) {
			t.Errorf("should use last() for revset, want %q in %q", wantLast, got)
		}
	})

	t.Run("multiple refs joined with +", func(t *testing.T) {
		got := cb.Log([]string{"HEAD", "origin/main"}, 100)
		wantRev := "ancestors(HEAD+origin/main)"
		if !strings.Contains(got, wantRev) {
			t.Errorf("missing revset %q in %q", wantRev, got)
		}
		wantLast := fmt.Sprintf("last(%s, %d)", wantRev, 100)
		if !strings.Contains(got, wantLast) {
			t.Errorf("should use last() for revset, want %q in %q", wantLast, got)
		}
	})

	t.Run("all cases include parent template", func(t *testing.T) {
		for _, refs := range [][]string{{"HEAD"}, nil, {"feature"}, {"HEAD", "origin/main"}} {
			got := cb.Log(refs, 5)
			if !strings.Contains(got, "{p1node} {p2node}") {
				t.Errorf("should use {p1node} {p2node} for refs=%v: %q", refs, got)
			}
		}
	})
}

func TestGitLogRange(t *testing.T) {
	tests := []struct {
		name      string
		refs      []string
		forkPoint string
		wantNot   string
	}{
		{
			name:      "single ref with fork point",
			refs:      []string{"HEAD"},
			forkPoint: "abc123",
			wantNot:   "--not 'abc123^'",
		},
		{
			name:      "multiple refs",
			refs:      []string{"HEAD", "feature"},
			forkPoint: "def456",
			wantNot:   "--not 'def456^'",
		},
	}
	cb := &GitCommandBuilder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cb.LogRange(tt.refs, tt.forkPoint)
			if !strings.HasPrefix(got, "git log") {
				t.Errorf("LogRange() should start with 'git log': %q", got)
			}
			if !strings.Contains(got, tt.wantNot) {
				t.Errorf("LogRange() missing --not clause %q in %q", tt.wantNot, got)
			}
			for _, ref := range tt.refs {
				if !strings.Contains(got, "'"+ref+"'") {
					t.Errorf("LogRange() missing quoted ref %q in %q", ref, got)
				}
			}
		})
	}
}

func TestSaplingLogRange(t *testing.T) {
	tests := []struct {
		name      string
		refs      []string
		forkPoint string
		wantRev   string
	}{
		{
			name:      "HEAD translated to .",
			refs:      []string{"HEAD"},
			forkPoint: "abc123",
			wantRev:   "(abc123)::.",
		},
		{
			name:      "non-HEAD ref preserved",
			refs:      []string{"feature"},
			forkPoint: "abc123",
			wantRev:   "(abc123)::feature",
		},
		{
			name:      "multiple refs joined with +",
			refs:      []string{"HEAD", "feature"},
			forkPoint: "def456",
			wantRev:   "(def456)::.+feature",
		},
	}
	cb := &SaplingCommandBuilder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cb.LogRange(tt.refs, tt.forkPoint)
			if !strings.Contains(got, "sl log") {
				t.Errorf("LogRange() should use 'sl log': %q", got)
			}
			if !strings.Contains(got, tt.wantRev) {
				t.Errorf("LogRange() missing revset %q in %q", tt.wantRev, got)
			}
		})
	}
}

func TestGitResolveRef(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.ResolveRef("HEAD")
	if got != "git rev-parse --verify 'HEAD'" {
		t.Errorf("ResolveRef() = %q", got)
	}
	got = cb.ResolveRef("abc123")
	if got != "git rev-parse --verify 'abc123'" {
		t.Errorf("ResolveRef() = %q", got)
	}
}

func TestSaplingResolveRef(t *testing.T) {
	tests := []struct {
		ref     string
		wantRef string
	}{
		{"HEAD", "."}, // HEAD translated to .
		{"abc123", "abc123"},
	}
	cb := &SaplingCommandBuilder{}
	for _, tt := range tests {
		got := cb.ResolveRef(tt.ref)
		if !strings.Contains(got, "'"+tt.wantRef+"'") {
			t.Errorf("ResolveRef(%q) missing ref %q in %q", tt.ref, tt.wantRef, got)
		}
		if !strings.Contains(got, "sl log") {
			t.Errorf("ResolveRef(%q) should use 'sl log': %q", tt.ref, got)
		}
	}
}

func TestGitMergeBase(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.MergeBase("HEAD", "origin/main")
	want := "git merge-base 'HEAD' 'origin/main'"
	if got != want {
		t.Errorf("MergeBase() = %q, want %q", got, want)
	}
}

func TestSaplingMergeBase(t *testing.T) {
	tests := []struct {
		ref1, ref2 string
		wantRev    string
	}{
		{"HEAD", "remote/main", "ancestor(., remote/main)"},
		{"abc123", "def456", "ancestor(abc123, def456)"},
	}
	cb := &SaplingCommandBuilder{}
	for _, tt := range tests {
		got := cb.MergeBase(tt.ref1, tt.ref2)
		if !strings.Contains(got, tt.wantRev) {
			t.Errorf("MergeBase(%q, %q) missing revset %q in %q", tt.ref1, tt.ref2, tt.wantRev, got)
		}
	}
}

func TestGitDefaultBranchRef(t *testing.T) {
	cb := &GitCommandBuilder{}
	if got := cb.DefaultBranchRef("main"); got != "origin/main" {
		t.Errorf("DefaultBranchRef(main) = %q, want origin/main", got)
	}
	if got := cb.DefaultBranchRef("master"); got != "origin/master" {
		t.Errorf("DefaultBranchRef(master) = %q, want origin/master", got)
	}
}

func TestSaplingDefaultBranchRef(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	if got := cb.DefaultBranchRef("main"); got != "remote/main" {
		t.Errorf("DefaultBranchRef(main) = %q, want remote/main", got)
	}
}

func TestGitDetectDefaultBranch(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.DetectDefaultBranch()
	if !strings.Contains(got, "symbolic-ref") {
		t.Errorf("DetectDefaultBranch() should use symbolic-ref: %q", got)
	}
	if !strings.Contains(got, "origin/HEAD") {
		t.Errorf("DetectDefaultBranch() should reference origin/HEAD: %q", got)
	}
}

func TestSaplingDetectDefaultBranch(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.DetectDefaultBranch()
	if !strings.Contains(got, "sl config remotenames.selectivepulldefault") {
		t.Errorf("DetectDefaultBranch() should query sl config: %q", got)
	}
	if !strings.Contains(got, "echo main") {
		t.Errorf("DetectDefaultBranch() should fallback to 'main': %q", got)
	}
}

func TestGitRevListCount(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.RevListCount("HEAD..origin/main")
	want := "git rev-list --count 'HEAD..origin/main'"
	if got != want {
		t.Errorf("RevListCount() = %q, want %q", got, want)
	}
}

func TestParseRangeToRevset(t *testing.T) {
	tests := []struct {
		input                    string
		wantExclude, wantInclude string
	}{
		{"HEAD..origin/main", ".", "origin/main"},
		{"abc123..def456", "abc123", "def456"},
		{"notarange", "", "notarange"},
	}
	for _, tt := range tests {
		exc, inc := parseRangeToRevset(tt.input)
		if exc != tt.wantExclude || inc != tt.wantInclude {
			t.Errorf("parseRangeToRevset(%q) = (%q, %q), want (%q, %q)",
				tt.input, exc, inc, tt.wantExclude, tt.wantInclude)
		}
	}
}

func TestSaplingRevListCount(t *testing.T) {
	tests := []struct {
		name      string
		rangeSpec string
		wantRev   string
	}{
		{
			name:      "HEAD..origin/main translates HEAD to .",
			rangeSpec: "HEAD..origin/main",
			wantRev:   "only(origin/main, .)",
		},
		{
			name:      "non-HEAD range",
			rangeSpec: "abc123..def456",
			wantRev:   "only(def456, abc123)",
		},
		{
			name:      "no .. in range falls back to raw",
			rangeSpec: "some-revset",
			wantRev:   "some-revset",
		},
	}
	cb := &SaplingCommandBuilder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cb.RevListCount(tt.rangeSpec)
			if !strings.Contains(got, tt.wantRev) {
				t.Errorf("RevListCount(%q) missing revset %q in %q", tt.rangeSpec, tt.wantRev, got)
			}
			if !strings.Contains(got, "wc -l") {
				t.Errorf("RevListCount(%q) should pipe to wc -l: %q", tt.rangeSpec, got)
			}
		})
	}
}

func TestGitNewestTimestamp(t *testing.T) {
	cb := &GitCommandBuilder{}
	got := cb.NewestTimestamp("HEAD..origin/main")
	want := "git log --format=%aI -1 'HEAD..origin/main'"
	if got != want {
		t.Errorf("NewestTimestamp() = %q, want %q", got, want)
	}
}

func TestSaplingNewestTimestamp(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.NewestTimestamp("HEAD..origin/main")
	if !strings.Contains(got, "last(only(origin/main, .))") {
		t.Errorf("NewestTimestamp(HEAD..origin/main) = %q, want revset with last(only(origin/main, .))", got)
	}
	if !strings.Contains(got, "--limit 1") {
		t.Errorf("NewestTimestamp() = %q, want --limit 1", got)
	}
}

func TestSaplingLogRange_HasLimit(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.LogRange([]string{"HEAD"}, "abc123")
	if !strings.Contains(got, "last(") {
		t.Errorf("LogRange() = %q, want last() to prevent unbounded output", got)
	}
}

func TestSaplingShowFile_HEADMapping(t *testing.T) {
	cb := &SaplingCommandBuilder{}
	got := cb.ShowFile("foo.txt", "HEAD")
	// In Sapling, "." is the working copy parent (equivalent to git HEAD).
	// ".^" would be the grandparent — one commit too far back.
	if !strings.Contains(got, "-r '.'") {
		t.Errorf("ShowFile(foo.txt, HEAD) = %q, want command with -r '.' (not '.^')", got)
	}
	if strings.Contains(got, ".^") {
		t.Errorf("ShowFile(foo.txt, HEAD) = %q, must NOT contain '.^'", got)
	}
}
