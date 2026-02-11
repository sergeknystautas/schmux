package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/conflictresolve"
)

// LinearSyncFromDefault performs an iterative rebase from the default branch into the current branch.
// This brings commits FROM the default branch INTO the current branch one at a time, preserving local changes.
// Supports diverged branches - will replay local commits on top of default branch's commits.
func (m *Manager) LinearSyncFromDefault(ctx context.Context, workspaceID string) (*LinearSyncResult, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Get the default branch
	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	workspacePath := w.Path
	defaultRef := "origin/" + defaultBranch

	// 1. git fetch origin
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = workspacePath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git fetch origin failed: %w: %s", err, string(output))
	}

	// 2. Check if default branch is already an ancestor of HEAD (nothing to pull)
	ancestorCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", defaultRef, "HEAD")
	ancestorCmd.Dir = workspacePath
	if err := ancestorCmd.Run(); err == nil {
		// default branch IS an ancestor of HEAD - nothing new to pull
		return &LinearSyncResult{
			Success:      true,
			SuccessCount: 0,
			Branch:       defaultBranch,
		}, nil
	}
	// Otherwise proceed - there are commits to pull (whether we're behind or diverged)

	// 3. git log --oneline --reverse HEAD..<default branch> - Get list of commit hashes
	logCmd := exec.CommandContext(ctx, "git", "log", "--oneline", "--reverse", "HEAD.."+defaultRef)
	logCmd.Dir = workspacePath
	output, err := logCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w: %s", err, string(output))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	// 4. If zero lines (no commits): return "Caught up to <default branch>"
	if len(lines) == 1 && lines[0] == "" {
		return &LinearSyncResult{
			Success:      true,
			SuccessCount: 0,
			Branch:       defaultBranch,
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
			Success:      true,
			SuccessCount: 0,
			Branch:       defaultBranch,
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
				Success:         false,
				SuccessCount:    successCount,
				ConflictingHash: hash,
				Branch:          defaultBranch,
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
		Success:      true,
		SuccessCount: successCount,
		Branch:       defaultBranch,
	}, nil
}

// LinearSyncToDefault performs a fast-forward push to the default branch.
// The current branch's commits are pushed directly to the default branch without a merge commit.
func (m *Manager) LinearSyncToDefault(ctx context.Context, workspaceID string) (*LinearSyncResult, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Get the default branch
	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	workspacePath := w.Path
	defaultRef := "origin/" + defaultBranch

	// 1. git fetch origin
	fmt.Printf("[workspace] linear-sync-to-default: workspace_id=%s fetching origin\n", workspaceID)
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = workspacePath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git fetch origin failed: %w: %s", err, string(output))
	}

	// 2. Check if default branch is an ancestor of HEAD (we're ahead, FF possible)
	ancestorCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", defaultRef, "HEAD")
	ancestorCmd.Dir = workspacePath
	if err := ancestorCmd.Run(); err != nil {
		// default branch is NOT an ancestor of HEAD
		// Check if HEAD is an ancestor of default branch (we're behind)
		reverseCheckCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", "HEAD", defaultRef)
		reverseCheckCmd.Dir = workspacePath
		if err := reverseCheckCmd.Run(); err != nil {
			// Branches have diverged, FF not possible
			return &LinearSyncResult{
				Success: false,
				Branch:  defaultBranch,
			}, nil
		}
		// HEAD IS an ancestor of default branch - we're behind
		return &LinearSyncResult{
			Success: false,
			Branch:  defaultBranch,
		}, nil
	}

	// 3. Re-check all conditions on server with fresh git status
	dirty, ahead, behind, linesAdded, linesRemoved, filesChanged, _ := m.gitStatus(ctx, workspacePath, w.Repo)
	if dirty {
		return &LinearSyncResult{
			Success: false,
			Branch:  defaultBranch,
		}, nil
	}
	if linesAdded != 0 || linesRemoved != 0 {
		return &LinearSyncResult{
			Success: false,
			Branch:  defaultBranch,
		}, nil
	}
	if filesChanged != 0 {
		return &LinearSyncResult{
			Success: false,
			Branch:  defaultBranch,
		}, nil
	}
	if behind != 0 {
		return &LinearSyncResult{
			Success: false,
			Branch:  defaultBranch,
		}, nil
	}
	if ahead < 1 {
		return &LinearSyncResult{
			Success: false,
			Branch:  defaultBranch,
		}, nil
	}

	// 4. Get current branch name
	currentBranch, err := m.gitCurrentBranch(ctx, workspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch: %w", err)
	}
	fmt.Printf("[workspace] linear-sync-to-default: workspace_id=%s current_branch=%s\n", workspaceID, currentBranch)

	// 5. Push to default branch
	if currentBranch == defaultBranch {
		// On default branch: simple push
		fmt.Printf("[workspace] linear-sync-to-default: workspace_id=%s pushing from %s\n", workspaceID, defaultBranch)
		pushCmd := exec.CommandContext(ctx, "git", "push")
		pushCmd.Dir = workspacePath
		if output, err := pushCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git push failed: %w: %s", err, string(output))
		}
	} else {
		// On feature branch: set upstream to default branch, push to default branch, then sync local
		fmt.Printf("[workspace] linear-sync-to-default: workspace_id=%s setting upstream to %s\n", workspaceID, defaultBranch)
		upstreamCmd := exec.CommandContext(ctx, "git", "branch", "--set-upstream-to="+defaultRef)
		upstreamCmd.Dir = workspacePath
		if output, err := upstreamCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git branch --set-upstream-to=%s failed: %w: %s", defaultRef, err, string(output))
		}

		fmt.Printf("[workspace] linear-sync-to-default: workspace_id=%s pushing to %s\n", workspaceID, defaultBranch)
		pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "HEAD:"+defaultBranch)
		pushCmd.Dir = workspacePath
		if output, err := pushCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git push origin HEAD:%s failed: %w: %s", defaultBranch, err, string(output))
		}

		// Sync local branch to match new default branch
		fmt.Printf("[workspace] linear-sync-to-default: workspace_id=%s syncing local branch\n", workspaceID)
		mergeCmd := exec.CommandContext(ctx, "git", "merge", "--ff-only", defaultRef)
		mergeCmd.Dir = workspacePath
		if output, err := mergeCmd.CombinedOutput(); err != nil {
			// This shouldn't fail since we just pushed, but log warning
			fmt.Printf("[workspace] linear-sync-to-default: warning: git merge --ff-only failed: %s\n", string(output))
		}
	}

	fmt.Printf("[workspace] linear-sync-to-default: workspace_id=%s success\n", workspaceID)
	return &LinearSyncResult{
		Success:      true,
		SuccessCount: ahead,
		Branch:       defaultBranch,
	}, nil
}

// LinearSyncFromMain performs an iterative rebase from origin/main into the current branch.
// Deprecated: Use LinearSyncFromDefault instead.
func (m *Manager) LinearSyncFromMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error) {
	return m.LinearSyncFromDefault(ctx, workspaceID)
}

// LinearSyncToMain performs a fast-forward push to origin/main.
// Deprecated: Use LinearSyncToDefault instead.
func (m *Manager) LinearSyncToMain(ctx context.Context, workspaceID string) (*LinearSyncResult, error) {
	return m.LinearSyncToDefault(ctx, workspaceID)
}

// LinearSyncResolveConflict rebases exactly one commit from the default branch, handling conflicts.
// When a conflict occurs during replay of local commits, it pauses the rebase, runs a non-interactive
// one-shot LLM call to resolve the conflicted files, then continues. Repeats for each conflicting commit.
// The onStep callback (if non-nil) is called at each progress step for real-time reporting.
func (m *Manager) LinearSyncResolveConflict(ctx context.Context, workspaceID string, onStep ResolveConflictStepFunc) (*LinearSyncResolveConflictResult, error) {
	emit := func(step ResolveConflictStep) {
		if onStep != nil {
			onStep(step)
		}
	}

	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Acquire repo lock for the duration of this operation
	lock := m.repoLock(w.Repo)
	lock.Lock()
	defer lock.Unlock()

	// Get the default branch
	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	workspacePath := w.Path
	defaultRef := "origin/" + defaultBranch

	// 1. Get the oldest commit hash from HEAD..<default branch>
	logCmd := exec.CommandContext(ctx, "git", "log", "--oneline", "--reverse", "HEAD.."+defaultRef)
	logCmd.Dir = workspacePath
	output, err := logCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w: %s", err, string(output))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		msg := fmt.Sprintf("Caught up to %s", defaultBranch)
		emit(ResolveConflictStep{Action: "check_behind", Status: "done", Message: msg})
		return &LinearSyncResolveConflictResult{
			Success:     true,
			Message:     msg,
			Resolutions: []ConflictResolution{},
		}, nil
	}

	// Get the first (oldest) commit hash
	parts := strings.Fields(lines[0])
	if len(parts) == 0 {
		msg := fmt.Sprintf("Caught up to %s", defaultBranch)
		emit(ResolveConflictStep{Action: "check_behind", Status: "done", Message: msg})
		return &LinearSyncResolveConflictResult{
			Success:     true,
			Message:     msg,
			Resolutions: []ConflictResolution{},
		}, nil
	}
	hash := parts[0]
	emit(ResolveConflictStep{
		Action:  "check_behind",
		Status:  "done",
		Message: fmt.Sprintf("%d commits behind origin/%s, rebasing %s", len(lines), defaultBranch, hash),
		Hash:    hash,
	})
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
		} else {
			return nil, fmt.Errorf("git commit failed: %w: %s", err, string(commitOutput))
		}
	}

	created := didCommit
	wipMsg := "No local changes, skipped WIP commit"
	if didCommit {
		wipMsg = "Created WIP commit to preserve local changes"
	}
	emit(ResolveConflictStep{Action: "wip_commit", Status: "done", Message: wipMsg, Created: &created})
	fmt.Printf("[workspace] linear-sync-resolve-conflict: %s\n", wipMsg)

	// Helper to abort rebase and unwind WIP commit
	abortAndUnwind := func(reason string) {
		emit(ResolveConflictStep{Action: "abort", Status: "in_progress", Message: fmt.Sprintf("Aborting: %s", reason)})
		abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
		abortCmd.Dir = workspacePath
		_ = abortCmd.Run()
		if didCommit {
			resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
			resetCmd.Dir = workspacePath
			_ = resetCmd.Run()
		}
		emit(ResolveConflictStep{Action: "abort", Status: "failed", Message: fmt.Sprintf("Aborted: %s", reason)})
	}

	// Helper to unwind WIP commit (after successful rebase)
	unwindWIP := func() {
		if didCommit {
			emit(ResolveConflictStep{Action: "wip_unwind", Status: "in_progress", Message: "Unwinding WIP commit"})
			resetCmd := exec.CommandContext(ctx, "git", "reset", "--mixed", "HEAD~1")
			resetCmd.Dir = workspacePath
			if output, err := resetCmd.CombinedOutput(); err != nil {
				fmt.Printf("[workspace] linear-sync-resolve-conflict: warning: git reset --mixed failed: %s\n", string(output))
				emit(ResolveConflictStep{Action: "wip_unwind", Status: "failed", Message: fmt.Sprintf("Warning: git reset --mixed failed: %s", string(output))})
			} else {
				emit(ResolveConflictStep{Action: "wip_unwind", Status: "done", Message: "Restored local changes"})
			}
		}
	}

	// 3. git rebase <hash>
	emit(ResolveConflictStep{Action: "rebase_start", Status: "in_progress", Message: fmt.Sprintf("git rebase %s", hash)})
	rebaseCmd := exec.CommandContext(ctx, "git", "rebase", hash)
	rebaseCmd.Dir = workspacePath
	rebaseOutput, rebaseErr := rebaseCmd.CombinedOutput()

	var resolutions []ConflictResolution

	if rebaseErr == nil {
		// Clean rebase - no conflicts
		emit(ResolveConflictStep{Action: "rebase_start", Status: "done", Message: fmt.Sprintf("Rebased %s cleanly", hash)})
		unwindWIP()
		return &LinearSyncResolveConflictResult{
			Success:     true,
			Message:     fmt.Sprintf("Rebased %s cleanly", hash),
			Hash:        hash,
			Resolutions: []ConflictResolution{},
		}, nil
	}

	if !rebaseInProgress(workspacePath) {
		msg := strings.TrimSpace(string(rebaseOutput))
		if msg == "" {
			msg = rebaseErr.Error()
		}
		fullMsg := fmt.Sprintf("git rebase %s failed: %s", hash, msg)
		emit(ResolveConflictStep{Action: "rebase_start", Status: "failed", Message: fullMsg})
		abortAndUnwind(fullMsg)
		return &LinearSyncResolveConflictResult{
			Success:     false,
			Message:     fullMsg,
			Hash:        hash,
			Resolutions: resolutions,
		}, nil
	}

	emit(ResolveConflictStep{Action: "rebase_start", Status: "done", Message: fmt.Sprintf("git rebase %s — conflict detected", hash)})

	// 4. Conflict loop
	for {
		// Get unmerged files
		unmergedFiles := m.getUnmergedFiles(ctx, workspacePath)
		if len(unmergedFiles) == 0 {
			// No unmerged files but rebase is in progress — git may have auto-resolved
			// content conflicts and just needs a continue.
			if rebaseInProgress(workspacePath) {
				emit(ResolveConflictStep{Action: "rebase_continue", Status: "in_progress", Message: "No unmerged files, attempting git rebase --continue"})
				autoContinueCmd := exec.CommandContext(ctx, "git", "rebase", "--continue")
				autoContinueCmd.Dir = workspacePath
				autoContinueCmd.Env = append(os.Environ(), "GIT_EDITOR=true")
				autoContinueOutput, autoContinueErr := autoContinueCmd.CombinedOutput()
				if autoContinueErr == nil {
					if !rebaseInProgress(workspacePath) {
						emit(ResolveConflictStep{Action: "rebase_continue", Status: "done", Message: "Rebase complete (auto-resolved)"})
						break
					}
					emit(ResolveConflictStep{Action: "rebase_continue", Status: "done", Message: "Continuing to next commit"})
					continue
				}
				// Continue failed — check if there are now unmerged files (new conflict)
				if rebaseInProgress(workspacePath) {
					nextUnmerged := m.getUnmergedFiles(ctx, workspacePath)
					if len(nextUnmerged) > 0 {
						emit(ResolveConflictStep{Action: "rebase_continue", Status: "done", Message: "Next commit has conflicts"})
						continue
					}
				}
				msg := fmt.Sprintf("git rebase --continue failed: %s", string(autoContinueOutput))
				emit(ResolveConflictStep{Action: "rebase_continue", Status: "failed", Message: msg})
				abortAndUnwind(msg)
				return &LinearSyncResolveConflictResult{
					Success:     false,
					Message:     msg,
					Hash:        hash,
					Resolutions: resolutions,
				}, nil
			}
			msg := fmt.Sprintf("Rebase failed (no unmerged files, no rebase in progress) on %s", hash)
			fmt.Printf("[workspace] linear-sync-resolve-conflict: %s\n", msg)
			abortAndUnwind(msg)
			return &LinearSyncResolveConflictResult{
				Success:     false,
				Message:     msg,
				Hash:        hash,
				Resolutions: resolutions,
			}, nil
		}

		// Get local commit info (the commit being replayed)
		localCommitHash := m.getRebaseHead(ctx, workspacePath)
		localCommitMessage := m.getRebaseMessage(ctx, workspacePath)

		emit(ResolveConflictStep{
			Action:             "conflict_detected",
			Status:             "done",
			Message:            fmt.Sprintf("Conflict on %s — %d file(s)", localCommitHash[:minLen(len(localCommitHash), 7)], len(unmergedFiles)),
			LocalCommit:        localCommitHash,
			LocalCommitMessage: localCommitMessage,
			Files:              unmergedFiles,
		})
		fmt.Printf("[workspace] linear-sync-resolve-conflict: conflict on files: %v\n", unmergedFiles)

		// Build prompt and call LLM
		emit(ResolveConflictStep{
			Action:      "llm_call",
			Status:      "in_progress",
			Message:     fmt.Sprintf("Calling LLM to resolve %d file(s)...", len(unmergedFiles)),
			LocalCommit: localCommitHash,
			Files:       unmergedFiles,
		})

		prompt := conflictresolve.BuildPrompt(workspacePath, hash, localCommitHash, localCommitMessage, unmergedFiles)
		oneshotResult, rawResponse, err := conflictresolve.Execute(ctx, m.config, prompt, workspacePath)

		// Record the resolution attempt
		fileNames := make([]string, 0, len(unmergedFiles))
		fileNames = append(fileNames, unmergedFiles...)

		resolution := ConflictResolution{
			LocalCommit:        localCommitHash,
			LocalCommitMessage: localCommitMessage,
			Files:              fileNames,
		}

		if err != nil {
			fmt.Printf("[workspace] linear-sync-resolve-conflict: oneshot error on %s: %v\n", localCommitHash, err)
			resolution.Summary = fmt.Sprintf("LLM error: %v", err)
			resolutions = append(resolutions, resolution)
			msg := fmt.Sprintf("Could not resolve conflict on local commit %s: %v", localCommitHash, err)
			stepSummary := truncateString(rawResponse, 2000)
			emit(ResolveConflictStep{Action: "llm_call", Status: "failed", Message: msg, LocalCommit: localCommitHash, Files: unmergedFiles, Summary: stepSummary})
			abortAndUnwind(msg)
			return &LinearSyncResolveConflictResult{
				Success:     false,
				Message:     msg,
				Hash:        hash,
				Resolutions: resolutions,
			}, nil
		}

		resolution.AllResolved = oneshotResult.AllResolved
		resolution.Confidence = oneshotResult.Confidence
		resolution.Summary = oneshotResult.Summary
		resolutions = append(resolutions, resolution)
		fmt.Printf("[workspace] linear-sync-resolve-conflict: oneshot result on %s: all_resolved=%t confidence=%s summary=%q\n", localCommitHash, oneshotResult.AllResolved, oneshotResult.Confidence, oneshotResult.Summary)

		// Check decision logic: must be all_resolved=true AND confidence=high
		if !oneshotResult.AllResolved || oneshotResult.Confidence != "high" {
			reason := "not all resolved"
			if oneshotResult.AllResolved {
				reason = fmt.Sprintf("%s confidence", oneshotResult.Confidence)
			}
			msg := fmt.Sprintf("Could not resolve conflict on local commit %s: %s", localCommitHash, reason)
			emit(ResolveConflictStep{
				Action:      "llm_call",
				Status:      "failed",
				Message:     msg,
				LocalCommit: localCommitHash,
				Files:       unmergedFiles,
				Confidence:  oneshotResult.Confidence,
				Summary:     oneshotResult.Summary,
			})
			abortAndUnwind(msg)
			return &LinearSyncResolveConflictResult{
				Success:     false,
				Message:     msg,
				Hash:        hash,
				Resolutions: resolutions,
			}, nil
		}

		emit(ResolveConflictStep{
			Action:      "llm_call",
			Status:      "done",
			Message:     oneshotResult.Summary,
			LocalCommit: localCommitHash,
			Files:       unmergedFiles,
			Confidence:  oneshotResult.Confidence,
			Summary:     oneshotResult.Summary,
		})

		// Validate LLM response: every unmerged file must be present; extra files are ignored
		unmergedSet := make(map[string]struct{}, len(unmergedFiles))
		for _, f := range unmergedFiles {
			unmergedSet[f] = struct{}{}
		}
		for filePath := range oneshotResult.Files {
			if _, ok := unmergedSet[filePath]; !ok {
				fmt.Printf("[workspace] linear-sync-resolve-conflict: ignoring extra file %q from LLM response (not in conflicted set)\n", filePath)
			}
		}

		// Validate on-disk state matches the reported actions
		for _, f := range unmergedFiles {
			action, ok := oneshotResult.Files[f]
			if !ok {
				msg := fmt.Sprintf("LLM omitted conflicted file %q from response", f)
				abortAndUnwind(msg)
				return &LinearSyncResolveConflictResult{
					Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
				}, nil
			}

			cleaned := filepath.Clean(f)
			if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
				msg := fmt.Sprintf("Invalid file path from git: %q", f)
				abortAndUnwind(msg)
				return &LinearSyncResolveConflictResult{
					Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
				}, nil
			}
			fullPath := filepath.Join(workspacePath, cleaned)

			switch action.Action {
			case "deleted":
				if _, err := os.Stat(fullPath); err == nil {
					msg := fmt.Sprintf("LLM reported %q as deleted but file still exists on disk", f)
					abortAndUnwind(msg)
					return &LinearSyncResolveConflictResult{
						Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
					}, nil
				}
			case "modified":
				data, err := os.ReadFile(fullPath)
				if err != nil {
					msg := fmt.Sprintf("LLM reported %q as modified but cannot read file: %v", f, err)
					abortAndUnwind(msg)
					return &LinearSyncResolveConflictResult{
						Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
					}, nil
				}
				contents := string(data)
				if strings.Contains(contents, "<<<<<<<") || strings.Contains(contents, ">>>>>>>") {
					msg := fmt.Sprintf("File %q still contains conflict markers after LLM resolution", f)
					abortAndUnwind(msg)
					return &LinearSyncResolveConflictResult{
						Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
					}, nil
				}
			default:
				msg := fmt.Sprintf("LLM returned unknown action %q for file %q", action.Action, f)
				abortAndUnwind(msg)
				return &LinearSyncResolveConflictResult{
					Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
				}, nil
			}
		}

		// Stage resolved files based on the LLM action
		var addFiles []string
		var rmFiles []string
		for _, f := range unmergedFiles {
			action := oneshotResult.Files[f]
			switch action.Action {
			case "modified":
				addFiles = append(addFiles, f)
			case "deleted":
				rmFiles = append(rmFiles, f)
			}
		}
		if len(addFiles) > 0 {
			addArgs := append([]string{"add", "--"}, addFiles...)
			addCmd := exec.CommandContext(ctx, "git", addArgs...)
			addCmd.Dir = workspacePath
			if addOutput, err := addCmd.CombinedOutput(); err != nil {
				msg := fmt.Sprintf("git add failed after resolution: %v: %s", err, string(addOutput))
				abortAndUnwind(msg)
				return &LinearSyncResolveConflictResult{
					Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
				}, nil
			}
		}
		if len(rmFiles) > 0 {
			rmArgs := append([]string{"rm", "--ignore-unmatch", "--"}, rmFiles...)
			rmCmd := exec.CommandContext(ctx, "git", rmArgs...)
			rmCmd.Dir = workspacePath
			if rmOutput, err := rmCmd.CombinedOutput(); err != nil {
				msg := fmt.Sprintf("git rm failed after resolution: %v: %s", err, string(rmOutput))
				abortAndUnwind(msg)
				return &LinearSyncResolveConflictResult{
					Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
				}, nil
			}
		}

		// git rebase --continue
		emit(ResolveConflictStep{Action: "rebase_continue", Status: "in_progress", Message: "git rebase --continue"})
		continueCmd := exec.CommandContext(ctx, "git", "rebase", "--continue")
		continueCmd.Dir = workspacePath
		continueCmd.Env = append(os.Environ(), "GIT_EDITOR=true")
		continueOutput, continueErr := continueCmd.CombinedOutput()

		if continueErr == nil {
			// Continue succeeded
			if !rebaseInProgress(workspacePath) {
				emit(ResolveConflictStep{Action: "rebase_continue", Status: "done", Message: "Rebase complete"})
				break
			}
			emit(ResolveConflictStep{Action: "rebase_continue", Status: "done", Message: "Continuing to next commit"})
			nextUnmerged := m.getUnmergedFiles(ctx, workspacePath)
			if len(nextUnmerged) == 0 {
				fmt.Printf("[workspace] linear-sync-resolve-conflict: rebase in progress with no conflicts; continuing\n")
				continue
			}
			continue
		}

		// Continue failed (non-zero exit)
		if rebaseInProgress(workspacePath) {
			nextUnmerged := m.getUnmergedFiles(ctx, workspacePath)
			if len(nextUnmerged) > 0 {
				emit(ResolveConflictStep{Action: "rebase_continue", Status: "done", Message: "Next commit also has conflicts"})
				continue
			}
		}
		msg := fmt.Sprintf("git rebase --continue failed: %s", string(continueOutput))
		emit(ResolveConflictStep{Action: "rebase_continue", Status: "failed", Message: msg})
		abortAndUnwind(msg)
		return &LinearSyncResolveConflictResult{
			Success: false, Message: msg, Hash: hash, Resolutions: resolutions,
		}, nil
	}

	// Unwind WIP commit
	unwindWIP()

	return &LinearSyncResolveConflictResult{
		Success:     true,
		Message:     fmt.Sprintf("Rebased %s with %d conflict(s) resolved", hash, len(resolutions)),
		Hash:        hash,
		Resolutions: resolutions,
	}, nil
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncateString returns s truncated to maxLen characters, appending "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// getUnmergedFiles returns the list of unmerged (conflicted) file paths.
func (m *Manager) getUnmergedFiles(ctx context.Context, workspacePath string) []string {
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "--diff-filter=U")
	diffCmd.Dir = workspacePath
	diffOutput, err := diffCmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(diffOutput)), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// getRebaseHead returns the commit hash being replayed (REBASE_HEAD).
func (m *Manager) getRebaseHead(ctx context.Context, workspacePath string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "REBASE_HEAD")
	cmd.Dir = workspacePath
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// getRebaseMessage reads the commit message of the commit being replayed via git.
func (m *Manager) getRebaseMessage(ctx context.Context, workspacePath string) string {
	cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%s", "REBASE_HEAD")
	cmd.Dir = workspacePath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// rebaseInProgress returns true if a rebase is currently in progress.
// Checks both rebase-merge (interactive/merge) and rebase-apply (am/apply) backends.
func rebaseInProgress(workspacePath string) bool {
	gitDir, err := resolveGitDir(workspacePath)
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-merge")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-apply")); err == nil {
		return true
	}
	return false
}
