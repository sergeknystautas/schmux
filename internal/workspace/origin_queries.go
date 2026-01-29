package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EnsureOriginQueries ensures origin query repos exist for all configured repos.
// These are bare clones stored in ~/.schmux/bare/ used for branch/commit queries
// without needing a workspace checked out.
func (m *Manager) EnsureOriginQueries(ctx context.Context) error {
	bareReposPath := m.config.GetBareReposPath()
	if bareReposPath == "" {
		return fmt.Errorf("bare repos path not configured")
	}

	// Ensure directory exists
	if err := os.MkdirAll(bareReposPath, 0755); err != nil {
		return fmt.Errorf("failed to create bare repos directory: %w", err)
	}

	for _, repo := range m.config.GetRepos() {
		repoName := extractRepoName(repo.URL)
		bareRepoPath := filepath.Join(bareReposPath, repoName+".git")

		// Skip if already exists
		if _, err := os.Stat(bareRepoPath); err == nil {
			continue
		}

		// Clone as bare repo for origin queries
		fmt.Printf("[workspace] creating origin query repo: %s\n", repo.Name)
		if err := m.cloneOriginQueryRepo(ctx, repo.URL, bareRepoPath); err != nil {
			fmt.Printf("[workspace] warning: failed to create bare clone for %s: %v\n", repo.Name, err)
			continue
		}
	}

	return nil
}

// cloneOriginQueryRepo clones a repository as a bare clone for branch/commit querying.
func (m *Manager) cloneOriginQueryRepo(ctx context.Context, url, path string) error {
	args := []string{"clone", "--bare", url, path}
	cmd := exec.CommandContext(ctx, "git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone --bare failed: %w: %s", err, string(output))
	}

	// Configure fetch refspec so 'git fetch' updates remote tracking branches
	configCmd := exec.CommandContext(ctx, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	configCmd.Dir = path
	if output, err := configCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config fetch refspec failed: %w: %s", err, string(output))
	}

	return nil
}

// FetchOriginQueries fetches updates for all origin query repos.
func (m *Manager) FetchOriginQueries(ctx context.Context) {
	bareReposPath := m.config.GetBareReposPath()
	if bareReposPath == "" {
		return
	}

	for _, repo := range m.config.GetRepos() {
		repoName := extractRepoName(repo.URL)
		bareRepoPath := filepath.Join(bareReposPath, repoName+".git")

		// Skip if doesn't exist
		if _, err := os.Stat(bareRepoPath); os.IsNotExist(err) {
			continue
		}

		// Fetch with short timeout
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		cmd := exec.CommandContext(fetchCtx, "git", "fetch", "--prune", "origin")
		cmd.Dir = bareRepoPath
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("[workspace] warning: fetch failed for origin query repo %s: %v: %s\n", repo.Name, err, string(output))
		}
		cancel()
	}
}

// GetRecentBranches returns recent branches from all bare clones, sorted by commit date.
func (m *Manager) GetRecentBranches(ctx context.Context, limit int) ([]RecentBranch, error) {
	if limit <= 0 {
		limit = 10
	}

	bareReposPath := m.config.GetBareReposPath()
	if bareReposPath == "" {
		return nil, fmt.Errorf("bare repos path not configured")
	}

	var allBranches []RecentBranch

	for _, repo := range m.config.GetRepos() {
		repoName := extractRepoName(repo.URL)
		bareRepoPath := filepath.Join(bareReposPath, repoName+".git")

		// Skip if doesn't exist
		if _, err := os.Stat(bareRepoPath); os.IsNotExist(err) {
			continue
		}

		branches, err := m.getRecentBranchesFromBare(ctx, bareRepoPath, repo.Name, repo.URL, limit)
		if err != nil {
			fmt.Printf("[workspace] warning: failed to get branches from %s: %v\n", repo.Name, err)
			continue
		}

		allBranches = append(allBranches, branches...)
	}

	// Sort all branches by commit date (most recent first)
	sort.Slice(allBranches, func(i, j int) bool {
		return allBranches[i].CommitDate > allBranches[j].CommitDate
	})

	// Limit total results
	if len(allBranches) > limit {
		allBranches = allBranches[:limit]
	}

	return allBranches, nil
}

// getRecentBranchesFromBare queries a bare clone for recent branches.
func (m *Manager) getRecentBranchesFromBare(ctx context.Context, bareRepoPath, repoName, repoURL string, limit int) ([]RecentBranch, error) {
	// First, detect the default branch
	defaultBranch := m.getDefaultBranch(ctx, bareRepoPath)

	cmd := exec.CommandContext(ctx, "git", "for-each-ref",
		"--sort=-committerdate",
		"--count", strconv.Itoa(limit+5), // fetch extra to account for filtered branches
		"--format=%(refname:short)|%(committerdate:iso8601)|%(subject)",
		"refs/remotes/origin/",
	)
	cmd.Dir = bareRepoPath

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref failed: %w", err)
	}

	var branches []RecentBranch
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}

		branchRef := parts[0]
		commitDate := parts[1]
		subject := ""
		if len(parts) >= 3 {
			subject = parts[2]
		}

		// Remove "origin/" prefix
		branch := strings.TrimPrefix(branchRef, "origin/")

		// Skip HEAD and origin (these are refs, not branches)
		if branch == "HEAD" || branch == "origin" {
			continue
		}

		// Skip the default branch (main/master/etc)
		if branch == defaultBranch {
			continue
		}

		branches = append(branches, RecentBranch{
			RepoName:   repoName,
			RepoURL:    repoURL,
			Branch:     branch,
			CommitDate: commitDate,
			Subject:    subject,
		})

		// Stop if we have enough
		if len(branches) >= limit {
			break
		}
	}

	return branches, nil
}

// getDefaultBranch detects the default branch for a bare repo.
func (m *Manager) getDefaultBranch(ctx context.Context, bareRepoPath string) string {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = bareRepoPath
	output, err := cmd.Output()
	if err != nil {
		// Fallback: assume main or master
		return "main"
	}
	// Output is like "refs/remotes/origin/main"
	ref := strings.TrimSpace(string(output))
	return strings.TrimPrefix(ref, "refs/remotes/origin/")
}

// GetBranchCommitLog returns the commit subjects for a branch relative to the default branch.
// Uses the bare clone to avoid needing a worktree checkout.
func (m *Manager) GetBranchCommitLog(ctx context.Context, repoURL, branch string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}

	bareReposPath := m.config.GetBareReposPath()
	if bareReposPath == "" {
		return nil, fmt.Errorf("bare repos path not configured")
	}

	repoName := extractRepoName(repoURL)
	bareRepoPath := filepath.Join(bareReposPath, repoName+".git")

	if _, err := os.Stat(bareRepoPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("bare clone not found for %s", repoName)
	}

	defaultBranch := m.getDefaultBranch(ctx, bareRepoPath)

	const commitDelimiter = "---COMMIT---"
	cmd := exec.CommandContext(ctx, "git", "log",
		"--format=%B"+commitDelimiter,
		fmt.Sprintf("--max-count=%d", limit),
		fmt.Sprintf("origin/%s..origin/%s", defaultBranch, branch),
	)
	cmd.Dir = bareRepoPath

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var messages []string
	for _, msg := range strings.Split(string(output), commitDelimiter) {
		trimmed := strings.TrimSpace(msg)
		if trimmed != "" {
			messages = append(messages, trimmed)
		}
	}

	return messages, nil
}
