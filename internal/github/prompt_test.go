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

	prompt := BuildReviewPrompt(pr, "/home/user/.schmux/workspaces/ws-123", "pr/42")

	if prompt == "" {
		t.Error("prompt is empty")
	}
	// Just check key pieces are included
	if !strings.Contains(prompt, "#42") {
		t.Error("prompt missing PR number")
	}
	if !strings.Contains(prompt, "/home/user/.schmux/workspaces/ws-123") {
		t.Error("prompt missing workspace path")
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
