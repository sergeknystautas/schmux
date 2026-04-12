//go:build notimelapse

package main

import "fmt"

type TimelapseCommand struct{}

func NewTimelapseCommand() *TimelapseCommand {
	return &TimelapseCommand{}
}

func (c *TimelapseCommand) Run(_ []string) error {
	return fmt.Errorf("timelapse is not available in this build")
}
