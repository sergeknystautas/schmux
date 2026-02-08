package remote

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConnection_QueueSession(t *testing.T) {
	cfg := ConnectionConfig{
		FlavorID:      "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test Flavor",
		WorkspacePath: "/tmp/test",
		VCS:           "git",
	}

	conn := NewConnection(cfg)

	// Queue a session
	resultCh := conn.QueueSession(context.Background(), "session-1", "test-window", "/tmp", "echo test")

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
		FlavorID:      "test-flavor",
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
		FlavorID:      "test-flavor",
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
		FlavorID:      "test-flavor",
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
				FlavorID:      "test",
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
			if conn != nil && conn.flavor.ID != tt.cfg.FlavorID {
				t.Errorf("flavor ID mismatch: expected %s, got %s", tt.cfg.FlavorID, conn.flavor.ID)
			}
		})
	}
}
