//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestE2ERemoteSSHSmoke is a slower smoke test that validates remote workspaces over actual SSH.
// This test connects to localhost via SSH, which provides the most realistic validation.
//
// Prerequisites:
// - SSH server running on localhost
// - SSH keys configured (done in Dockerfile.e2e)
func TestE2ERemoteSSHSmoke(t *testing.T) {
	t.Parallel()

	// Check if SSH server is available
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "localhost", "echo", "test")
	err := cmd.Run()
	cancel()
	if err != nil {
		t.Skipf("SSH to localhost not available (this is expected in non-SSH Docker builds): %v", err)
	}

	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	var profileID string
	t.Run("AddSSHRemoteProfile", func(t *testing.T) {
		// SSH to localhost with strict host key checking disabled for testing.
		// -tt forces remote PTY allocation, which tmux needs even in control mode.
		profileID = env.AddRemoteProfileToConfig(
			"localhost",
			"Localhost via SSH (E2E Test)",
			"/tmp/ssh-test-workspace",
			"ssh -tt -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null localhost",
		)
		if profileID == "" {
			t.Fatal("Expected profile ID, got empty")
		}
		t.Logf("SSH profile ID: %s", profileID)
	})

	t.Run("DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	var sessionID string
	t.Run("SpawnRemoteSessionViaSSH", func(t *testing.T) {
		t.Log("Spawning remote session via SSH (this may take a few seconds)...")
		sessionID = env.SpawnRemoteSession(profileID, "echo", "", env.Nickname("ssh-test"))
		if sessionID == "" {
			t.Fatal("Expected session ID from SSH remote spawn")
		}
		t.Logf("SSH remote session spawned: %s", sessionID)
	})

	t.Run("WaitForSSHConnection", func(t *testing.T) {
		// SSH connection may take longer than mock
		host := env.WaitForRemoteHostStatus(profileID, "connected", 30*time.Second)
		if host == nil {
			t.Fatal("SSH remote host did not connect")
		}
		t.Logf("SSH remote host connected: %s (hostname: %s)", host.ID, host.Hostname)

		// Verify hostname is non-empty. In Docker, SSH to localhost resolves the
		// hostname via tmux's #{host} fallback, which returns the container ID
		// (e.g., "ef73ceafd16d") rather than "localhost". Any non-empty hostname
		// is valid — it confirms the tmux hostname extraction worked.
		if host.Hostname == "" {
			t.Error("expected non-empty hostname after SSH connection")
		}
	})

	t.Run("WaitForSessionRunning", func(t *testing.T) {
		sess := env.WaitForSessionRunning(sessionID, 15*time.Second)
		if sess == nil {
			t.Fatal("SSH session did not become running")
		}
		t.Logf("SSH session running: %s", sess.ID)
	})

	t.Run("VerifySessionInAPI", func(t *testing.T) {
		sessions := env.GetAPISessions()
		found := false
		for _, sess := range sessions {
			if sess.ID == sessionID {
				found = true
				if !sess.Running {
					t.Error("SSH session should be running")
				}
			}
		}
		if !found {
			t.Error("SSH session not found in API response")
		}
	})

	t.Run("DisposeSession", func(t *testing.T) {
		env.DisposeSession(sessionID)

		// Poll until session is gone
		env.PollUntil(5*time.Second, "session disposed", func() bool {
			sessions := env.GetAPISessions()
			for _, sess := range sessions {
				if sess.ID == sessionID {
					return false
				}
			}
			return true
		})
	})

	t.Run("VerifyHostExistsAfterLastSessionDisposed", func(t *testing.T) {
		// After disposing the last session, the host should still exist
		// with a valid status (either connected or disconnected depending on timing)
		hosts := env.GetRemoteHosts()
		found := false
		for _, host := range hosts {
			if host.ProfileID == profileID {
				found = true
				t.Logf("Host status after dispose: %s", host.Status)
				if host.Status != "connected" && host.Status != "disconnected" {
					t.Errorf("expected host status to be 'connected' or 'disconnected', got %q", host.Status)
				}
			}
		}
		if !found {
			t.Error("expected remote host to still exist after session dispose")
		}
	})
}
