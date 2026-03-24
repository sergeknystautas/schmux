//go:build !nogithub

package github

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// usernamePatterns matches the username from gh auth status output.
// Newer format: "account USERNAME (keyring)"
// Older format: "as USERNAME (oauth_token)"
var usernamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`account\s+(\S+)`),
	regexp.MustCompile(`\bas\s+(\S+)`),
}

// CheckAuth checks whether the gh CLI is installed and authenticated.
func CheckAuth(ctx context.Context) contracts.GitHubStatus {
	if _, err := exec.LookPath("gh"); err != nil {
		return contracts.GitHubStatus{}
	}

	cmd := exec.CommandContext(ctx, "gh", "auth", "status")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return contracts.GitHubStatus{}
	}

	// gh auth status writes to stdout (newer versions) or stderr (older versions).
	output := stdout.String() + stderr.String()
	username := parseUsername(output)

	return contracts.GitHubStatus{
		Available: true,
		Username:  username,
	}
}

// parseUsername extracts the GitHub username from gh auth status output.
func parseUsername(output string) string {
	for _, re := range usernamePatterns {
		if m := re.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}
