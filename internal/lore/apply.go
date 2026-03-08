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

// ApplyPublicMerge creates a branch, writes a single instruction file, commits, and cleans up.
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

// ApplyPublicMerge creates a branch, writes a single instruction file, commits, and cleans up.
// This is the v2 counterpart of ApplyProposal for the repo-public layer.
func ApplyPublicMerge(ctx context.Context, proposalID, bareDir, workBaseDir, filename, content, summary string) (*ApplyResult, error) {
	branch := fmt.Sprintf("schmux/lore-%s", strings.TrimPrefix(proposalID, "prop-"))

	defaultBranch, err := getDefaultBranch(ctx, bareDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	if err := runGit(ctx, bareDir, "branch", branch, defaultBranch); err != nil {
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	worktreePath := filepath.Join(workBaseDir, "lore-"+strings.TrimPrefix(proposalID, "prop-"))
	if err := runGit(ctx, bareDir, "worktree", "add", worktreePath, branch); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	defer func() {
		runGit(context.Background(), bareDir, "worktree", "remove", "--force", worktreePath)
	}()

	fullPath := filepath.Join(worktreePath, filepath.Clean(filename))
	if !strings.HasPrefix(fullPath, filepath.Clean(worktreePath)+string(os.PathSeparator)) {
		return nil, fmt.Errorf("path traversal in filename: %s", filename)
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for %s: %w", filename, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", filename, err)
	}

	if err := runGit(ctx, worktreePath, "add", filename); err != nil {
		return nil, fmt.Errorf("git add failed: %w", err)
	}

	msg := "chore: update instruction file with agent lore"
	if summary != "" {
		msg = fmt.Sprintf("chore: update instruction file with agent lore\n\n%s", summary)
	}
	if err := runGitWithAuthor(ctx, worktreePath, "schmux-lore", "schmux@localhost", "commit", "-m", msg); err != nil {
		return nil, fmt.Errorf("git commit failed: %w", err)
	}

	return &ApplyResult{Branch: branch}, nil
}

// ApplyToLayer writes merged content to a private instruction layer.
// For LayerRepoPublic, use ApplyPublicMerge (git branch+commit) instead.
func ApplyToLayer(store *InstructionStore, layer Layer, repo, content string) error {
	if layer == LayerRepoPublic {
		return fmt.Errorf("use ApplyPublicMerge for repo-public layer")
	}
	return store.Write(layer, repo, content)
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

// runGitWithAuthor runs a git command with author identity set via environment
// variables instead of git config, preventing config leakage to other worktrees.
func runGitWithAuthor(ctx context.Context, dir, name, email string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+name,
		"GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name,
		"GIT_COMMITTER_EMAIL="+email,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, string(output))
	}
	return nil
}
