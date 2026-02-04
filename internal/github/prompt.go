package github

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// BuildReviewPrompt builds a review prompt from PR metadata.
func BuildReviewPrompt(pr contracts.PullRequest) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Pull Request #%d: %s\n", pr.Number, pr.Title)
	fmt.Fprintf(&b, "Repository: %s\n", pr.RepoName)
	fmt.Fprintf(&b, "Author: @%s\n", pr.Author)
	fmt.Fprintf(&b, "Branch: %s -> %s\n", pr.SourceBranch, pr.TargetBranch)
	fmt.Fprintf(&b, "URL: %s\n", pr.HTMLURL)

	if pr.Body != "" {
		b.WriteString("\n")
		b.WriteString(pr.Body)
		b.WriteString("\n")
	}

	b.WriteString("\nPlease review this pull request. Consider code quality, correctness, test coverage, documentation, breaking changes, and performance implications.")

	return b.String()
}

// PRBranchName returns the local branch name for checking out a PR.
func PRBranchName(pr contracts.PullRequest) string {
	if pr.IsFork && pr.ForkOwner != "" {
		return fmt.Sprintf("pr/%s/%d", pr.ForkOwner, pr.Number)
	}
	return fmt.Sprintf("pr/%d", pr.Number)
}
