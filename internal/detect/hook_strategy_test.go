package detect

import "testing"

func TestGetHookStrategy_None(t *testing.T) {
	s, err := GetHookStrategy("none")
	if err != nil {
		t.Fatalf("GetHookStrategy(none): %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil strategy")
	}
	if s.SupportsHooks() {
		t.Error("none strategy should not support hooks")
	}
}

func TestGetHookStrategy_Unknown(t *testing.T) {
	_, err := GetHookStrategy("telekinesis")
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestGetHookStrategy_Empty(t *testing.T) {
	s, err := GetHookStrategy("")
	if err != nil {
		t.Fatalf("GetHookStrategy empty: %v", err)
	}
	if s.SupportsHooks() {
		t.Error("empty strategy should default to none")
	}
}

func TestNoneStrategy_SetupHooks(t *testing.T) {
	s, _ := GetHookStrategy("none")
	err := s.SetupHooks(HookContext{WorkspacePath: "/tmp/test"})
	if err != nil {
		t.Errorf("SetupHooks: %v", err)
	}
}

func TestNoneStrategy_CleanupHooks(t *testing.T) {
	s, _ := GetHookStrategy("none")
	err := s.CleanupHooks("/tmp/test")
	if err != nil {
		t.Errorf("CleanupHooks: %v", err)
	}
}

func TestNoneStrategy_WrapRemoteCommand(t *testing.T) {
	s, _ := GetHookStrategy("none")
	cmd, err := s.WrapRemoteCommand("echo hello")
	if err != nil {
		t.Errorf("WrapRemoteCommand: %v", err)
	}
	if cmd != "echo hello" {
		t.Errorf("WrapRemoteCommand = %q, want passthrough", cmd)
	}
}
