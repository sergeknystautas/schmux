package lore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ApplyResult holds the output of applying a proposal.
type ApplyResult struct {
	Branch string // the branch that was created and committed to
}

// ApplyProposal creates a temp worktree, writes instruction files, commits, and cleans up.
// It does NOT push — the caller decides whether to push.
// bareDir is the path to the bare clone. workBaseDir is where temp worktrees are created.
func ApplyProposal(ctx context.Context, proposal *Proposal, bareDir, workBaseDir string) (*ApplyResult, error) {
	now := time.Now().UTC()
	branch := fmt.Sprintf("schmux/lore-%s", now.Format("20060102-150405"))

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
	worktreePath := filepath.Join(workBaseDir, "lore-"+now.Format("20060102-150405"))
	if err := runGit(ctx, bareDir, "worktree", "add", worktreePath, branch); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Ensure cleanup
	defer func() {
		runGit(ctx, bareDir, "worktree", "remove", "--force", worktreePath)
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
