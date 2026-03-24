//go:build nodashboardsx

package main

import "fmt"

// DashboardSXCommand is a stub when the dashboardsx module is excluded.
type DashboardSXCommand struct{}

// NewDashboardSXCommand returns a disabled dashboardsx command.
func NewDashboardSXCommand() *DashboardSXCommand {
	return &DashboardSXCommand{}
}

// Run prints that dashboardsx is not available and returns an error.
func (cmd *DashboardSXCommand) Run(_ []string) error {
	return fmt.Errorf("dashboardsx is not available in this build")
}
