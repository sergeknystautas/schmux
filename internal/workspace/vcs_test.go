package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasVCSSupport(t *testing.T) {
	tests := []struct {
		vcs  string
		want bool
	}{
		{"", true},
		{"git", true},
		{"git-worktree", true},
		{"git-clone", true},
		{"sapling", true},
		{"mercurial", false},
		{"svn", false},
	}
	for _, tt := range tests {
		if got := HasVCSSupport(tt.vcs); got != tt.want {
			t.Errorf("HasVCSSupport(%q) = %v, want %v", tt.vcs, got, tt.want)
		}
	}
}

func TestIsGitVCS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		vcs  string
		want bool
	}{
		{"empty string defaults to git", "", true},
		{"git", "git", true},
		{"git-worktree", "git-worktree", true},
		{"git-clone", "git-clone", true},
		{"sapling is not git", "sapling", false},
		{"unknown is not git", "unknown", false},
		{"mercurial is not git", "mercurial", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsGitVCS(tt.vcs)
			if got != tt.want {
				t.Errorf("IsGitVCS(%q) = %v, want %v", tt.vcs, got, tt.want)
			}
		})
	}
}

func TestHasVCSMetadata(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Empty directory — no VCS metadata
	if hasVCSMetadata(tmpDir, "") {
		t.Error("empty directory should not have VCS metadata")
	}
	if hasVCSMetadata(tmpDir, "git") {
		t.Error("empty directory should not have git metadata")
	}
	if hasVCSMetadata(tmpDir, "sapling") {
		t.Error("empty directory should not have sapling metadata")
	}

	// Directory with .git file (worktree)
	gitFile := filepath.Join(tmpDir, "worktree-ws")
	os.MkdirAll(gitFile, 0755)
	os.WriteFile(filepath.Join(gitFile, ".git"), []byte("gitdir: /some/path"), 0644)
	if !hasVCSMetadata(gitFile, "") {
		t.Error("directory with .git file should have VCS metadata")
	}
	if !hasVCSMetadata(gitFile, "git-worktree") {
		t.Error("directory with .git file should have git-worktree metadata")
	}

	// Directory with .git directory (clone)
	cloneDir := filepath.Join(tmpDir, "clone-ws")
	os.MkdirAll(filepath.Join(cloneDir, ".git"), 0755)
	if !hasVCSMetadata(cloneDir, "git-clone") {
		t.Error("directory with .git dir should have git-clone metadata")
	}

	// Directory with .sl directory (sapling)
	slDir := filepath.Join(tmpDir, "sl-ws")
	os.MkdirAll(filepath.Join(slDir, ".sl"), 0755)
	if !hasVCSMetadata(slDir, "sapling") {
		t.Error("directory with .sl dir should have sapling metadata")
	}
	if hasVCSMetadata(slDir, "git") {
		t.Error("sapling directory should not have git metadata")
	}
}
