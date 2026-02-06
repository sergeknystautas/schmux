package github

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// BuildReviewPrompt builds a review prompt from PR metadata and workspace info.
func BuildReviewPrompt(pr contracts.PullRequest, workspacePath, workspaceBranch string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Pull Request #%d: %s\n", pr.Number, pr.Title)
	fmt.Fprintf(&b, "Repository: %s\n", pr.RepoName)
	fmt.Fprintf(&b, "Author: @%s\n", pr.Author)
	fmt.Fprintf(&b, "Branch: %s -> %s\n", pr.SourceBranch, pr.TargetBranch)
	fmt.Fprintf(&b, "URL: %s\n", pr.HTMLURL)

	// Add workspace context so the agent knows where the code is
	b.WriteString("\n")
	fmt.Fprintf(&b, "PR code checkout location:\n")
	fmt.Fprintf(&b, "  Working directory: %s\n", workspacePath)
	fmt.Fprintf(&b, "  Current branch: %s (PR #%d already checked out)\n", workspaceBranch, pr.Number)
	b.WriteString("  The PR code is already in your working directory. Read the files directly.\n")

	if pr.Body != "" {
		b.WriteString("\n")
		b.WriteString(pr.Body)
		b.WriteString("\n")
	}

	b.WriteString("\nPlease review this pull request. ")
	b.WriteString("Focus on: (1) What problem is this solving? What are the goals? ")
	b.WriteString("(2) How does it accomplish those goals architecturally—what changed and why? ")
	b.WriteString("(3) Walk through an example of how something will work (or work differently) after this change. ")
	b.WriteString("Do NOT focus on code coverage or minor style nitpicks—understand the substantive change first.")

	return b.String()
}

// PRBranchName returns the local branch name for checking out a PR.
func PRBranchName(pr contracts.PullRequest) string {
	if pr.IsFork && pr.ForkOwner != "" {
		return fmt.Sprintf("pr/%s/%d", pr.ForkOwner, pr.Number)
	}
	return fmt.Sprintf("pr/%d", pr.Number)
}
