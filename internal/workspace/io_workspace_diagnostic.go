package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// IOWorkspaceDiagnosticCapture holds all data for a single IO workspace diagnostic snapshot.
type IOWorkspaceDiagnosticCapture struct {
	Timestamp time.Time
	Snapshot  IOWorkspaceTelemetrySnapshot
	Findings  []string
	Verdict   string
}

type ioWorkspaceDiagnosticMeta struct {
	Timestamp        string                                              `json:"timestamp"`
	TotalCommands    int64                                               `json:"totalCommands"`
	TotalDurationMS  float64                                             `json:"totalDurationMs"`
	Counters         map[string]int64                                    `json:"counters"`
	TriggerCounts    map[string]int64                                    `json:"triggerCounts"`
	SpanDurations    map[string]WorkspaceRefreshDurationStats            `json:"spanDurations"`
	ByTriggerSpans   map[string]map[string]WorkspaceRefreshDurationStats `json:"byTriggerSpans"`
	ByWorkspaceSpans map[string]map[string]WorkspaceRefreshDurationStats `json:"byWorkspaceSpans"`
	Findings         []string                                            `json:"findings"`
	Verdict          string                                              `json:"verdict"`
}

// NewIOWorkspaceDiagnosticCapture creates a new diagnostic capture from a telemetry snapshot,
// computing automated findings and verdict.
func NewIOWorkspaceDiagnosticCapture(snap IOWorkspaceTelemetrySnapshot, ts time.Time) *IOWorkspaceDiagnosticCapture {
	findings, verdict := computeFindings(snap)
	return &IOWorkspaceDiagnosticCapture{
		Timestamp: ts,
		Snapshot:  snap,
		Findings:  findings,
		Verdict:   verdict,
	}
}

// WriteToDir writes all diagnostic files to the given directory.
func (d *IOWorkspaceDiagnosticCapture) WriteToDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// meta.json
	meta := ioWorkspaceDiagnosticMeta{
		Timestamp:        d.Timestamp.UTC().Format(time.RFC3339),
		TotalCommands:    d.Snapshot.TotalCommands,
		TotalDurationMS:  d.Snapshot.TotalDurationMS,
		Counters:         d.Snapshot.Counters,
		TriggerCounts:    d.Snapshot.TriggerCounts,
		SpanDurations:    d.Snapshot.SpanDurations,
		ByTriggerSpans:   d.Snapshot.ByTriggerSpans,
		ByWorkspaceSpans: d.Snapshot.ByWorkspaceSpans,
		Findings:         d.Findings,
		Verdict:          d.Verdict,
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaJSON, 0o644); err != nil {
		return err
	}

	// commands-ringbuffer.txt — human-readable dump of AllCommands ring
	if err := os.WriteFile(filepath.Join(dir, "commands-ringbuffer.txt"), []byte(formatCommandEntries(d.Snapshot.AllCommands)), 0o644); err != nil {
		return err
	}

	// slow-commands.txt — same format but from SlowCommands ring
	if err := os.WriteFile(filepath.Join(dir, "slow-commands.txt"), []byte(formatCommandEntries(d.Snapshot.SlowCommands)), 0o644); err != nil {
		return err
	}

	// by-workspace.txt — per-workspace summary
	if err := os.WriteFile(filepath.Join(dir, "by-workspace.txt"), []byte(formatByWorkspace(d.Snapshot)), 0o644); err != nil {
		return err
	}

	return nil
}

// formatCommandEntries formats command entries as human-readable lines:
// {ts} {duration_ms}ms {command} workspace={workspace_id}
func formatCommandEntries(entries []IOWorkspaceCommandEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "%s %.1fms %s workspace=%s\n", e.Timestamp, e.DurationMS, e.Command, e.WorkspaceID)
	}
	return sb.String()
}

// formatByWorkspace formats per-workspace summary: workspace ID, total commands,
// total time, top 5 slowest command types.
func formatByWorkspace(snap IOWorkspaceTelemetrySnapshot) string {
	var sb strings.Builder

	// Sort workspace IDs for deterministic output
	wsIDs := make([]string, 0, len(snap.ByWorkspaceSpans))
	for wsID := range snap.ByWorkspaceSpans {
		wsIDs = append(wsIDs, wsID)
	}
	sort.Strings(wsIDs)

	for _, wsID := range wsIDs {
		spans := snap.ByWorkspaceSpans[wsID]
		var totalCommands int64
		var totalDuration float64

		type cmdDuration struct {
			cmdType string
			totalMS float64
			count   int64
		}
		var cmds []cmdDuration

		for cmdType, stats := range spans {
			totalCommands += stats.Count
			totalDuration += stats.TotalMS
			cmds = append(cmds, cmdDuration{cmdType: cmdType, totalMS: stats.TotalMS, count: stats.Count})
		}

		// Sort by total duration descending
		sort.Slice(cmds, func(i, j int) bool {
			return cmds[i].totalMS > cmds[j].totalMS
		})

		fmt.Fprintf(&sb, "workspace=%s commands=%d total_duration=%.1fms\n", wsID, totalCommands, totalDuration)

		// Top 5 slowest command types
		limit := 5
		if len(cmds) < limit {
			limit = len(cmds)
		}
		for i := 0; i < limit; i++ {
			fmt.Fprintf(&sb, "  %s count=%d total=%.1fms\n", cmds[i].cmdType, cmds[i].count, cmds[i].totalMS)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// computeFindings computes automated first-pass analysis findings and a verdict.
func computeFindings(snap IOWorkspaceTelemetrySnapshot) ([]string, string) {
	var findings []string
	var dominantPattern string

	// Flag if any single command type exceeds 50% of total time
	if snap.TotalDurationMS > 0 {
		for cmdType, stats := range snap.SpanDurations {
			pct := (stats.TotalMS / snap.TotalDurationMS) * 100
			if pct > 50 {
				finding := fmt.Sprintf("Command type %q accounts for %.1f%% of total time (%.1fms / %.1fms)",
					cmdType, pct, stats.TotalMS, snap.TotalDurationMS)
				findings = append(findings, finding)
				dominantPattern = fmt.Sprintf("dominated by %s", cmdType)
			}
		}
	}

	// Flag if watcher-triggered commands overlap with poller-triggered commands
	watcherSpans := snap.ByTriggerSpans["watcher"]
	pollerSpans := snap.ByTriggerSpans["poller"]
	if watcherSpans != nil && pollerSpans != nil {
		var overlapping []string
		for cmdType := range watcherSpans {
			if _, ok := pollerSpans[cmdType]; ok {
				overlapping = append(overlapping, cmdType)
			}
		}
		if len(overlapping) > 0 {
			sort.Strings(overlapping)
			finding := fmt.Sprintf("Watcher and poller both execute the same command types: %s",
				strings.Join(overlapping, ", "))
			findings = append(findings, finding)
			if dominantPattern == "" {
				dominantPattern = "watcher/poller overlap"
			}
		}
	}

	// Flag if any workspace accounts for >50% of total time
	if snap.TotalDurationMS > 0 {
		for wsID, spans := range snap.ByWorkspaceSpans {
			var wsTotalMS float64
			for _, stats := range spans {
				wsTotalMS += stats.TotalMS
			}
			pct := (wsTotalMS / snap.TotalDurationMS) * 100
			if pct > 50 {
				finding := fmt.Sprintf("Workspace %q accounts for %.1f%% of total time (%.1fms / %.1fms)",
					wsID, pct, wsTotalMS, snap.TotalDurationMS)
				findings = append(findings, finding)
			}
		}
	}

	// Flag total command count per second (using wall-clock time)
	if snap.StartedAt != "" && snap.SnapshotAt != "" && snap.TotalCommands > 0 {
		startedAt, errStart := time.Parse(time.RFC3339Nano, snap.StartedAt)
		snapshotAt, errSnap := time.Parse(time.RFC3339Nano, snap.SnapshotAt)
		if errStart == nil && errSnap == nil {
			elapsedSec := snapshotAt.Sub(startedAt).Seconds()
			if elapsedSec > 0 {
				rate := float64(snap.TotalCommands) / elapsedSec
				finding := fmt.Sprintf("Command rate: %.2f commands/sec (%d commands in %.1fs)",
					rate, snap.TotalCommands, elapsedSec)
				findings = append(findings, finding)
				if dominantPattern == "" && rate > 10 {
					dominantPattern = "high command rate"
				}
			}
		}
	}

	// Build verdict
	verdict := "No obvious issues detected."
	if dominantPattern != "" {
		verdict = fmt.Sprintf("Dominant pattern: %s.", dominantPattern)
	}
	if len(findings) == 0 {
		findings = []string{"No issues detected."}
	}

	return findings, verdict
}
