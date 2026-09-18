package chat

import "testing"

// Quota notifications must not disturb the headless nudge tracker.
func TestCodexPlanUsageLeavesNudgeIdle(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(NewHarness([]byte(`{"method":"account/rateLimits/updated","params":{"rateLimits":{"primary":{"usedPercent":31,"windowDurationMins":10080}}}}`)))
	if got := tr.Result().State; got != "Idle" {
		t.Fatalf("nudge state = %q, want Idle", got)
	}
}
