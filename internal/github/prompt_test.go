package github

import (
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestBuildReviewPrompt(t *testing.T) {
	pr := contracts.PullRequest{
		Number:       42,
		Title:        "Add feature X",
		Body:         "This PR adds feature X to the system.",
		RepoName:     "schmux",
		Author:       "someone",
		SourceBranch: "feature-x",
		TargetBranch: "main",
		HTMLURL:      "https://github.com/user/schmux/pull/42",
		CreatedAt:    time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	prompt := BuildReviewPrompt(pr)

	checks := []string{
		"Pull Request #42: Add feature X",
		"Repository: schmux",
		"Author: @someone",
		"Branch: feature-x -> main",
		"URL: https://github.com/user/schmux/pull/42",
		"This PR adds feature X to the system.",
		"Please review this pull request.",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

func TestBuildReviewPrompt_EmptyBody(t *testing.T) {
	pr := contracts.PullRequest{
		Number:       1,
		Title:        "Fix bug",
		RepoName:     "repo",
		Author:       "dev",
		SourceBranch: "fix-bug",
		TargetBranch: "main",
		HTMLURL:      "https://github.com/user/repo/pull/1",
	}

	prompt := BuildReviewPrompt(pr)
	if strings.Contains(prompt, "\n\n\n") {
		t.Error("prompt has extra blank lines for empty body")
	}
}

func TestPRBranchName(t *testing.T) {
	tests := []struct {
		name string
		pr   contracts.PullRequest
		want string
	}{
		{
			name: "regular PR",
			pr:   contracts.PullRequest{Number: 42},
			want: "pr/42",
		},
		{
			name: "fork PR",
			pr:   contracts.PullRequest{Number: 42, IsFork: true, ForkOwner: "contributor"},
			want: "pr/contributor/42",
		},
		{
			name: "fork without owner",
			pr:   contracts.PullRequest{Number: 42, IsFork: true},
			want: "pr/42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PRBranchName(tt.pr); got != tt.want {
				t.Errorf("PRBranchName() = %q, want %q", got, tt.want)
			}
		})
	}
}
