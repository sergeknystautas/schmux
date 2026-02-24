package workspace

import (
	"strings"
	"sync"
	"time"
)

// RefreshTrigger identifies what triggered a git command execution.
type RefreshTrigger string

const (
	RefreshTriggerPoller   RefreshTrigger = "poller"
	RefreshTriggerWatcher  RefreshTrigger = "watcher"
	RefreshTriggerExplicit RefreshTrigger = "explicit"
)

// Ring buffer capacities and thresholds.
const (
	ioSlowRingCapacity = 128
	ioFullRingCapacity = 512
	ioSlowThresholdMS  = 100.0 // commands >= 100ms are considered slow
)

// WorkspaceRefreshDurationStats holds aggregate duration statistics for a set of spans.
type WorkspaceRefreshDurationStats struct {
	Count   int64   `json:"count"`
	TotalMS float64 `json:"total_ms"`
	MaxMS   float64 `json:"max_ms"`
	AvgMS   float64 `json:"avg_ms"`
}

// refreshDurationAgg accumulates duration statistics. Not safe for concurrent use;
// the caller (IOWorkspaceTelemetry) must hold the mutex.
type refreshDurationAgg struct {
	count   int64
	totalMS float64
	maxMS   float64
}

func (a *refreshDurationAgg) record(durationMS float64) {
	a.count++
	a.totalMS += durationMS
	if durationMS > a.maxMS {
		a.maxMS = durationMS
	}
}

func (a *refreshDurationAgg) snapshot() WorkspaceRefreshDurationStats {
	s := WorkspaceRefreshDurationStats{
		Count:   a.count,
		TotalMS: a.totalMS,
		MaxMS:   a.maxMS,
	}
	if a.count > 0 {
		s.AvgMS = a.totalMS / float64(a.count)
	}
	return s
}

// IOWorkspaceCommandEntry represents a single recorded git command.
type IOWorkspaceCommandEntry struct {
	Timestamp   string  `json:"ts"`
	Command     string  `json:"command"`
	WorkspaceID string  `json:"workspace_id"`
	WorkingDir  string  `json:"working_dir"`
	Trigger     string  `json:"trigger"`
	DurationMS  float64 `json:"duration_ms"`
	ExitCode    int     `json:"exit_code"`
	StdoutBytes int64   `json:"stdout_bytes"`
	StderrBytes int64   `json:"stderr_bytes"`
}

// refreshSlowSpanRing is a fixed-size circular buffer for command entries.
// Not safe for concurrent use; the caller must hold the mutex.
type refreshSlowSpanRing struct {
	entries []IOWorkspaceCommandEntry
	cursor  int
	full    bool
	cap     int
}

func newRefreshSlowSpanRing(capacity int) *refreshSlowSpanRing {
	return &refreshSlowSpanRing{
		entries: make([]IOWorkspaceCommandEntry, capacity),
		cap:     capacity,
	}
}

func (r *refreshSlowSpanRing) add(entry IOWorkspaceCommandEntry) {
	r.entries[r.cursor] = entry
	r.cursor = (r.cursor + 1) % r.cap
	if r.cursor == 0 {
		r.full = true
	}
}

// snapshot returns all entries in chronological order (oldest first).
func (r *refreshSlowSpanRing) snapshot() []IOWorkspaceCommandEntry {
	if !r.full {
		result := make([]IOWorkspaceCommandEntry, r.cursor)
		copy(result, r.entries[:r.cursor])
		return result
	}
	result := make([]IOWorkspaceCommandEntry, r.cap)
	n := copy(result, r.entries[r.cursor:])
	copy(result[n:], r.entries[:r.cursor])
	return result
}

func (r *refreshSlowSpanRing) reset() {
	r.cursor = 0
	r.full = false
	// Zero out entries so stale data isn't visible
	for i := range r.entries {
		r.entries[i] = IOWorkspaceCommandEntry{}
	}
}

// IOWorkspaceTelemetrySnapshot holds a point-in-time snapshot of all telemetry data.
type IOWorkspaceTelemetrySnapshot struct {
	SnapshotAt       string                                              `json:"snapshot_at"`
	TotalCommands    int64                                               `json:"total_commands"`
	TotalDurationMS  float64                                             `json:"total_duration_ms"`
	Counters         map[string]int64                                    `json:"counters"`
	TriggerCounts    map[string]int64                                    `json:"trigger_counts"`
	SpanDurations    map[string]WorkspaceRefreshDurationStats            `json:"span_durations"`
	ByTriggerSpans   map[string]map[string]WorkspaceRefreshDurationStats `json:"by_trigger_spans"`
	ByWorkspaceSpans map[string]map[string]WorkspaceRefreshDurationStats `json:"by_workspace_spans"`
	SlowCommands     []IOWorkspaceCommandEntry                           `json:"slow_commands"`
	AllCommands      []IOWorkspaceCommandEntry                           `json:"all_commands"`
	RingCapacity     int                                                 `json:"ring_capacity"`
	SlowRingCapacity int                                                 `json:"slow_ring_capacity"`
	SlowThresholdMS  float64                                             `json:"slow_threshold_ms"`
}

// IOWorkspaceTelemetry is a mutex-protected in-memory collector for git command
// execution telemetry. All methods are nil-safe (no-op on nil receiver).
type IOWorkspaceTelemetry struct {
	mu               sync.Mutex
	totalCommands    int64
	totalDurationMS  float64
	counters         map[string]int64                          // command type -> count
	triggerCounts    map[string]int64                          // trigger name -> count
	spanDurations    map[string]*refreshDurationAgg            // command type -> aggregate
	byTriggerSpans   map[string]map[string]*refreshDurationAgg // trigger -> command type -> aggregate
	byWorkspaceSpans map[string]map[string]*refreshDurationAgg // workspace ID -> command type -> aggregate
	slowRing         *refreshSlowSpanRing
	fullRing         *refreshSlowSpanRing
}

// NewIOWorkspaceTelemetry creates a new telemetry collector.
func NewIOWorkspaceTelemetry() *IOWorkspaceTelemetry {
	return &IOWorkspaceTelemetry{
		counters:         make(map[string]int64),
		triggerCounts:    make(map[string]int64),
		spanDurations:    make(map[string]*refreshDurationAgg),
		byTriggerSpans:   make(map[string]map[string]*refreshDurationAgg),
		byWorkspaceSpans: make(map[string]map[string]*refreshDurationAgg),
		slowRing:         newRefreshSlowSpanRing(ioSlowRingCapacity),
		fullRing:         newRefreshSlowSpanRing(ioFullRingCapacity),
	}
}

// extractCommandType derives the command type key from args.
// For example, ["status", "--porcelain"] -> "git_status", ["fetch"] -> "git_fetch".
// Empty args returns "git".
func extractCommandType(args []string) string {
	if len(args) == 0 {
		return "git"
	}
	return "git_" + args[0]
}

// RecordCommand records a single git command execution.
// Safe to call on nil receiver.
func (t *IOWorkspaceTelemetry) RecordCommand(bin string, args []string, workspaceID, workingDir string, trigger RefreshTrigger, duration time.Duration, exitCode int, stdoutBytes, stderrBytes int64) {
	if t == nil {
		return
	}

	durationMS := float64(duration) / float64(time.Millisecond)
	cmdType := extractCommandType(args)
	triggerStr := string(trigger)

	// Build the full command string
	var cmdParts []string
	cmdParts = append(cmdParts, bin)
	cmdParts = append(cmdParts, args...)
	fullCommand := strings.Join(cmdParts, " ")

	entry := IOWorkspaceCommandEntry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		Command:     fullCommand,
		WorkspaceID: workspaceID,
		WorkingDir:  workingDir,
		Trigger:     triggerStr,
		DurationMS:  durationMS,
		ExitCode:    exitCode,
		StdoutBytes: stdoutBytes,
		StderrBytes: stderrBytes,
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Update totals
	t.totalCommands++
	t.totalDurationMS += durationMS

	// Update per-command-type counter
	t.counters[cmdType]++

	// Update per-trigger counter
	t.triggerCounts[triggerStr]++

	// Update span duration aggregates
	agg, ok := t.spanDurations[cmdType]
	if !ok {
		agg = &refreshDurationAgg{}
		t.spanDurations[cmdType] = agg
	}
	agg.record(durationMS)

	// Update by-trigger span aggregates
	triggerSpans, ok := t.byTriggerSpans[triggerStr]
	if !ok {
		triggerSpans = make(map[string]*refreshDurationAgg)
		t.byTriggerSpans[triggerStr] = triggerSpans
	}
	triggerAgg, ok := triggerSpans[cmdType]
	if !ok {
		triggerAgg = &refreshDurationAgg{}
		triggerSpans[cmdType] = triggerAgg
	}
	triggerAgg.record(durationMS)

	// Update by-workspace span aggregates
	wsSpans, ok := t.byWorkspaceSpans[workspaceID]
	if !ok {
		wsSpans = make(map[string]*refreshDurationAgg)
		t.byWorkspaceSpans[workspaceID] = wsSpans
	}
	wsAgg, ok := wsSpans[cmdType]
	if !ok {
		wsAgg = &refreshDurationAgg{}
		wsSpans[cmdType] = wsAgg
	}
	wsAgg.record(durationMS)

	// Add to full ring (all commands)
	t.fullRing.add(entry)

	// Add to slow ring if above threshold
	if durationMS >= ioSlowThresholdMS {
		t.slowRing.add(entry)
	}
}

// Snapshot returns a point-in-time copy of all telemetry data.
// If reset is true, all data is cleared after taking the snapshot.
// Safe to call on nil receiver (returns zero-value snapshot).
func (t *IOWorkspaceTelemetry) Snapshot(reset bool) IOWorkspaceTelemetrySnapshot {
	if t == nil {
		return IOWorkspaceTelemetrySnapshot{}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	snap := IOWorkspaceTelemetrySnapshot{
		SnapshotAt:       time.Now().UTC().Format(time.RFC3339Nano),
		TotalCommands:    t.totalCommands,
		TotalDurationMS:  t.totalDurationMS,
		Counters:         copyMapInt64(t.counters),
		TriggerCounts:    copyMapInt64(t.triggerCounts),
		SpanDurations:    snapshotDurationAggs(t.spanDurations),
		ByTriggerSpans:   snapshotNestedDurationAggs(t.byTriggerSpans),
		ByWorkspaceSpans: snapshotNestedDurationAggs(t.byWorkspaceSpans),
		SlowCommands:     t.slowRing.snapshot(),
		AllCommands:      t.fullRing.snapshot(),
		RingCapacity:     ioFullRingCapacity,
		SlowRingCapacity: ioSlowRingCapacity,
		SlowThresholdMS:  ioSlowThresholdMS,
	}

	if reset {
		t.resetLocked()
	}

	return snap
}

// Reset clears all telemetry data.
// Safe to call on nil receiver.
func (t *IOWorkspaceTelemetry) Reset() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.resetLocked()
}

// resetLocked clears all data. Must be called with mu held.
func (t *IOWorkspaceTelemetry) resetLocked() {
	t.totalCommands = 0
	t.totalDurationMS = 0
	t.counters = make(map[string]int64)
	t.triggerCounts = make(map[string]int64)
	t.spanDurations = make(map[string]*refreshDurationAgg)
	t.byTriggerSpans = make(map[string]map[string]*refreshDurationAgg)
	t.byWorkspaceSpans = make(map[string]map[string]*refreshDurationAgg)
	t.slowRing.reset()
	t.fullRing.reset()
}

// copyMapInt64 returns a shallow copy of a map[string]int64.
func copyMapInt64(src map[string]int64) map[string]int64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// snapshotDurationAggs converts a map of aggregates to their snapshot form.
func snapshotDurationAggs(src map[string]*refreshDurationAgg) map[string]WorkspaceRefreshDurationStats {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]WorkspaceRefreshDurationStats, len(src))
	for k, v := range src {
		dst[k] = v.snapshot()
	}
	return dst
}

// snapshotNestedDurationAggs converts nested maps of aggregates to their snapshot form.
func snapshotNestedDurationAggs(src map[string]map[string]*refreshDurationAgg) map[string]map[string]WorkspaceRefreshDurationStats {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]map[string]WorkspaceRefreshDurationStats, len(src))
	for outerKey, innerMap := range src {
		inner := make(map[string]WorkspaceRefreshDurationStats, len(innerMap))
		for k, v := range innerMap {
			inner[k] = v.snapshot()
		}
		dst[outerKey] = inner
	}
	return dst
}
