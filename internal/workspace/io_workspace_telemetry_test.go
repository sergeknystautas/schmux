package workspace

import (
	"testing"
	"time"
)

func TestIOWorkspaceTelemetry_NilSafe(t *testing.T) {
	var tel *IOWorkspaceTelemetry
	// All methods must be safe on nil receiver
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, 100*time.Millisecond, 0, 500, 0)
	tel.Reset()
	snap := tel.Snapshot(false)
	if snap.TotalCommands != 0 {
		t.Fatalf("expected 0 total commands on nil, got %d", snap.TotalCommands)
	}
}

func TestIOWorkspaceTelemetry_RecordAndSnapshot(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"status", "--porcelain"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 50*time.Millisecond, 0, 200, 0)
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 200*time.Millisecond, 0, 1000, 50)
	tel.RecordCommand("git", []string{"status", "--porcelain"}, "ws-2", "/tmp/ws2", RefreshTriggerWatcher, 30*time.Millisecond, 0, 100, 0)

	snap := tel.Snapshot(false)

	if snap.TotalCommands != 3 {
		t.Fatalf("expected 3 total commands, got %d", snap.TotalCommands)
	}
	if len(snap.Counters) == 0 {
		t.Fatal("expected non-empty counters")
	}
	if snap.Counters["git_status"] != 2 {
		t.Fatalf("expected 2 git_status, got %d", snap.Counters["git_status"])
	}
	if snap.Counters["git_fetch"] != 1 {
		t.Fatalf("expected 1 git_fetch, got %d", snap.Counters["git_fetch"])
	}
	if snap.TriggerCounts["poller"] != 2 {
		t.Fatalf("expected 2 poller triggers, got %d", snap.TriggerCounts["poller"])
	}
	if snap.TriggerCounts["watcher"] != 1 {
		t.Fatalf("expected 1 watcher trigger, got %d", snap.TriggerCounts["watcher"])
	}
	if len(snap.SpanDurations) == 0 {
		t.Fatal("expected non-empty span durations")
	}
	// Verify span duration stats for git_status
	statusStats, ok := snap.SpanDurations["git_status"]
	if !ok {
		t.Fatal("expected span durations for git_status")
	}
	if statusStats.Count != 2 {
		t.Fatalf("expected git_status count 2, got %d", statusStats.Count)
	}
	// Total should be 50+30 = 80ms
	if statusStats.TotalMS < 79 || statusStats.TotalMS > 81 {
		t.Fatalf("expected git_status total ~80ms, got %.2f", statusStats.TotalMS)
	}
	// Max should be 50ms
	if statusStats.MaxMS < 49 || statusStats.MaxMS > 51 {
		t.Fatalf("expected git_status max ~50ms, got %.2f", statusStats.MaxMS)
	}
	// Avg should be 40ms
	if statusStats.AvgMS < 39 || statusStats.AvgMS > 41 {
		t.Fatalf("expected git_status avg ~40ms, got %.2f", statusStats.AvgMS)
	}

	// Verify total duration is approximately 280ms (50+200+30)
	if snap.TotalDurationMS < 279 || snap.TotalDurationMS > 281 {
		t.Fatalf("expected total duration ~280ms, got %.2f", snap.TotalDurationMS)
	}
}

func TestIOWorkspaceTelemetry_SlowRing(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	// Record a command above the slow threshold (100ms)
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp", RefreshTriggerPoller, 150*time.Millisecond, 0, 500, 0)
	// Record one below
	tel.RecordCommand("git", []string{"show-ref"}, "ws-1", "/tmp", RefreshTriggerPoller, 5*time.Millisecond, 0, 50, 0)

	snap := tel.Snapshot(false)
	if len(snap.SlowCommands) != 1 {
		t.Fatalf("expected 1 slow command, got %d", len(snap.SlowCommands))
	}
	if snap.SlowCommands[0].Command != "git fetch" {
		t.Fatalf("expected 'git fetch', got %q", snap.SlowCommands[0].Command)
	}
	if snap.SlowCommands[0].DurationMS < 149 || snap.SlowCommands[0].DurationMS > 151 {
		t.Fatalf("expected duration ~150ms, got %.2f", snap.SlowCommands[0].DurationMS)
	}
}

func TestIOWorkspaceTelemetry_FullRing(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	// Record a command that goes into the full ring
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, 10*time.Millisecond, 0, 100, 0)

	snap := tel.Snapshot(false)
	if len(snap.AllCommands) == 0 {
		t.Fatal("expected non-empty all commands ring")
	}
	if snap.AllCommands[0].Command != "git status" {
		t.Fatalf("expected 'git status', got %q", snap.AllCommands[0].Command)
	}
	if snap.RingCapacity != 512 {
		t.Fatalf("expected ring capacity 512, got %d", snap.RingCapacity)
	}
	if snap.SlowRingCapacity != 128 {
		t.Fatalf("expected slow ring capacity 128, got %d", snap.SlowRingCapacity)
	}
}

func TestIOWorkspaceTelemetry_FullRingOverflow(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	// Fill beyond capacity — oldest entries should be dropped
	for i := 0; i < 600; i++ {
		tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, time.Millisecond, 0, 10, 0)
	}

	snap := tel.Snapshot(false)
	if len(snap.AllCommands) != 512 {
		t.Fatalf("expected 512 entries in full ring, got %d", len(snap.AllCommands))
	}
	if snap.TotalCommands != 600 {
		t.Fatalf("expected 600 total commands, got %d", snap.TotalCommands)
	}
}

func TestIOWorkspaceTelemetry_SnapshotReset(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, 50*time.Millisecond, 0, 200, 0)

	snap := tel.Snapshot(true) // reset=true
	if snap.TotalCommands != 1 {
		t.Fatalf("expected 1, got %d", snap.TotalCommands)
	}

	snap2 := tel.Snapshot(false)
	if snap2.TotalCommands != 0 {
		t.Fatalf("expected 0 after reset, got %d", snap2.TotalCommands)
	}
	if len(snap2.Counters) != 0 {
		t.Fatalf("expected empty counters after reset, got %d", len(snap2.Counters))
	}
	if len(snap2.AllCommands) != 0 {
		t.Fatalf("expected empty all commands after reset, got %d", len(snap2.AllCommands))
	}
	if len(snap2.SlowCommands) != 0 {
		t.Fatalf("expected empty slow commands after reset, got %d", len(snap2.SlowCommands))
	}
}

func TestIOWorkspaceTelemetry_ByWorkspace(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 100*time.Millisecond, 0, 500, 0)
	tel.RecordCommand("git", []string{"fetch"}, "ws-2", "/tmp/ws2", RefreshTriggerPoller, 200*time.Millisecond, 0, 500, 0)

	snap := tel.Snapshot(false)
	if len(snap.ByWorkspaceSpans) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(snap.ByWorkspaceSpans))
	}
	ws1Spans, ok := snap.ByWorkspaceSpans["ws-1"]
	if !ok {
		t.Fatal("expected ws-1 in by-workspace spans")
	}
	fetchStats, ok := ws1Spans["git_fetch"]
	if !ok {
		t.Fatal("expected git_fetch stats for ws-1")
	}
	if fetchStats.Count != 1 {
		t.Fatalf("expected 1 fetch for ws-1, got %d", fetchStats.Count)
	}
	if fetchStats.TotalMS < 99 || fetchStats.TotalMS > 101 {
		t.Fatalf("expected ws-1 fetch total ~100ms, got %.2f", fetchStats.TotalMS)
	}

	ws2Spans, ok := snap.ByWorkspaceSpans["ws-2"]
	if !ok {
		t.Fatal("expected ws-2 in by-workspace spans")
	}
	fetchStats2, ok := ws2Spans["git_fetch"]
	if !ok {
		t.Fatal("expected git_fetch stats for ws-2")
	}
	if fetchStats2.TotalMS < 199 || fetchStats2.TotalMS > 201 {
		t.Fatalf("expected ws-2 fetch total ~200ms, got %.2f", fetchStats2.TotalMS)
	}
}

func TestIOWorkspaceTelemetry_ByTrigger(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, 50*time.Millisecond, 0, 100, 0)
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerWatcher, 30*time.Millisecond, 0, 100, 0)
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp", RefreshTriggerExplicit, 100*time.Millisecond, 0, 500, 0)

	snap := tel.Snapshot(false)
	if len(snap.ByTriggerSpans) != 3 {
		t.Fatalf("expected 3 triggers, got %d", len(snap.ByTriggerSpans))
	}
	pollerSpans, ok := snap.ByTriggerSpans["poller"]
	if !ok {
		t.Fatal("expected poller in by-trigger spans")
	}
	if _, ok := pollerSpans["git_status"]; !ok {
		t.Fatal("expected git_status in poller spans")
	}
}

func TestIOWorkspaceTelemetry_CommandTypeExtraction(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()

	// Test various git subcommands
	tel.RecordCommand("git", []string{"status", "--porcelain"}, "ws-1", "/tmp", RefreshTriggerPoller, time.Millisecond, 0, 0, 0)
	tel.RecordCommand("git", []string{"merge-base", "--is-ancestor", "HEAD", "origin/main"}, "ws-1", "/tmp", RefreshTriggerPoller, time.Millisecond, 0, 0, 0)
	tel.RecordCommand("git", []string{"rev-list", "--left-right", "--count", "HEAD...origin/main"}, "ws-1", "/tmp", RefreshTriggerPoller, time.Millisecond, 0, 0, 0)
	tel.RecordCommand("git", []string{"show-ref", "--verify", "--quiet", "refs/heads/main"}, "ws-1", "/tmp", RefreshTriggerPoller, time.Millisecond, 0, 0, 0)
	tel.RecordCommand("git", nil, "ws-1", "/tmp", RefreshTriggerPoller, time.Millisecond, 0, 0, 0)

	snap := tel.Snapshot(false)

	expected := map[string]int64{
		"git_status":     1,
		"git_merge-base": 1,
		"git_rev-list":   1,
		"git_show-ref":   1,
		"git":            1, // no args -> just "git"
	}
	for key, want := range expected {
		if snap.Counters[key] != want {
			t.Errorf("expected counter %s=%d, got %d", key, want, snap.Counters[key])
		}
	}
}

func TestIOWorkspaceTelemetry_CommandEntryFields(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"fetch", "--all"}, "ws-42", "/workspace/dir", RefreshTriggerExplicit, 250*time.Millisecond, 128, 4096, 256)

	snap := tel.Snapshot(false)
	if len(snap.AllCommands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(snap.AllCommands))
	}
	entry := snap.AllCommands[0]
	if entry.Command != "git fetch --all" {
		t.Fatalf("expected 'git fetch --all', got %q", entry.Command)
	}
	if entry.WorkspaceID != "ws-42" {
		t.Fatalf("expected workspace 'ws-42', got %q", entry.WorkspaceID)
	}
	if entry.WorkingDir != "/workspace/dir" {
		t.Fatalf("expected working dir '/workspace/dir', got %q", entry.WorkingDir)
	}
	if entry.Trigger != "explicit" {
		t.Fatalf("expected trigger 'explicit', got %q", entry.Trigger)
	}
	if entry.ExitCode != 128 {
		t.Fatalf("expected exit code 128, got %d", entry.ExitCode)
	}
	if entry.StdoutBytes != 4096 {
		t.Fatalf("expected stdout bytes 4096, got %d", entry.StdoutBytes)
	}
	if entry.StderrBytes != 256 {
		t.Fatalf("expected stderr bytes 256, got %d", entry.StderrBytes)
	}
	if entry.Timestamp == "" {
		t.Fatal("expected non-empty timestamp")
	}
}
