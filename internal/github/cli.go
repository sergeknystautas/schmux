//go:build !nogithub

package github

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CLI shells out to the gh CLI for operations that need the user's GitHub
// credentials (repo creation, owner listing). Availability is checked via
// CheckAuth before use.
type CLI struct{}

// NewCLI returns a gh CLI wrapper.
func NewCLI() *CLI { return &CLI{} }

// RepoURL returns the canonical https URL for owner/name (no .git suffix,
// matching the style of existing config repo entries).
func (c *CLI) RepoURL(owner, name string) string {
	return "https://github.com/" + owner + "/" + name
}

// CreateRepo creates owner/name on GitHub via `gh repo create`.
func (c *CLI) CreateRepo(ctx context.Context, owner, name string, private bool) error {
	vis := "--public"
	if private {
		vis = "--private"
	}
	cmd := exec.CommandContext(ctx, "gh", "repo", "create", owner+"/"+name, vis)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh repo create %s/%s failed: %w: %s", owner, name, err, strings.TrimSpace(out.String()))
	}
	return nil
}

// ListOwners returns the authenticated user's login followed by the logins of
// organizations they belong to. Org listing failures are non-fatal (an org-less
// token still yields the personal account).
func (c *CLI) ListOwners(ctx context.Context) ([]string, error) {
	owners, err := ghLines(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return nil, err
	}
	if orgs, err := ghLines(ctx, "api", "user/orgs", "--jq", ".[].login"); err == nil {
		owners = append(owners, orgs...)
	}
	return owners, nil
}

func ghLines(ctx context.Context, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s failed: %w", strings.Join(args, " "), err)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}
