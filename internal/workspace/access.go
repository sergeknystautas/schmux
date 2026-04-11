package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProbeResult contains the result of a repo access probe.
type ProbeResult struct {
	Accessible    bool   `json:"accessible"`
	DefaultBranch string `json:"default_branch"`
	VCS           string `json:"vcs,omitempty"` // "git" or "sapling"
	Error         string `json:"error,omitempty"`
	ErrorType     string `json:"error_type,omitempty"` // "ssh", "auth", "network", "timeout"
}

// ProbeRepoAccess checks that a repo URL or local path is accessible and
// detects the default branch. The context should carry a timeout (recommended 10s).
func ProbeRepoAccess(ctx context.Context, repoURL string) ProbeResult {
	// Local path: verify VCS markers exist, no network probe needed.
	if isLocalPath(repoURL) {
		return probeLocalRepo(ctx, repoURL)
	}

	// Remote URL: use git ls-remote.
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--symref", repoURL, "HEAD")
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return ProbeResult{
			Accessible: false,
			Error:      classifyGitError(string(output), err),
			ErrorType:  classifyGitErrorType(string(output), err),
		}
	}

	return ProbeResult{
		Accessible:    true,
		DefaultBranch: parseDefaultBranch(string(output)),
		VCS:           "git",
	}
}

// isLocalPath returns true if the string looks like a filesystem path.
func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, ".")
}

// probeLocalRepo checks a local directory for VCS markers and detects the default branch.
func probeLocalRepo(ctx context.Context, path string) ProbeResult {
	// Check for git repo
	if info, err := os.Stat(filepath.Join(path, ".git")); err == nil && info.IsDir() {
		branch := detectLocalGitBranch(ctx, path)
		return ProbeResult{Accessible: true, DefaultBranch: branch, VCS: "git"}
	}
	// Check for sapling repo (.sl or .hg)
	for _, marker := range []string{".sl", ".hg"} {
		if info, err := os.Stat(filepath.Join(path, marker)); err == nil && info.IsDir() {
			branch := detectLocalSaplingBranch(ctx, path)
			return ProbeResult{Accessible: true, DefaultBranch: branch, VCS: "sapling"}
		}
	}
	return ProbeResult{
		Accessible: false,
		Error:      "Not a recognized repository — no .git, .sl, or .hg directory found.",
		ErrorType:  "unknown",
	}
}

func detectLocalGitBranch(ctx context.Context, path string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "symbolic-ref", "--short", "HEAD")
	if out, err := cmd.Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			return b
		}
	}
	return "main"
}

func detectLocalSaplingBranch(ctx context.Context, path string) string {
	// Sapling bookmark or branch detection
	cmd := exec.CommandContext(ctx, "sl", "--cwd", path, "log", "-r", ".", "-T", "{activebookmark}")
	if out, err := cmd.Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			return b
		}
	}
	return "main"
}

func parseDefaultBranch(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "ref: refs/heads/") {
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) >= 1 {
				branch := strings.TrimPrefix(parts[0], "ref: refs/heads/")
				if branch != "" {
					return branch
				}
			}
		}
	}
	return "main"
}

func classifyGitError(output string, err error) string {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "publickey") {
		return "Can't connect — make sure your SSH key is added to this host."
	}
	if strings.Contains(lower, "authentication failed") || strings.Contains(lower, "invalid credentials") {
		return "Authentication failed — check your credentials or try an SSH URL."
	}
	if strings.Contains(lower, "could not resolve host") || strings.Contains(lower, "network is unreachable") {
		return "Can't reach this URL — check your network connection."
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "context deadline exceeded") {
		return "Connection timed out — check your network or try a different URL."
	}
	return fmt.Sprintf("Repository access failed: %s", strings.TrimSpace(output))
}

func classifyGitErrorType(output string, err error) string {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "publickey") {
		return "ssh"
	}
	if strings.Contains(lower, "authentication failed") {
		return "auth"
	}
	if strings.Contains(lower, "could not resolve host") || strings.Contains(lower, "network is unreachable") {
		return "network"
	}
	if strings.Contains(err.Error(), "context deadline exceeded") {
		return "timeout"
	}
	return "unknown"
}
