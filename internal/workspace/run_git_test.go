package workspace

import (
	"context"
	"os/exec"
	"testing"
)

func TestRunGit_RecordsTelemetry(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	m := &Manager{}
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()
	_, err := m.runGit(ctx, "ws-test", RefreshTriggerExplicit, t.TempDir(), "version")
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			t.Skip("git not available")
		}
		t.Fatalf("runGit failed: %v", err)
	}

	snap := tel.Snapshot(false)
	if snap.TotalCommands != 1 {
		t.Fatalf("expected 1 command recorded, got %d", snap.TotalCommands)
	}
	if snap.Counters["git_version"] != 1 {
		t.Fatalf("expected git_version counter = 1, got %d", snap.Counters["git_version"])
	}
}

func TestRunGit_NilTelemetry(t *testing.T) {
	m := &Manager{}
	ctx := context.Background()
	_, err := m.runGit(ctx, "ws-test", RefreshTriggerExplicit, t.TempDir(), "version")
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			t.Skip("git not available")
		}
		t.Fatalf("runGit failed: %v", err)
	}
}

func TestRunGit_CapturesExitCode(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	m := &Manager{}
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()
	// This should fail with non-zero exit (not a git repo)
	_, _ = m.runGit(ctx, "ws-test", RefreshTriggerExplicit, t.TempDir(), "log", "--oneline", "-1")

	snap := tel.Snapshot(false)
	if snap.TotalCommands != 1 {
		t.Fatalf("expected 1 command recorded, got %d", snap.TotalCommands)
	}
	// Running git log in a temp dir that's not a repo should produce a non-zero exit code
	if len(snap.AllCommands) == 0 {
		t.Fatal("expected at least 1 command recorded in AllCommands")
	}
	if snap.AllCommands[0].ExitCode == 0 {
		t.Error("expected non-zero exit code for git log in a non-repo directory")
	}
}
