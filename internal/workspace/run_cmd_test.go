package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
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

func TestRunGit_CapturesStderrOnExitError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	m := &Manager{}
	ctx := context.Background()

	_, err := m.runGit(ctx, "ws-test", RefreshTriggerExplicit, t.TempDir(), "log", "--oneline", "-1")
	if err == nil {
		t.Fatal("expected runGit to fail outside a git repo")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exec.ExitError, got %T", err)
	}
	if !strings.Contains(string(exitErr.Stderr), "not a git repository") {
		t.Fatalf("expected stderr on ExitError, got %q", string(exitErr.Stderr))
	}
}

func TestEnsureOriginQueries_SkipsLocalRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{{
		Name:     "week-map-view",
		URL:      "local:week-map-view",
		BarePath: "week-map-view.git",
	}}

	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	if err := m.EnsureOriginQueries(context.Background()); err != nil {
		t.Fatalf("EnsureOriginQueries() failed: %v", err)
	}

	queryRepoPath := filepath.Join(cfg.GetQueryRepoPath(), "week-map-view.git")
	if _, err := os.Stat(queryRepoPath); err == nil {
		t.Fatalf("expected local repo query clone to be skipped, but %s exists", queryRepoPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat query repo path: %v", err)
	}
}
