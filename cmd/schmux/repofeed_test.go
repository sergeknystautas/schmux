package main

import (
	"testing"
)

func TestRepofeedCommand_DaemonNotRunning(t *testing.T) {
	client := &MockDaemonClient{isRunning: false}
	cmd := NewRepofeedCommand(client)
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("expected error when daemon not running")
	}
}
