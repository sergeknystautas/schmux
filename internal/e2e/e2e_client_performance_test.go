//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestE2EConfigFileEditReconcilesClientPerformance hand-edits config.json while
// the daemon runs. The daemon must keep the client-performance workspace's name
// while the feature is on and drop it when the edit turns the feature off.
func TestE2EConfigFileEditReconcilesClientPerformance(t *testing.T) {
	t.Parallel()
	env := New(t)
	const dedicated = "client-performance"
	configPath := filepath.Join(env.HomeDir, ".schmux", "config.json")
	setEnabled := func(enabled bool) {
		t.Helper()
		cfg, err := config.Load(configPath)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		cfg.ClientPerformance = &config.ClientPerformanceConfig{Enabled: &enabled, Repo: "perf-repo", Target: "echo"}
		if err := cfg.Save(); err != nil {
			t.Fatalf("save config: %v", err)
		}
	}

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)
	repoPath := env.SetupTestRepo(workspaceRoot, "perf-repo")
	setEnabled(true)
	env.DaemonStart()
	defer func() {
		if env.HealthCheck() {
			env.DaemonStop()
		}
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	sessionID := env.SpawnSession("file://"+repoPath, "main", "echo", "", env.Nickname("perf"))
	workspaceID := env.GetWorkspaceIDForSession(sessionID)

	// Give the workspace the name a Launch would have given it; Launch itself
	// needs dev mode and a chat agent.
	env.DaemonStop()
	st, err := state.Load(filepath.Join(env.HomeDir, ".schmux", "state.json"), nil)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	ws, found := st.GetWorkspace(workspaceID)
	if !found {
		t.Fatalf("workspace %s missing from state", workspaceID)
	}
	ws.Dedicated = dedicated
	if err := st.UpdateWorkspace(ws); err != nil {
		t.Fatalf("update workspace: %v", err)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
	env.DaemonStart()

	nameOf := func(workspaces []APIWorkspace) (string, bool) {
		for _, w := range workspaces {
			if w.ID == workspaceID {
				return w.Dedicated, true
			}
		}
		return "", false
	}
	if name, found := nameOf(env.GetAPIWorkspaces()); !found || name != dedicated {
		t.Fatalf("after startup: workspace found=%v name=%q, want %q", found, name, dedicated)
	}

	conn, err := env.ConnectDashboardWebSocket()
	if err != nil {
		t.Fatalf("connect dashboard websocket: %v", err)
	}
	defer conn.Close()

	setEnabled(false)
	deadline := time.Now().Add(15 * time.Second) // failure backstop
	last := "no sessions message"
	for {
		msg, err := env.ReadDashboardMessage(conn, time.Until(deadline))
		if err != nil {
			t.Fatalf("workspace %s kept its name after the edit; last observed %s: %v", workspaceID, last, err)
		}
		if msg.Type != "sessions" {
			continue
		}
		name, found := nameOf(msg.Workspaces)
		if found && name == "" {
			return
		}
		last = "name " + name
	}
}
