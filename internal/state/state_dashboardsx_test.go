//go:build !nodashboardsx

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardSXStatus(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	os.WriteFile(statePath, []byte(`{"workspaces":[],"sessions":[]}`), 0644)
	st, err := Load(statePath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Initially nil
	if got := st.GetDashboardSXStatus(); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}

	// Set and read back
	now := time.Now().Truncate(time.Second)
	status := &DashboardSXStatus{
		LastHeartbeatTime:   now,
		LastHeartbeatStatus: 403,
		LastHeartbeatError:  "registration not found",
		CertDomain:          "12540.dashboard.sx",
		CertExpiresAt:       now.Add(10 * 24 * time.Hour),
	}
	st.SetDashboardSXStatus(status)

	got := st.GetDashboardSXStatus()
	if got == nil {
		t.Fatal("expected non-nil status")
	}
	if got.LastHeartbeatStatus != 403 {
		t.Errorf("expected status 403, got %d", got.LastHeartbeatStatus)
	}
	if got.LastHeartbeatError != "registration not found" {
		t.Errorf("expected error message, got %q", got.LastHeartbeatError)
	}
	if got.CertDomain != "12540.dashboard.sx" {
		t.Errorf("expected domain, got %q", got.CertDomain)
	}

	// Verify persistence
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st2, err := Load(statePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	got2 := st2.GetDashboardSXStatus()
	if got2 == nil {
		t.Fatal("expected non-nil after reload")
	}
	if got2.LastHeartbeatStatus != 403 {
		t.Errorf("expected 403 after reload, got %d", got2.LastHeartbeatStatus)
	}
}
