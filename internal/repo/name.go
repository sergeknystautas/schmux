package repo

import (
	"fmt"
	"regexp"
	"strings"
)

// repoNameInvalidChars matches every character outside the valid repo-name
// set: ASCII letters, digits, "-" and "_".
var repoNameInvalidChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// maxRepoNameLen matches the {repo} route-parameter limit in
// dashboard.isValidRepoName and validateSpawnRepo.
const maxRepoNameLen = 128

// SanitizeRepoName rewrites a repo name to the valid character set: ASCII
// letters, digits, "-" and "_". The name becomes a workspace ID prefix (a
// directory and a tmux session name), a bare-repo directory, per-repo
// directories under ~/.schmux, and a {repo} API route segment; this set is
// accepted by every one of those. Every other character becomes "-", the
// result is cut to 128 characters, and leading and trailing "-" are trimmed
// (tmux rejects a leading "-"), so ".github" → "github".
func SanitizeRepoName(name string) string {
	result := repoNameInvalidChars.ReplaceAllString(name, "-")
	if len(result) > maxRepoNameLen {
		result = result[:maxRepoNameLen]
	}
	return strings.Trim(result, "-")
}

// repoNameFromURL generates a human-readable repo name from a git URL.
// It uses the last path segment (stripped of .git), lowercased, reduced to
// the valid repo-name characters (SanitizeRepoName).
// On collision with existingNames, it prepends the owner (truncated to 6 chars).
// If that still collides, it appends a numeric suffix (-2, -3, etc.).
func repoNameFromURL(url string, existingNames []string) string {
	repo, owner := extractRepoAndOwner(url)
	repo = SanitizeRepoName(repo)
	owner = SanitizeRepoName(owner)

	nameSet := make(map[string]bool, len(existingNames))
	for _, n := range existingNames {
		nameSet[n] = true
	}

	// Try repo name alone
	candidate := repo
	if !nameSet[candidate] {
		return candidate
	}

	// Try owner-repo (if owner available)
	if owner != "" {
		if len(owner) > 6 {
			owner = owner[:6]
		}
		candidate = owner + "-" + repo
		if !nameSet[candidate] {
			return candidate
		}
	}

	// Numeric suffix
	for i := 2; ; i++ {
		suffixed := fmt.Sprintf("%s-%d", candidate, i)
		if !nameSet[suffixed] {
			return suffixed
		}
	}
}

// extractRepoAndOwner parses a git URL and returns (repo, owner), both lowercased.
// For "git@github.com:anthropics/claude-code.git" → ("claude-code", "anthropics").
// For URLs with only one path segment, owner is empty.
func extractRepoAndOwner(url string) (string, string) {
	// Normalize SSH-style URLs: git@host:path → path
	path := url
	if idx := strings.Index(path, "://"); idx >= 0 {
		path = path[idx+3:]
	}
	if strings.HasPrefix(path, "git@") {
		if idx := strings.Index(path, ":"); idx >= 0 {
			path = path[idx+1:]
		}
	} else {
		// Remove host for http/ssh URLs
		if idx := strings.Index(path, "/"); idx >= 0 {
			path = path[idx+1:]
		}
	}

	// Strip .git suffix
	path = strings.TrimSuffix(path, ".git")

	// Split into segments
	segments := strings.Split(path, "/")

	var repo, owner string
	if len(segments) >= 2 {
		repo = segments[len(segments)-1]
		owner = segments[len(segments)-2]
	} else if len(segments) == 1 {
		repo = segments[0]
	}

	return strings.ToLower(repo), strings.ToLower(owner)
}
