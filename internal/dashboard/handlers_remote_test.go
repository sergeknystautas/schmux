package dashboard

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

// TestConnectProgressSSEDisconnect verifies that when an SSE client disconnects
// mid-provisioning, the goroutine exits and doesn't leak (Issue 3 fix).
// This tests the cleanup pattern used in handleRemoteConnectStream.
func TestConnectProgressSSEDisconnect(t *testing.T) {
	// Simulate the SSE handler pattern
	progressCh := make(chan string, 10)
	doneCh := make(chan struct{})
	cleanupOnce := sync.Once{}

	cleanup := func() {
		cleanupOnce.Do(func() {
			go func() {
				for range progressCh {
					// Discard
				}
			}()
			close(doneCh)
		})
	}

	// Create a context we can cancel (simulates client disconnect)
	ctx, cancel := context.WithCancel(context.Background())

	// Start the goroutine (simulates ConnectWithProgress goroutine)
	goroutineDone := make(chan struct{})
	goroutineStarted := make(chan struct{})
	go func() {
		defer close(goroutineDone)
		close(goroutineStarted) // Signal that goroutine is running

		// Simulate slow provisioning
		for i := 0; i < 10; i++ {
			select {
			case progressCh <- "progress":
			case <-doneCh:
				// Cleanup called, stop
				return
			case <-ctx.Done():
				// Context cancelled, stop
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(progressCh)
	}()

	// Wait for goroutine to start (instead of time.Sleep)
	<-goroutineStarted

	// Simulate client disconnect
	cancel()
	cleanup()

	// Verify goroutine exits
	select {
	case <-goroutineDone:
		// Good - goroutine exited
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not exit after cleanup (goroutine leak)")
	}
}

// TestCleanupOnceIdempotent verifies cleanup can be called multiple times safely
func TestCleanupOnceIdempotent(t *testing.T) {
	progressCh := make(chan string, 10)
	doneCh := make(chan struct{})
	cleanupOnce := sync.Once{}

	cleanup := func() {
		cleanupOnce.Do(func() {
			go func() {
				for range progressCh {
				}
			}()
			close(doneCh)
		})
	}

	// Call cleanup multiple times
	cleanup()
	cleanup()
	cleanup()

	// Verify doneCh is closed exactly once (wouldn't panic on second close if Once works)
	select {
	case <-doneCh:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("doneCh should be closed")
	}
}

func TestToFlavorResponse_AllFields(t *testing.T) {
	f := config.RemoteFlavor{
		ID:                    "devvm",
		Flavor:                "devvm",
		DisplayName:           "DevVM",
		VCS:                   "hg",
		WorkspacePath:         "/data/users/$USER",
		ConnectCommand:        "ssh $HOST",
		ReconnectCommand:      "ssh $HOST",
		ProvisionCommand:      "setup.sh",
		HostnameRegex:         `devvm\d+\.example\.com`,
		VSCodeCommandTemplate: "code --remote ssh-remote+$HOST",
	}

	resp := toFlavorResponse(f)

	if resp.ID != f.ID {
		t.Errorf("ID: got %q, want %q", resp.ID, f.ID)
	}
	if resp.Flavor != f.Flavor {
		t.Errorf("Flavor: got %q, want %q", resp.Flavor, f.Flavor)
	}
	if resp.DisplayName != f.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", resp.DisplayName, f.DisplayName)
	}
	if resp.VCS != f.VCS {
		t.Errorf("VCS: got %q, want %q", resp.VCS, f.VCS)
	}
	if resp.WorkspacePath != f.WorkspacePath {
		t.Errorf("WorkspacePath: got %q, want %q", resp.WorkspacePath, f.WorkspacePath)
	}
	if resp.ConnectCommand != f.ConnectCommand {
		t.Errorf("ConnectCommand: got %q, want %q", resp.ConnectCommand, f.ConnectCommand)
	}
	if resp.ReconnectCommand != f.ReconnectCommand {
		t.Errorf("ReconnectCommand: got %q, want %q", resp.ReconnectCommand, f.ReconnectCommand)
	}
	if resp.ProvisionCommand != f.ProvisionCommand {
		t.Errorf("ProvisionCommand: got %q, want %q", resp.ProvisionCommand, f.ProvisionCommand)
	}
	if resp.HostnameRegex != f.HostnameRegex {
		t.Errorf("HostnameRegex: got %q, want %q", resp.HostnameRegex, f.HostnameRegex)
	}
	if resp.VSCodeCommandTemplate != f.VSCodeCommandTemplate {
		t.Errorf("VSCodeCommandTemplate: got %q, want %q", resp.VSCodeCommandTemplate, f.VSCodeCommandTemplate)
	}
}
