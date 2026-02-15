package lore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyResult holds the output of applying a proposal.
type ApplyResult struct {
	Branch string // the branch that was created and committed to
}

// ApplyProposal creates a temp worktree, writes instruction files, commits, and cleans up.
// It does NOT push — the caller decides whether to push.
// bareDir is the path to the bare clone. workBaseDir is where temp worktrees are created.
func ApplyProposal(ctx context.Context, proposal *Proposal, bareDir, workBaseDir string) (*ApplyResult, error) {
	proposalID := proposal.ID
	branch := fmt.Sprintf("schmux/lore-%s", strings.TrimPrefix(proposalID, "prop-"))

	// Determine default branch to branch from
	defaultBranch, err := getDefaultBranch(ctx, bareDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	// Create branch from default branch
	if err := runGit(ctx, bareDir, "branch", branch, defaultBranch); err != nil {
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	// Create temp worktree
	worktreePath := filepath.Join(workBaseDir, "lore-"+strings.TrimPrefix(proposalID, "prop-"))
	if err := runGit(ctx, bareDir, "worktree", "add", worktreePath, branch); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Ensure cleanup
	defer func() {
		runGit(context.Background(), bareDir, "worktree", "remove", "--force", worktreePath)
	}()

	// Configure git user in worktree
	runGit(ctx, worktreePath, "config", "user.email", "schmux@localhost")
	runGit(ctx, worktreePath, "config", "user.name", "schmux-lore")

	// Write proposed files
	var filesToAdd []string
	for relPath, content := range proposal.ProposedFiles {
		fullPath := filepath.Join(worktreePath, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", relPath, err)
		}
		filesToAdd = append(filesToAdd, relPath)
	}

	// Stage and commit
	addArgs := append([]string{"add"}, filesToAdd...)
	if err := runGit(ctx, worktreePath, addArgs...); err != nil {
		return nil, fmt.Errorf("git add failed: %w", err)
	}

	n := len(proposal.ProposedFiles)
	msg := fmt.Sprintf("chore: update instruction files with agent lore (%d files)", n)
	if proposal.DiffSummary != "" {
		msg = fmt.Sprintf("chore: update instruction files with agent lore\n\n%s", proposal.DiffSummary)
	}
	if err := runGit(ctx, worktreePath, "commit", "-m", msg); err != nil {
		return nil, fmt.Errorf("git commit failed: %w", err)
	}

	return &ApplyResult{Branch: branch}, nil
}

// PushBranch pushes a branch to origin.
func PushBranch(ctx context.Context, bareDir, branch string) error {
	return runGit(ctx, bareDir, "push", "origin", branch)
}

// CreatePR creates a pull request using the gh CLI.
// It returns the PR URL on success. If gh is not installed, it returns an error
// that the caller can handle gracefully (e.g., log a warning instead of failing).
func CreatePR(ctx context.Context, bareDir, branch, title, body string) (string, error) {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("gh CLI not found: %w", err)
	}

	defaultBranch, err := getDefaultBranch(ctx, bareDir)
	if err != nil {
		return "", fmt.Errorf("failed to get default branch for PR base: %w", err)
	}

	cmd := exec.CommandContext(ctx, ghPath, "pr", "create",
		"--head", branch,
		"--base", defaultBranch,
		"--title", title,
		"--body", body,
	)
	cmd.Dir = bareDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %w: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func getDefaultBranch(ctx context.Context, bareDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "HEAD")
	cmd.Dir = bareDir
	output, err := cmd.Output()
	if err != nil {
		// Fall back to "main"
		return "main", nil
	}
	ref := strings.TrimSpace(string(output))
	// refs/heads/main -> main
	return strings.TrimPrefix(ref, "refs/heads/"), nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, string(output))
	}
	return nil
}
