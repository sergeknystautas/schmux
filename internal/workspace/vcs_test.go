package workspace

import "testing"

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
