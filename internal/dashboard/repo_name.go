package dashboard

import "strings"

// isGitURL returns true if the input looks like a git remote URL.
func isGitURL(s string) bool {
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		if strings.HasPrefix(s, prefix) {
			// Must have a path component after the host
			rest := s[len(prefix):]
			return strings.Contains(rest, "/")
		}
	}
	if strings.HasPrefix(s, "git@") {
		return strings.Contains(s, ":")
	}
	return false
}
