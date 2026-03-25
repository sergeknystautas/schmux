//go:build nogithub

package main

import "fmt"

// AuthGitHubCommand is a stub when the GitHub module is excluded.
type AuthGitHubCommand struct{}

// NewAuthGitHubCommand returns a disabled auth github command.
func NewAuthGitHubCommand() *AuthGitHubCommand {
	return &AuthGitHubCommand{}
}

// Run prints that GitHub auth is not available and returns an error.
func (cmd *AuthGitHubCommand) Run(_ []string) error {
	return fmt.Errorf("GitHub authentication is not available in this build")
}
