package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LinearSyncFromMain performs an iterative rebase from origin/main into the current branch.
// This brings commits FROM main INTO the current branch one at a time, preserving local changes.
// Supports diverged branches - will replay local commits on top of main's commits.
func (m *Manager) LinearSyncFromMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	workspacePath := w.Path

	// 1. git fetch origin
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = workspacePath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git fetch origin failed: %w: %s", err, string(output))
	}

	// 2. Check if origin/main is already an ancestor of HEAD (nothing to pull)
	ancestorCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", "origin/main", "HEAD")
	ancestorCmd.Dir = workspacePath
	if err := ancestorCmd.Run(); err == nil {
		// origin/main IS an ancestor of HEAD - nothing new to pull from main
		return &LinearSyncResult{
			Success: true,
			Message: "Already caught up to main",
		}, nil
	}
	// Otherwise proceed - there are commits to pull (whether we're behind or diverged)

	// 3. git log --oneline --reverse HEAD..origin/main - Get list of commit hashes
	logCmd := exec.CommandContext(ctx, "git", "log", "--oneline", "--reverse", "HEAD..origin/main")
	logCmd.Dir = workspacePath
	output, err := logCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w: %s", err, string(output))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	// 4. If zero lines (no commits): return "Caught up to main"
	if len(lines) == 1 && lines[0] == "" {
		return &LinearSyncResult{
			Success: true,
			Message: "Caught up to main",
		}, nil
	}

	// Extract commit hashes (first word of each line)
	commitHashes := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			commitHashes = append(commitHashes, parts[0])
		}
	}

	if len(commitHashes) == 0 {
		return &LinearSyncResult{
			Success: true,
			Message: "Caught up to main",
		}, nil
	}

	// 5. git add -A + git commit -m "WIP: <UUID>" to save local changes (including untracked files)
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = workspacePath
	if output, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add -A failed: %w: %s", err, string(output))
	}

	wipUUID := fmt.Sprintf("WIP: %d", time.Now().UnixNano())
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", wipUUID)
	commitCmd.Dir = workspacePath
	commitOutput, err := commitCmd.CombinedOutput()
	didCommit := true
	if err != nil {
		if strings.Contains(string(commitOutput), "nothing to commit") {
			didCommit = false
			fmt.Printf("[workspace] no local changes to commit\n")
		} else {
			return nil, fmt.Errorf("git commit failed: %w: %s", err, string(commitOutput))
		}
	} else {
		fmt.Printf("[workspace] committed local changes as: %s\n", wipUUID)
	}

	// 6. For each commit hash: git rebase <hash>
	successCount := 0
	for i, hash := range commitHashes {
		rebaseCmd := exec.CommandContext(ctx, "git", "rebase", hash)
		rebaseCmd.Dir = workspacePath
		if err := rebaseCmd.Run(); err != nil {
			// Conflict occurred
			// git rebase --abort
			abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
			abortCmd.Dir = workspacePath
			_ = abortCmd.Run()

			// git reset --mixed HEAD~1 - undo the WIP commit
			if didCommit {
				resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
				resetCmd.Dir = workspacePath
				_ = resetCmd.Run()
				fmt.Printf("[workspace] reset WIP commit after conflict\n")
			}

			return &LinearSyncResult{
				Success: false,
				Message: fmt.Sprintf("FF'd %d commits before conflict", successCount),
			}, nil
		}
		successCount++
		fmt.Printf("[workspace] rebased %d/%d: %s\n", i+1, len(commitHashes), hash)
	}

	// 7. All succeeded: git reset --mixed HEAD~1 - undo the WIP commit
	if didCommit {
		resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
		resetCmd.Dir = workspacePath
		if output, err := resetCmd.CombinedOutput(); err != nil {
			fmt.Printf("[workspace] warning: git reset --mixed failed: %s\n", string(output))
		} else {
			fmt.Printf("[workspace] restored local changes after successful rebase\n")
		}
	}

	return &LinearSyncResult{
		Success: true,
		Message: fmt.Sprintf("FF'd %d commits successfully. Caught up to main", successCount),
	}, nil
}

// LinearSyncToMain performs a fast-forward push to origin/main.
// The current branch's commits are pushed directly to main without a merge commit.
func (m *Manager) LinearSyncToMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	workspacePath := w.Path

	// 1. git fetch origin
	fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s fetching origin\n", workspaceID)
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = workspacePath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git fetch origin failed: %w: %s", err, string(output))
	}

	// 2. Check if origin/main is an ancestor of HEAD (we're ahead, FF possible)
	// git merge-base --is-ancestor origin/main HEAD
	ancestorCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", "origin/main", "HEAD")
	ancestorCmd.Dir = workspacePath
	if err := ancestorCmd.Run(); err != nil {
		// origin/main is NOT an ancestor of HEAD
		// Check if HEAD is an ancestor of origin/main (we're behind)
		reverseCheckCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", "HEAD", "origin/main")
		reverseCheckCmd.Dir = workspacePath
		if err := reverseCheckCmd.Run(); err != nil {
			// Branches have diverged, FF not possible
			return &LinearSyncResult{
				Success: false,
				Message: "Branch is not an ancestor of main",
			}, nil
		}
		// HEAD IS an ancestor of origin/main - we're behind
		return &LinearSyncResult{
			Success: false,
			Message: "Branch is behind main, pull first",
		}, nil
	}

	// 3. Re-check all conditions on server with fresh git status
	dirty, ahead, behind, linesAdded, linesRemoved, filesChanged := m.gitStatus(ctx, workspacePath)
	if dirty {
		return &LinearSyncResult{
			Success: false,
			Message: "Workspace has uncommitted changes",
		}, nil
	}
	if linesAdded != 0 || linesRemoved != 0 {
		return &LinearSyncResult{
			Success: false,
			Message: "Workspace has uncommitted line changes",
		}, nil
	}
	if filesChanged != 0 {
		return &LinearSyncResult{
			Success: false,
			Message: "Workspace has changed files",
		}, nil
	}
	if behind != 0 {
		return &LinearSyncResult{
			Success: false,
			Message: "Branch is behind main",
		}, nil
	}
	if ahead < 1 {
		return &LinearSyncResult{
			Success: false,
			Message: "No commits to push",
		}, nil
	}

	// 4. Get current branch name
	currentBranch, err := m.gitCurrentBranch(ctx, workspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch: %w", err)
	}
	fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s current_branch=%s\n", workspaceID, currentBranch)

	// 5. Push to main
	if currentBranch == "main" {
		// On main branch: simple push
		fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s pushing from main\n", workspaceID)
		pushCmd := exec.CommandContext(ctx, "git", "push")
		pushCmd.Dir = workspacePath
		if output, err := pushCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git push failed: %w: %s", err, string(output))
		}
	} else {
		// On feature branch: set upstream to main, push to main, then sync local
		fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s setting upstream to main\n", workspaceID)
		upstreamCmd := exec.CommandContext(ctx, "git", "branch", "--set-upstream-to=origin/main")
		upstreamCmd.Dir = workspacePath
		if output, err := upstreamCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git branch --set-upstream-to=origin/main failed: %w: %s", err, string(output))
		}

		fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s pushing to main\n", workspaceID)
		pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "HEAD:main")
		pushCmd.Dir = workspacePath
		if output, err := pushCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git push origin HEAD:main failed: %w: %s", err, string(output))
		}

		// Sync local branch to match new main
		fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s syncing local branch\n", workspaceID)
		mergeCmd := exec.CommandContext(ctx, "git", "merge", "--ff-only", "origin/main")
		mergeCmd.Dir = workspacePath
		if output, err := mergeCmd.CombinedOutput(); err != nil {
			// This shouldn't fail since we just pushed, but log warning
			fmt.Printf("[workspace] linear-sync-to-main: warning: git merge --ff-only failed: %s\n", string(output))
		}
	}

	fmt.Printf("[workspace] linear-sync-to-main: workspace_id=%s success\n", workspaceID)
	return &LinearSyncResult{
		Success: true,
		Message: fmt.Sprintf("Pushed %d commit(s) to main", ahead),
	}, nil
}

// LinearSyncResolveConflict rebases exactly one commit from origin/main, handling conflicts.
// If there's a conflict, it captures the conflicted files, resolves structurally (git add + continue),
// and returns the list of files that had conflicts for semantic resolution by an LLM.
func (m *Manager) LinearSyncResolveConflict(ctx context.Context, workspaceID string) (*LinearSyncResolveConflictResult, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	workspacePath := w.Path

	// 1. Get the oldest commit hash from HEAD..origin/main
	logCmd := exec.CommandContext(ctx, "git", "log", "--oneline", "--reverse", "HEAD..origin/main")
	logCmd.Dir = workspacePath
	output, err := logCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w: %s", err, string(output))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return &LinearSyncResolveConflictResult{
			Success: true,
			Message: "Caught up to main",
		}, nil
	}

	// Get the first (oldest) commit hash
	parts := strings.Fields(lines[0])
	if len(parts) == 0 {
		return &LinearSyncResolveConflictResult{
			Success: true,
			Message: "Caught up to main",
		}, nil
	}
	hash := parts[0]
	fmt.Printf("[workspace] linear-sync-resolve-conflict: workspace_id=%s rebasing hash=%s\n", workspaceID, hash)

	// 2. Create WIP commit to preserve local changes (including untracked files)
	addWipCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addWipCmd.Dir = workspacePath
	if output, err := addWipCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add -A failed: %w: %s", err, string(output))
	}

	wipUUID := fmt.Sprintf("WIP: %d", time.Now().UnixNano())
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", wipUUID)
	commitCmd.Dir = workspacePath
	commitOutput, err := commitCmd.CombinedOutput()
	didCommit := true
	if err != nil {
		if strings.Contains(string(commitOutput), "nothing to commit") {
			didCommit = false
			fmt.Printf("[workspace] linear-sync-resolve-conflict: no local changes to commit\n")
		} else {
			return nil, fmt.Errorf("git commit failed: %w: %s", err, string(commitOutput))
		}
	} else {
		fmt.Printf("[workspace] linear-sync-resolve-conflict: committed local changes as: %s\n", wipUUID)
	}

	// 3. git rebase <hash>
	rebaseCmd := exec.CommandContext(ctx, "git", "rebase", hash)
	rebaseCmd.Dir = workspacePath
	rebaseErr := rebaseCmd.Run()

	var conflictedFiles []string
	hadConflict := false

	if rebaseErr != nil {
		hadConflict = true
		fmt.Printf("[workspace] linear-sync-resolve-conflict: conflict detected during rebase\n")

		// 4. Note the conflicts
		diffCmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "--diff-filter=U")
		diffCmd.Dir = workspacePath
		diffOutput, err := diffCmd.Output()
		if err != nil {
			// Abort and restore
			abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
			abortCmd.Dir = workspacePath
			_ = abortCmd.Run()
			if didCommit {
				resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
				resetCmd.Dir = workspacePath
				_ = resetCmd.Run()
			}
			return nil, fmt.Errorf("git diff --name-only failed: %w", err)
		}

		conflictedFiles = strings.Split(strings.TrimSpace(string(diffOutput)), "\n")
		// Filter out empty strings
		filtered := make([]string, 0, len(conflictedFiles))
		for _, f := range conflictedFiles {
			if f != "" {
				filtered = append(filtered, f)
			}
		}
		conflictedFiles = filtered
		fmt.Printf("[workspace] linear-sync-resolve-conflict: conflicted files: %v\n", conflictedFiles)

		// 5. git add the files
		addCmd := exec.CommandContext(ctx, "git", "add", "-A")
		addCmd.Dir = workspacePath
		if addOutput, err := addCmd.CombinedOutput(); err != nil {
			abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
			abortCmd.Dir = workspacePath
			_ = abortCmd.Run()
			if didCommit {
				resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
				resetCmd.Dir = workspacePath
				_ = resetCmd.Run()
			}
			return nil, fmt.Errorf("git add failed: %w: %s", err, string(addOutput))
		}

		// 6. git rebase --continue
		continueCmd := exec.CommandContext(ctx, "git", "rebase", "--continue")
		continueCmd.Dir = workspacePath
		continueCmd.Env = append(os.Environ(), "GIT_EDITOR=true") // Skip commit message editor
		if continueOutput, err := continueCmd.CombinedOutput(); err != nil {
			fmt.Printf("[workspace] linear-sync-resolve-conflict: rebase --continue failed: %s\n", string(continueOutput))
			abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
			abortCmd.Dir = workspacePath
			_ = abortCmd.Run()
			if didCommit {
				resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
				resetCmd.Dir = workspacePath
				_ = resetCmd.Run()
			}
			return nil, fmt.Errorf("git rebase --continue failed: %w: %s", err, string(continueOutput))
		}
	}

	// 7. git reset --mixed HEAD~1
	if didCommit {
		resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
		resetCmd.Dir = workspacePath
		if output, err := resetCmd.CombinedOutput(); err != nil {
			fmt.Printf("[workspace] linear-sync-resolve-conflict: warning: git reset --mixed failed: %s\n", string(output))
		} else {
			fmt.Printf("[workspace] linear-sync-resolve-conflict: restored local changes after rebase\n")
		}
	}

	if hadConflict {
		return &LinearSyncResolveConflictResult{
			Success:         true,
			Message:         fmt.Sprintf("Rebased %s with conflicts", hash),
			Hash:            hash,
			ConflictedFiles: conflictedFiles,
			HadConflict:     true,
		}, nil
	}

	return &LinearSyncResolveConflictResult{
		Success:     true,
		Message:     fmt.Sprintf("Rebased %s cleanly", hash),
		Hash:        hash,
		HadConflict: false,
	}, nil
}
