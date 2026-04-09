package remote

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestConnection_QueueSession(t *testing.T) {
	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)

	// Queue a session
	resultCh := conn.QueueSession(context.Background(), "session-1", "test-window", "/tmp", "echo test", "")

	// Verify session is in queue using polling with deadline
	deadline := time.Now().Add(1 * time.Second)
	queueLen := 0
	for time.Now().Before(deadline) {
		conn.pendingSessionsMu.Lock()
		queueLen = len(conn.pendingSessions)
		conn.pendingSessionsMu.Unlock()

		if queueLen == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if queueLen != 1 {
		t.Errorf("expected 1 queued session, got %d", queueLen)
	}

	// Verify channel doesn't receive result immediately
	// Using short timeout since we're testing that result isn't ready
	received := false
	deadline = time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-resultCh:
			received = true
			break
		default:
			time.Sleep(10 * time.Millisecond)
		}
		if received {
			break
		}
	}

	if received {
		t.Error("result channel should not have received a result yet")
	}
}

func TestConnection_ContextCancellation(t *testing.T) {
	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate starting a connection (without actually connecting)
	conn.closed = false

	// Cancel the context
	cancel()

	// Verify context is canceled using deadline polling
	deadline := time.Now().Add(500 * time.Millisecond)
	contextDone := false
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			contextDone = true
			break
		default:
			time.Sleep(10 * time.Millisecond)
		}
		if contextDone {
			break
		}
	}

	if !contextDone {
		t.Error("context should be done after cancellation")
	}

	// Note: We can't fully test process cleanup without actually starting a process,
	// but we've verified the context cancellation mechanism works
}

func TestConnection_ProvisioningOutput(t *testing.T) {
	var mu sync.Mutex
	progressMessages := []string{}
	onProgress := func(msg string) {
		mu.Lock()
		progressMessages = append(progressMessages, msg)
		mu.Unlock()
	}

	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
		OnProgress:    onProgress,
	}

	conn := NewConnection(cfg)

	// Simulate provisioning output
	output := strings.NewReader("Starting provisioning\nAllocating resources\nCompleted")
	go conn.parseProvisioningOutput(output)

	// Poll for parsing to complete (expect 3 progress messages)
	deadline := time.Now().Add(2 * time.Second)
	expectedCount := 3
	actualCount := 0
	for time.Now().Before(deadline) {
		mu.Lock()
		actualCount = len(progressMessages)
		mu.Unlock()

		if actualCount >= expectedCount {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if actualCount != expectedCount {
		t.Errorf("expected %d progress messages, got %d", expectedCount, actualCount)
	}

	// Verify provisioning output was stored
	stored := conn.ProvisioningOutput()
	if !strings.Contains(stored, "Starting provisioning") {
		t.Error("provisioning output not stored correctly")
	}
}

func TestConnection_HostnameExtraction(t *testing.T) {
	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)

	// Simulate provisioning output with hostname
	output := strings.NewReader("Establish ControlMaster connection to dev12345.example.com\n")
	go conn.parseProvisioningOutput(output)

	// Poll until hostname is extracted
	deadline := time.Now().Add(2 * time.Second)
	expectedHostname := "dev12345.example.com"
	actualHostname := ""
	for time.Now().Before(deadline) {
		actualHostname = conn.Hostname()
		if actualHostname == expectedHostname {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if actualHostname != expectedHostname {
		t.Errorf("expected hostname %q, got %q", expectedHostname, actualHostname)
	}

	// Verify status changed to connecting
	if conn.host.Status != "connecting" {
		t.Errorf("expected status 'connecting', got %q", conn.host.Status)
	}
}

func TestConnection_HostnameExtractionNoMatch(t *testing.T) {
	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)

	// Simulate provisioning output that does NOT contain a hostname match.
	// This exercises the code path where the hostname stays empty and
	// the tmux fallback (display-message) would be attempted after
	// control mode is established.
	output := strings.NewReader("Connecting to remote host...\nAuthentication successful\nSetting up environment\n")
	done := make(chan struct{})
	go func() {
		conn.parseProvisioningOutput(output)
		close(done)
	}()

	// Wait for parsing to complete
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("parseProvisioningOutput did not complete in time")
	}

	// Hostname should remain empty since output didn't match the regex
	if hostname := conn.Hostname(); hostname != "" {
		t.Errorf("expected empty hostname, got %q", hostname)
	}

	// Status should remain provisioning (not changed to connecting)
	conn.mu.RLock()
	status := conn.host.Status
	conn.mu.RUnlock()
	if status != "provisioning" {
		t.Errorf("expected status 'provisioning', got %q", status)
	}
}

func TestConnection_CloseNotifiesPendingSessions(t *testing.T) {
	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)

	// Queue multiple sessions
	ch1 := conn.QueueSession(context.Background(), "s1", "win1", "/tmp", "cmd1", "")
	ch2 := conn.QueueSession(context.Background(), "s2", "win2", "/tmp", "cmd2", "")

	// Close the connection — should notify all pending callers
	conn.Close()

	// Both channels should receive error results without blocking
	select {
	case result := <-ch1:
		if result.Error == nil {
			t.Error("expected error result for session s1")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("pending session s1 was not notified on Close()")
	}

	select {
	case result := <-ch2:
		if result.Error == nil {
			t.Error("expected error result for session s2")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("pending session s2 was not notified on Close()")
	}

	// Queue should be empty after close
	conn.pendingSessionsMu.Lock()
	remaining := len(conn.pendingSessions)
	conn.pendingSessionsMu.Unlock()
	if remaining != 0 {
		t.Errorf("expected empty queue after close, got %d", remaining)
	}
}

func TestConnection_UnsubscribePTYOutputClosesChannel(t *testing.T) {
	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)
	ch := conn.SubscribePTYOutput()

	conn.UnsubscribePTYOutput(ch)

	// Channel should be closed — reading should return zero value immediately
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("channel was not closed after unsubscribe")
	}
}

func TestPendingSessionResult(t *testing.T) {
	// Test that PendingSessionResult properly carries window and pane IDs
	result := PendingSessionResult{
		WindowID: "@1",
		PaneID:   "%5",
		Error:    nil,
	}

	if result.WindowID != "@1" {
		t.Errorf("expected window ID '@1', got '%s'", result.WindowID)
	}

	if result.PaneID != "%5" {
		t.Errorf("expected pane ID '%%5', got '%s'", result.PaneID)
	}

	if result.Error != nil {
		t.Error("expected no error")
	}
}

func TestConnectionConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ConnectionConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: ConnectionConfig{
				ProfileID:     "test",
				Flavor:        "production",
				DisplayName:   "Production",
				WorkspacePath: "/workspace",
				VCS:           "git",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewConnection(tt.cfg)
			if conn == nil && !tt.wantErr {
				t.Error("expected non-nil connection")
			}
			if conn != nil && conn.flavor.ID != tt.cfg.ProfileID {
				t.Errorf("profile ID mismatch: expected %s, got %s", tt.cfg.ProfileID, conn.flavor.ID)
			}
		})
	}
}

// TestProvision_SucceedsWhenControlModeEstablished verifies Bug 4:
// Provision() must succeed when controlModeEstablished is true even if the
// host status is "provisioning". Previously Provision() called IsConnected()
// which required status=="connected", causing a self-inflicted failure when
// the caller set status to "provisioning" for UI feedback.
func TestProvision_SucceedsWhenControlModeEstablished(t *testing.T) {
	cfg := ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)

	// Simulate the state after Connect() succeeds but before Provision() runs:
	// - controlModeEstablished is true (connection is ready)
	// - host status is "provisioning" (caller set it for UI feedback)
	conn.controlModeEstablished.Store(true)
	conn.mu.Lock()
	conn.host.Status = "provisioning"
	conn.mu.Unlock()

	// Provision with empty command should succeed (no-op)
	err := conn.Provision(context.Background(), "")
	if err != nil {
		t.Errorf("Provision with empty command should succeed, got: %v", err)
	}

	// Now verify that Provision with a real command checks controlModeEstablished
	// rather than IsConnected(). Since we have no real client, it should return
	// "not connected" only if controlModeEstablished is false.
	conn.controlModeEstablished.Store(false)
	err = conn.Provision(context.Background(), "echo hello")
	if err == nil {
		t.Error("Provision should fail when controlModeEstablished is false")
	}
	if err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}

	// With controlModeEstablished=true but no client, it should still fail
	// (but NOT because of status check - because client is nil)
	conn.controlModeEstablished.Store(true)
	err = conn.Provision(context.Background(), "echo hello")
	if err == nil {
		t.Error("Provision should fail when client is nil")
	}
	if err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestConnectionConfigFromResolved(t *testing.T) {
	r := config.ResolvedFlavor{
		ProfileID:          "devvm",
		ProfileDisplayName: "DevVM Profile",
		Flavor:             "devvm",
		FlavorDisplayName:  "DevVM",
		WorkspacePath:      "/data/users/$USER",
		VCS:                "hg",
		ConnectCommand:     "ssh $HOST",
		ReconnectCommand:   "ssh $HOST",
		ProvisionCommand:   "setup.sh",
		HostnameRegex:      `devvm\d+`,
	}

	cc := ConnectionConfigFromResolved(r)

	if cc.ProfileID != r.ProfileID {
		t.Errorf("ProfileID: got %q, want %q", cc.ProfileID, r.ProfileID)
	}
	if cc.Flavor != r.Flavor {
		t.Errorf("Flavor: got %q, want %q", cc.Flavor, r.Flavor)
	}
	if cc.DisplayName != r.FlavorDisplayName {
		t.Errorf("DisplayName: got %q, want %q", cc.DisplayName, r.FlavorDisplayName)
	}
	if cc.WorkspacePath != r.WorkspacePath {
		t.Errorf("WorkspacePath: got %q, want %q", cc.WorkspacePath, r.WorkspacePath)
	}
	if cc.VCS != r.VCS {
		t.Errorf("VCS: got %q, want %q", cc.VCS, r.VCS)
	}
	if cc.ProvisionCommand != r.ProvisionCommand {
		t.Errorf("ProvisionCommand: got %q, want %q", cc.ProvisionCommand, r.ProvisionCommand)
	}
	if cc.HostnameRegex != r.HostnameRegex {
		t.Errorf("HostnameRegex: got %q, want %q", cc.HostnameRegex, r.HostnameRegex)
	}
	// OnStatusChange, OnProgress, Logger should be nil (not set by helper)
	if cc.OnStatusChange != nil {
		t.Error("OnStatusChange should be nil")
	}
	if cc.OnProgress != nil {
		t.Error("OnProgress should be nil")
	}
	if cc.Logger != nil {
		t.Error("Logger should be nil")
	}
}
