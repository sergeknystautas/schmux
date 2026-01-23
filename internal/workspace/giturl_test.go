package workspace

import (
	"context"
	"testing"
)

func TestBuildGitBranchURL(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		branch    string
		want      string
		wantEmpty bool
	}{
		// GitHub HTTPS URLs
		{
			name:    "GitHub HTTPS with main branch",
			repoURL: "https://github.com/user/repo.git",
			branch:  "main",
			want:    "https://github.com/user/repo/tree/main",
		},
		{
			name:    "GitHub HTTPS without .git",
			repoURL: "https://github.com/user/repo",
			branch:  "develop",
			want:    "https://github.com/user/repo/tree/develop",
		},
		{
			name:    "GitHub HTTPS with feature branch",
			repoURL: "https://github.com/user/repo.git",
			branch:  "feature/new-stuff",
			want:    "https://github.com/user/repo/tree/feature/new-stuff",
		},
		// GitHub SSH URLs
		{
			name:    "GitHub SSH with main branch",
			repoURL: "git@github.com:user/repo.git",
			branch:  "main",
			want:    "https://github.com/user/repo/tree/main",
		},
		{
			name:    "GitHub SSH without .git",
			repoURL: "git@github.com:user/repo",
			branch:  "develop",
			want:    "https://github.com/user/repo/tree/develop",
		},
		// GitLab HTTPS URLs
		{
			name:    "GitLab HTTPS with main",
			repoURL: "https://gitlab.com/user/repo.git",
			branch:  "main",
			want:    "https://gitlab.com/user/repo/-/tree/main",
		},
		{
			name:    "GitLab HTTPS without .git",
			repoURL: "https://gitlab.com/user/repo",
			branch:  "feature",
			want:    "https://gitlab.com/user/repo/-/tree/feature",
		},
		// GitLab SSH URLs
		{
			name:    "GitLab SSH",
			repoURL: "git@gitlab.com:user/repo.git",
			branch:  "main",
			want:    "https://gitlab.com/user/repo/-/tree/main",
		},
		// Bitbucket HTTPS URLs
		{
			name:    "Bitbucket HTTPS",
			repoURL: "https://bitbucket.org/user/repo.git",
			branch:  "main",
			want:    "https://bitbucket.org/user/repo/src/main",
		},
		{
			name:    "Bitbucket HTTPS without .git",
			repoURL: "https://bitbucket.org/user/repo",
			branch:  "develop",
			want:    "https://bitbucket.org/user/repo/src/develop",
		},
		// Bitbucket SSH URLs
		{
			name:    "Bitbucket SSH",
			repoURL: "git@bitbucket.org:user/repo.git",
			branch:  "main",
			want:    "https://bitbucket.org/user/repo/src/main",
		},
		// Branch name encoding
		{
			name:    "Branch with slash",
			repoURL: "https://github.com/user/repo.git",
			branch:  "feature/new-stuff",
			want:    "https://github.com/user/repo/tree/feature/new-stuff",
		},
		{
			name:    "Branch with spaces encoded",
			repoURL: "https://github.com/user/repo.git",
			branch:  "feature/new stuff",
			want:    "https://github.com/user/repo/tree/feature/new%20stuff",
		},
		// Edge cases - should return empty string
		{
			name:      "Empty repo URL",
			repoURL:   "",
			branch:    "main",
			wantEmpty: true,
		},
		{
			name:      "Empty branch",
			repoURL:   "https://github.com/user/repo.git",
			branch:    "",
			wantEmpty: true,
		},
		{
			name:      "Malformed SSH URL missing colon",
			repoURL:   "git@github.com/user/repo.git",
			branch:    "main",
			wantEmpty: true,
		},
		{
			name:      "Malformed SSH URL missing parts",
			repoURL:   "git@github.com:user",
			branch:    "main",
			wantEmpty: true,
		},
		{
			name:      "Invalid HTTPS URL",
			repoURL:   "://not-a-url",
			branch:    "main",
			wantEmpty: true,
		},
		// Generic/hosted Git (fallback pattern)
		{
			name:    "Generic Git host",
			repoURL: "https://gitea.example.com/user/repo.git",
			branch:  "main",
			want:    "https://gitea.example.com/user/repo/tree/main",
		},
		{
			name:    "Generic Git host SSH",
			repoURL: "git@gitea.example.com:user/repo.git",
			branch:  "develop",
			want:    "https://gitea.example.com/user/repo/tree/develop",
		},
		// Real-world examples
		{
			name:    "Real GitHub repo",
			repoURL: "https://github.com/sergeknystautas/schmux.git",
			branch:  "main",
			want:    "https://github.com/sergeknystautas/schmux/tree/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildGitBranchURL(tt.repoURL, tt.branch)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("BuildGitBranchURL() = %q, want empty string", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("BuildGitBranchURL() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestBranchHasUpstream(t *testing.T) {
	// Create a test git repository
	dir := gitTestWorkTree(t)

	ctx := context.Background()

	// main branch has no upstream by default in a fresh repo
	if BranchHasUpstream(ctx, dir) {
		t.Error("BranchHasUpstream() returned true for branch with no upstream")
	}

	// Create a bare repo to use as a remote
	bareDir := t.TempDir()
	runGit(t, dir, "init", "--bare", bareDir)

	// Add the bare repo as a remote and push main branch to it
	runGit(t, dir, "remote", "add", "origin", bareDir)
	runGit(t, dir, "push", "-u", "origin", "main")

	// Now main should have an upstream
	if !BranchHasUpstream(ctx, dir) {
		t.Error("BranchHasUpstream() returned false for branch with upstream")
	}

	// Create a local branch without upstream
	runGit(t, dir, "checkout", "-b", "local-branch")
	if BranchHasUpstream(ctx, dir) {
		t.Error("BranchHasUpstream() returned true for local branch without upstream")
	}
}
