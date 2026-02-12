package main

import (
	"testing"
)

func TestAnalyzeRepoCommandRun(t *testing.T) {
	mock := &MockDaemonClient{
		isRunning: true,
	}
	cmd := NewAnalyzeRepoCommand(mock)
	if err := cmd.Run([]string{"testrepo", "--depth", "10"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestAnalyzeRepoCommandUsage(t *testing.T) {
	cmd := NewAnalyzeRepoCommand(&MockDaemonClient{})
	if err := cmd.Run([]string{}); err == nil {
		t.Fatalf("expected usage error for empty args")
	}
}

func TestAnalyzeRepoCommandDaemonNotRunning(t *testing.T) {
	cmd := NewAnalyzeRepoCommand(&MockDaemonClient{isRunning: false})
	if err := cmd.Run([]string{"testrepo"}); err == nil {
		t.Fatalf("expected error when daemon is not running")
	}
}

func TestAnalyzeRepoCommandAnalyzeError(t *testing.T) {
	cmd := NewAnalyzeRepoCommand(&MockDaemonClient{
		isRunning:      true,
		analyzeRepoErr: testingErr("bad repo"),
	})
	if err := cmd.Run([]string{"missing-repo"}); err == nil {
		t.Fatalf("expected analyze error")
	}
}

type testingErr string

func (e testingErr) Error() string { return string(e) }
