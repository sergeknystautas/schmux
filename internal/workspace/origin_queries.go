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
// These are bare clones stored in ~/.schmux/query/ used for branch/commit queries
// without needing a workspace checked out.
func (m *Manager) EnsureOriginQueries(ctx context.Context) error {
	queryRepoPath := m.config.GetQueryRepoPath()
	if queryRepoPath == "" {
		return fmt.Errorf("query repo path not configured")
	}

	// Ensure directory exists
	if err := os.MkdirAll(queryRepoPath, 0755); err != nil {
		return fmt.Errorf("failed to create query repo directory: %w", err)
	}

	for _, repo := range m.config.GetRepos() {
		queryRepoPath, err := m.ensureOriginQueryRepo(ctx, repo.URL)
		if err != nil {
			fmt.Printf("[workspace] warning: %v\n", err)
			continue
		}

		// Detect and cache default branch after ensuring readiness
		defaultBranch := m.getDefaultBranch(ctx, queryRepoPath)
		if defaultBranch != "" {
			m.setDefaultBranch(repo.URL, defaultBranch)
		}
	}

	return nil
}

func (m *Manager) ensureOriginQueryRepo(ctx context.Context, repoURL string) (string, error) {
	queryRepoDir := m.config.GetQueryRepoPath()
	if queryRepoDir == "" {
		return "", fmt.Errorf("query repo path not configured")
	}

	if err := os.MkdirAll(queryRepoDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create query repo directory: %w", err)
	}

	// Get BarePath from config
	repo, found := m.config.FindRepoByURL(repoURL)
	if !found {
		return "", fmt.Errorf("repo not found in config: %s", repoURL)
	}
	if repo.BarePath == "" {
		return "", fmt.Errorf("repo %s has no bare_path set", repo.Name)
	}

	queryRepoPath := filepath.Join(queryRepoDir, repo.BarePath)

	if _, err := os.Stat(queryRepoPath); os.IsNotExist(err) {
		fmt.Printf("[workspace] creating origin query repo: %s\n", repo.BarePath)
		// Create parent directory (e.g., ~/.schmux/query/facebook/)
		if err := os.MkdirAll(filepath.Dir(queryRepoPath), 0755); err != nil {
			return "", fmt.Errorf("failed to create query repo directory: %w", err)
		}
		if err := m.cloneOriginQueryRepo(ctx, repoURL, queryRepoPath); err != nil {
			return "", fmt.Errorf("failed to create query repo clone: %w", err)
		}
		if err := m.prepareOriginQueryRepo(ctx, queryRepoPath, repoURL); err != nil {
			return "", fmt.Errorf("failed to initialize origin query repo: %w", err)
		}
	} else {
		m.ensureCorrectOriginURL(ctx, queryRepoPath, repoURL)
		if m.originQueryRepoNeedsRepair(ctx, queryRepoPath) {
			if err := m.prepareOriginQueryRepo(ctx, queryRepoPath, repoURL); err != nil {
				return "", fmt.Errorf("failed to repair origin query repo: %w", err)
			}
		}
	}

	return queryRepoPath, nil
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

// prepareOriginQueryRepo ensures the query repo has remote tracking refs and origin/HEAD.
func (m *Manager) prepareOriginQueryRepo(ctx context.Context, queryRepoPath, repoURL string) error {
	if err := m.ensureOriginFetchRefspec(ctx, queryRepoPath); err != nil {
		return err
	}
	if err := m.fetchOriginQueryRepo(ctx, queryRepoPath, repoURL); err != nil {
		return err
	}
	if !m.originHeadExists(ctx, queryRepoPath) {
		if err := m.setOriginHead(ctx, queryRepoPath); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ensureOriginFetchRefspec(ctx context.Context, queryRepoPath string) error {
	configCmd := exec.CommandContext(ctx, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	configCmd.Dir = queryRepoPath
	if output, err := configCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config fetch refspec failed: %w: %s", err, string(output))
	}
	return nil
}

func (m *Manager) fetchOriginQueryRepo(ctx context.Context, queryRepoPath, repoURL string) error {
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(fetchCtx, "git", "fetch", "--prune", "origin")
	cmd.Dir = queryRepoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		// Try to get repo name from config for better error messages
		repoName := repoURL
		if repo, found := m.config.FindRepoByURL(repoURL); found {
			repoName = repo.Name
		}
		return fmt.Errorf("git fetch failed for origin query repo %s: %w: %s", repoName, err, string(output))
	}
	return nil
}

func (m *Manager) setOriginHead(ctx context.Context, queryRepoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "remote", "set-head", "origin", "-a")
	cmd.Dir = queryRepoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git remote set-head failed: %w: %s", err, string(output))
	}
	return nil
}

func (m *Manager) originHeadExists(ctx context.Context, queryRepoPath string) bool {
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/HEAD")
	cmd.Dir = queryRepoPath
	return cmd.Run() == nil
}

// ensureCorrectOriginURL checks that the bare repo's origin URL matches the
// expected URL from config, and corrects it if mismatched. This handles cases
// where a repo was cloned with a different protocol (e.g., SSH vs HTTPS).
func (m *Manager) ensureCorrectOriginURL(ctx context.Context, repoPath, expectedURL string) {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return
	}
	currentURL := strings.TrimSpace(string(output))
	if currentURL == expectedURL {
		return
	}
	fmt.Printf("[workspace] fixing origin URL mismatch in %s: %s -> %s\n",
		filepath.Base(repoPath), currentURL, expectedURL)
	fix := exec.CommandContext(ctx, "git", "remote", "set-url", "origin", expectedURL)
	fix.Dir = repoPath
	if out, err := fix.CombinedOutput(); err != nil {
		fmt.Printf("[workspace] warning: failed to fix origin URL: %v: %s\n", err, string(out))
	}
}

func (m *Manager) originQueryRepoNeedsRepair(ctx context.Context, queryRepoPath string) bool {
	if m.originHeadExists(ctx, queryRepoPath) {
		return false
	}

	cmd := exec.CommandContext(ctx, "git", "for-each-ref", "--count", "1", "refs/remotes/origin/")
	cmd.Dir = queryRepoPath
	output, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(output)) == ""
}

// getQueryRepoPath returns the path to the query repo using BarePath from config.
func (m *Manager) getQueryRepoPath(repoURL string) string {
	queryRepoDir := m.config.GetQueryRepoPath()

	if repo, found := m.config.FindRepoByURL(repoURL); found && repo.BarePath != "" {
		return filepath.Join(queryRepoDir, repo.BarePath)
	}

	return "" // Repo not in config or has no bare_path
}

// FetchOriginQueries fetches updates for all origin query repos.
func (m *Manager) FetchOriginQueries(ctx context.Context) {
	queryRepoDir := m.config.GetQueryRepoPath()
	if queryRepoDir == "" {
		return
	}

	for _, repo := range m.config.GetRepos() {
		queryRepoPath := m.getQueryRepoPath(repo.URL)

		// Skip if doesn't exist
		if _, err := os.Stat(queryRepoPath); os.IsNotExist(err) {
			continue
		}

		if err := m.fetchOriginQueryRepo(ctx, queryRepoPath, repo.URL); err != nil {
			fmt.Printf("[workspace] warning: %v\n", err)
			continue
		}

		if !m.originHeadExists(ctx, queryRepoPath) {
			if err := m.setOriginHead(ctx, queryRepoPath); err != nil {
				fmt.Printf("[workspace] warning: %v\n", err)
			}
		}

		// Refresh default branch cache after fetch/set-head
		defaultBranch := m.getDefaultBranch(ctx, queryRepoPath)
		if defaultBranch != "" {
			m.setDefaultBranch(repo.URL, defaultBranch)
		}
	}
}

// GetRecentBranches returns recent branches from all bare clones, sorted by commit date.
func (m *Manager) GetRecentBranches(ctx context.Context, limit int) ([]RecentBranch, error) {
	if limit <= 0 {
		limit = 10
	}

	queryRepoDir := m.config.GetQueryRepoPath()
	if queryRepoDir == "" {
		return nil, fmt.Errorf("query repo path not configured")
	}

	var allBranches []RecentBranch

	for _, repo := range m.config.GetRepos() {
		queryRepoPath := m.getQueryRepoPath(repo.URL)

		// Skip if doesn't exist
		if _, err := os.Stat(queryRepoPath); os.IsNotExist(err) {
			continue
		}

		branches, err := m.getRecentBranchesFromBare(ctx, queryRepoPath, repo.Name, repo.URL, limit)
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
func (m *Manager) getRecentBranchesFromBare(ctx context.Context, queryRepoPath, repoName, repoURL string, limit int) ([]RecentBranch, error) {
	// Get default branch from cache (populated by EnsureOriginQueries)
	defaultBranch, err := m.GetDefaultBranch(ctx, repoURL)
	if err != nil {
		return nil, fmt.Errorf("default branch unknown: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "for-each-ref",
		"--sort=-committerdate",
		"--count", strconv.Itoa(limit+5), // fetch extra to account for filtered branches
		"--format=%(refname:short)|%(committerdate:iso8601)|%(subject)",
		"refs/remotes/origin/",
	)
	cmd.Dir = queryRepoPath

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
// Returns empty string if detection fails.
func (m *Manager) getDefaultBranch(ctx context.Context, queryRepoPath string) string {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = queryRepoPath
	output, err := cmd.Output()
	if err == nil {
		// Output is like "refs/remotes/origin/main"
		ref := strings.TrimSpace(string(output))
		return strings.TrimPrefix(ref, "refs/remotes/origin/")
	}

	cmd = exec.CommandContext(ctx, "git", "symbolic-ref", "HEAD")
	cmd.Dir = queryRepoPath
	output, err = cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		if strings.HasPrefix(ref, "refs/heads/") {
			return strings.TrimPrefix(ref, "refs/heads/")
		}
		if strings.HasPrefix(ref, "refs/remotes/origin/") {
			return strings.TrimPrefix(ref, "refs/remotes/origin/")
		}
	}

	return "" // Signal failure
}

// GetBranchCommitLog returns the commit subjects for a branch relative to the default branch.
// Uses the bare clone to avoid needing a worktree checkout.
func (m *Manager) GetBranchCommitLog(ctx context.Context, repoURL, branch string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}

	queryRepoDir := m.config.GetQueryRepoPath()
	if queryRepoDir == "" {
		return nil, fmt.Errorf("query repo path not configured")
	}

	queryRepoPath := m.getQueryRepoPath(repoURL)

	if _, err := os.Stat(queryRepoPath); os.IsNotExist(err) {
		// Try to get repo name from config for better error messages
		repoName := repoURL
		if repo, found := m.config.FindRepoByURL(repoURL); found {
			repoName = repo.Name
		}
		return nil, fmt.Errorf("bare clone not found for %s", repoName)
	}

	// Get default branch from cache (populated by EnsureOriginQueries)
	defaultBranch, err := m.GetDefaultBranch(ctx, repoURL)
	if err != nil {
		return nil, fmt.Errorf("default branch unknown: %w", err)
	}

	const commitDelimiter = "---COMMIT---"
	cmd := exec.CommandContext(ctx, "git", "log",
		"--format=%B"+commitDelimiter,
		fmt.Sprintf("--max-count=%d", limit),
		fmt.Sprintf("origin/%s..origin/%s", defaultBranch, branch),
	)
	cmd.Dir = queryRepoPath

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
