//go:build norepofeed

package main

import (
	"fmt"

	"github.com/sergeknystautas/schmux/pkg/cli"
)

type RepofeedCommand struct{}

func NewRepofeedCommand(_ cli.DaemonClient) *RepofeedCommand {
	return &RepofeedCommand{}
}

func (cmd *RepofeedCommand) Run(_ []string) error {
	return fmt.Errorf("repofeed is not available in this build")
}
