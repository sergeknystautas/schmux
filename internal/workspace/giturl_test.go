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

func TestRemoteBranchExists(t *testing.T) {
	// Create a test worktree repo
	repoDir := gitTestWorkTree(t)

	// Add the repo as its own remote (so we can fetch from it)
	runGit(t, repoDir, "remote", "add", "origin", repoDir)

	// Create a bare clone of it (simulating the bare clone for querying)
	bareDir := t.TempDir()
	runGit(t, "", "clone", "--bare", repoDir, bareDir)

	// Configure fetch refspec and fetch to populate refs/remotes/origin/*
	runGit(t, bareDir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	runGit(t, bareDir, "fetch", "origin")

	ctx := context.Background()

	// main branch should exist in bare clone
	if !RemoteBranchExists(ctx, bareDir, "main") {
		t.Error("RemoteBranchExists() returned false for main branch")
	}

	// Non-existent branch should return false
	if RemoteBranchExists(ctx, bareDir, "nonexistent-branch") {
		t.Error("RemoteBranchExists() returned true for non-existent branch")
	}
}
