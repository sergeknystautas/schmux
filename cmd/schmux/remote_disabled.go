//go:build notunnel

package main

import (
	"fmt"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

// RemoteCommand is a stub when the tunnel module is excluded.
type RemoteCommand struct{}

// NewRemoteCommand returns a disabled remote command.
func NewRemoteCommand(_ *cli.Client) *RemoteCommand {
	return &RemoteCommand{}
}

// Run prints that remote access is not available and returns an error.
func (cmd *RemoteCommand) Run(_ []string) error {
	return fmt.Errorf("remote access is not available in this build")
}
