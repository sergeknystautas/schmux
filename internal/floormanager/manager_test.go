package floormanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

func makeConfig(t *testing.T, jsonStr string) *config.Config {
	t.Helper()
	var cfg config.Config
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	return &cfg
}

func TestManagerStartSkipsWhenDisabled(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	err := m.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	// Stop should not hang when disabled
	m.Stop()
}

func TestManagerStartSkipsWhenDisabledExplicitly(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100},
		"floor_manager": {"enabled": false, "target": "claude"}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	err := m.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	m.Stop()
}

func TestManagerGetSessionIDEmpty(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	if id := m.GetSessionID(); id != "" {
		t.Errorf("GetSessionID() = %q, want empty string", id)
	}
}

func TestManagerGetSessionIDWithFloorManager(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	// Add a floor manager session to state
	st.AddSession(state.Session{
		ID:             "fm-001",
		WorkspaceID:    "",
		Target:         "claude",
		Nickname:       "floor-manager",
		TmuxSession:    "floor-manager",
		IsFloorManager: true,
	})

	m := New(cfg, st, nil, t.TempDir())

	if id := m.GetSessionID(); id != "fm-001" {
		t.Errorf("GetSessionID() = %q, want %q", id, "fm-001")
	}
}

func TestManagerInjectionCount(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	// Initial count should be 0
	if count := m.GetInjectionCount(); count != 0 {
		t.Errorf("GetInjectionCount() = %d, want 0", count)
	}

	// Increment and verify
	if count := m.IncrementInjectionCount(1); count != 1 {
		t.Errorf("IncrementInjectionCount(1) = %d, want 1", count)
	}
	if count := m.IncrementInjectionCount(1); count != 2 {
		t.Errorf("IncrementInjectionCount(1) = %d, want 2", count)
	}
	if count := m.GetInjectionCount(); count != 2 {
		t.Errorf("GetInjectionCount() = %d, want 2", count)
	}

	// Increment by more than 1 (batched signals)
	if count := m.IncrementInjectionCount(3); count != 5 {
		t.Errorf("IncrementInjectionCount(3) = %d, want 5", count)
	}
}

func TestManagerStopIdempotent(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	// Start with disabled config (closes doneCh immediately)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	// Stop should be idempotent — calling twice should not panic
	m.Stop()
	m.Stop()
}

func TestManagerGetSessionInfoEmpty(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	info := m.GetSessionInfo()
	if info != nil {
		t.Errorf("GetSessionInfo() = %+v, want nil", info)
	}
}

func TestManagerGetSessionInfoWithFloorManager(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")
	st.AddSession(state.Session{
		ID:             "fm-001",
		Target:         "claude",
		Nickname:       "floor-manager",
		TmuxSession:    "schmux-fm",
		IsFloorManager: true,
	})

	m := New(cfg, st, nil, t.TempDir())

	info := m.GetSessionInfo()
	if info == nil {
		t.Fatal("GetSessionInfo() returned nil")
	}
	if info.SessionID != "fm-001" {
		t.Errorf("SessionID = %q, want %q", info.SessionID, "fm-001")
	}
	if info.TmuxSession != "schmux-fm" {
		t.Errorf("TmuxSession = %q, want %q", info.TmuxSession, "schmux-fm")
	}
}

func TestManagerGetRotationThresholdDefault(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	threshold := m.GetRotationThreshold()
	if threshold != DefaultRotationThreshold {
		t.Errorf("GetRotationThreshold() = %d, want %d (default)", threshold, DefaultRotationThreshold)
	}
}

func TestManagerGetRotationThresholdCustom(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100},
		"floor_manager": {"enabled": true, "target": "claude", "rotation_threshold": 50}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	threshold := m.GetRotationThreshold()
	if threshold != 50 {
		t.Errorf("GetRotationThreshold() = %d, want 50", threshold)
	}
}

func TestManagerHandleRotationGuardsConcurrent(t *testing.T) {
	cfg := makeConfig(t, `{
		"workspace_path": "/tmp/test",
		"terminal": {"width": 120, "height": 40, "seed_lines": 100}
	}`)
	st := state.New("")

	m := New(cfg, st, nil, t.TempDir())

	// Simulate already rotating
	m.mu.Lock()
	m.rotating = true
	m.mu.Unlock()

	// HandleRotation should return immediately without panicking
	m.HandleRotation(context.Background(), false)

	// Verify still rotating (wasn't reset by the skipped call)
	m.mu.Lock()
	if !m.rotating {
		t.Error("rotating flag should still be true after skipped rotation")
	}
	m.mu.Unlock()
}

func TestInjectorStop(t *testing.T) {
	inj := NewInjector(context.Background(), nil, 0)

	// Stop should not panic
	inj.Stop()

	// Stop should be idempotent
	inj.Stop()
}
