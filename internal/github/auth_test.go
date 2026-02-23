package github

import (
	"context"
	"os/exec"
	"testing"
)

func TestParseUsername(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantUser string
	}{
		{
			name:     "newer gh format with account",
			output:   "github.com\n  ✓ Logged in to github.com account sergeknystautas (keyring)\n  - Active account: true\n",
			wantUser: "sergeknystautas",
		},
		{
			name:     "older gh format with as",
			output:   "github.com\n  ✓ Logged in to github.com as sergeknystautas (oauth_token)\n  ✓ Git operations for github.com configured to use https protocol.\n",
			wantUser: "sergeknystautas",
		},
		{
			name:     "empty output",
			output:   "",
			wantUser: "",
		},
		{
			name:     "not authenticated output",
			output:   "You are not logged into any GitHub hosts. Run gh auth login to authenticate.\n",
			wantUser: "",
		},
		{
			name:     "account with different username",
			output:   "github.com\n  ✓ Logged in to github.com account octocat (keyring)\n",
			wantUser: "octocat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUsername(tt.output)
			if got != tt.wantUser {
				t.Errorf("parseUsername() = %q, want %q", got, tt.wantUser)
			}
		})
	}
}

func TestCheckAuth_Integration(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh CLI not available, skipping integration test")
	}

	status := CheckAuth(context.Background())

	// gh is installed, so Available should be true (assuming the test
	// environment is authenticated). If not authenticated, CheckAuth
	// returns Available: false, which is also a valid outcome.
	if !status.Available {
		t.Log("gh CLI found but not authenticated (this is OK in CI)")
		return
	}

	if status.Username == "" {
		t.Error("CheckAuth() returned Available=true but empty Username")
	}
	t.Logf("Authenticated as: %s", status.Username)
}
