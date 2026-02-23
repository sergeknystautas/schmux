//go:build integration

package floormanager

import (
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestFloorManagerConfigAndPureFunctions(t *testing.T) {
	// 1. Create config with floor_manager enabled
	cfg := &config.Config{}
	cfg.FloorManager = &config.FloorManagerConfig{
		Enabled:           true,
		Target:            "test-target",
		RotationThreshold: 100,
	}

	if !cfg.GetFloorManagerEnabled() {
		t.Fatal("expected floor manager to be enabled")
	}
	if cfg.GetFloorManagerTarget() != "test-target" {
		t.Errorf("expected target=test-target, got %s", cfg.GetFloorManagerTarget())
	}
	if cfg.GetFloorManagerRotationThreshold() != 100 {
		t.Errorf("expected rotation_threshold=100, got %d", cfg.GetFloorManagerRotationThreshold())
	}

	// 2. Create state and add sessions
	st := state.New("", nil)
	st.AddSession(state.Session{
		ID:             "fm-1",
		Target:         "test-target",
		TmuxSession:    "schmux-fm",
		IsFloorManager: true,
	})
	st.AddSession(state.Session{
		ID:          "worker-1",
		Target:      "claude",
		TmuxSession: "schmux-worker",
		Nickname:    "auth-agent",
	})

	fmSess, found := st.GetFloorManagerSession()
	if !found {
		t.Fatal("expected floor manager session")
	}
	if fmSess.ID != "fm-1" {
		t.Errorf("expected fm-1, got %s", fmSess.ID)
	}

	// 3. Signal injection filtering
	if ShouldInjectSignal("", "working") {
		t.Error("working signals should not be injected")
	}
	if !ShouldInjectSignal("working", "error") {
		t.Error("error signals should be injected")
	}
	if !ShouldInjectSignal("working", "completed") {
		t.Error("completed signals should be injected")
	}

	// 4. Signal message formatting
	sig := signal.Signal{
		State:    "needs_input",
		Message:  "Need clarification on auth flow",
		Intent:   "Implementing JWT auth",
		Blockers: "Token format unclear",
	}
	msg := FormatSignalMessage("auth-001", "auth-agent", "working", sig)
	if !strings.Contains(msg, "[SIGNAL]") {
		t.Error("expected [SIGNAL] prefix")
	}
	if !strings.Contains(msg, "auth-agent") {
		t.Error("expected session name in message")
	}
	if !strings.Contains(msg, "needs_input") {
		t.Error("expected new state in message")
	}

	// 5. Prompt generation — static instructions only
	instructions := GenerateInstructions()
	if !strings.Contains(instructions, "floor manager") {
		t.Error("expected role definition in instructions")
	}
	if !strings.Contains(instructions, "Read `memory.md`") {
		t.Error("expected instruction to read memory.md on startup")
	}
	if !strings.Contains(instructions, "Run `schmux status`") {
		t.Error("expected instruction to run schmux status on startup")
	}
}
