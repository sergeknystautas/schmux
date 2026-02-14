package lore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry represents a single lore scratchpad entry or state-change record.
type Entry struct {
	Timestamp   time.Time `json:"ts"`
	Workspace   string    `json:"ws,omitempty"`
	Agent       string    `json:"agent,omitempty"`
	Type        string    `json:"type,omitempty"` // "operational" or "codebase"
	Text        string    `json:"text,omitempty"`
	StateChange string    `json:"state_change,omitempty"` // "proposed", "applied", "dismissed"
	EntryTS     string    `json:"entry_ts,omitempty"`     // references the ts of the entry being changed
	ProposalID  string    `json:"proposal_id,omitempty"`
}

// EntryFilter controls which entries are returned by ReadEntries.
type EntryFilter func(entries []Entry) []Entry

// FilterRaw returns only entries that have no state-change records overriding them.
func FilterRaw() EntryFilter {
	return func(entries []Entry) []Entry {
		// Build set of entry timestamps that have state changes
		changed := make(map[string]bool)
		for _, e := range entries {
			if e.StateChange != "" && e.EntryTS != "" {
				changed[e.EntryTS] = true
			}
		}
		// Return only lore entries (not state changes) whose ts is not in changed set
		var result []Entry
		for _, e := range entries {
			if e.StateChange != "" {
				continue // skip state-change records
			}
			tsStr := e.Timestamp.Format(time.RFC3339)
			if changed[tsStr] {
				continue // this entry has been promoted to a different state
			}
			result = append(result, e)
		}
		return result
	}
}

// ParseEntry parses a single JSONL line into an Entry.
func ParseEntry(line string) (Entry, error) {
	var e Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &e); err != nil {
		return Entry{}, fmt.Errorf("failed to parse entry: %w", err)
	}
	return e, nil
}

// ReadEntries reads all entries from a JSONL file and optionally filters them.
func ReadEntries(path string, filter EntryFilter) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		e, err := ParseEntry(line)
		if err != nil {
			fmt.Printf("[lore] skipping malformed entry: %v\n", err)
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if filter != nil {
		entries = filter(entries)
	}
	return entries, nil
}

// AppendEntry appends a single entry to a JSONL file. Creates the file if it doesn't exist.
func AppendEntry(path string, entry Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// AppendStateChange records a state transition for an entry.
func AppendStateChange(path, stateChange, entryTS, proposalID string) error {
	return AppendEntry(path, Entry{
		Timestamp:   time.Now().UTC(),
		StateChange: stateChange,
		EntryTS:     entryTS,
		ProposalID:  proposalID,
	})
}

// MarkEntriesByText finds entries whose Text matches the given texts and appends
// state-change records for them. The entries_used from the curator contain entry
// text strings, so we match by text and use the entry's timestamp as the reference.
func MarkEntriesByText(path string, stateChange string, entryTexts []string, proposalID string) error {
	entries, err := ReadEntries(path, nil)
	if err != nil {
		return err
	}
	textSet := make(map[string]bool, len(entryTexts))
	for _, t := range entryTexts {
		textSet[t] = true
	}
	for _, e := range entries {
		if e.StateChange != "" {
			continue
		}
		if textSet[e.Text] {
			if err := AppendStateChange(path, stateChange, e.Timestamp.Format(time.RFC3339), proposalID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolveEntryState returns the effective state of a lore entry given all state-change records.
// Returns "raw" if no state-change record exists for this entry.
func ResolveEntryState(entry Entry, allEntries []Entry) string {
	if entry.StateChange != "" {
		return "" // this is a state-change record, not a lore entry
	}
	tsStr := entry.Timestamp.Format(time.RFC3339)
	latestState := "raw"
	var latestTS time.Time
	for _, e := range allEntries {
		if e.StateChange != "" && e.EntryTS == tsStr {
			if e.Timestamp.After(latestTS) {
				latestState = e.StateChange
				latestTS = e.Timestamp
			}
		}
	}
	return latestState
}

// FilterByParams returns a filter that applies query parameter filters.
// Supported filters: state (raw/proposed/applied/dismissed), agent, type, limit.
// State resolution requires the full entry list to check state-change records.
func FilterByParams(state, agent, entryType string, limit int) EntryFilter {
	return func(entries []Entry) []Entry {
		var result []Entry
		for _, e := range entries {
			if e.StateChange != "" {
				continue // skip state-change records from output
			}
			if agent != "" && e.Agent != agent {
				continue
			}
			if entryType != "" && e.Type != entryType {
				continue
			}
			if state != "" {
				resolved := ResolveEntryState(e, entries)
				if resolved != state {
					continue
				}
			}
			result = append(result, e)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
		return result
	}
}

// PruneEntries removes applied/dismissed entries older than maxAge from the JSONL file.
// It rewrites the file in-place, keeping only entries that should be retained.
// Returns the number of pruned lines and any error encountered.
func PruneEntries(path string, maxAge time.Duration) (pruned int, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	// Read all raw lines (to preserve original JSON formatting)
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	now := time.Now()
	cutoff := now.Add(-maxAge)

	// Build map: entry_ts -> latest state-change record (applied/dismissed) with its timestamp.
	// We only care about "applied" or "dismissed" states for pruning.
	type stateInfo struct {
		state     string
		timestamp time.Time
	}
	stateMap := make(map[string]stateInfo)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
			continue // skip malformed, will keep the line
		}
		if e.StateChange == "applied" || e.StateChange == "dismissed" {
			if e.EntryTS != "" {
				// Keep the latest state change for each entry_ts
				existing, found := stateMap[e.EntryTS]
				if !found || e.Timestamp.After(existing.timestamp) {
					stateMap[e.EntryTS] = stateInfo{
						state:     e.StateChange,
						timestamp: e.Timestamp,
					}
				}
			}
		}
	}

	// Build set of entry_ts values to prune: those in applied/dismissed state
	// whose state-change timestamp is older than cutoff
	pruneSet := make(map[string]bool)
	for entryTS, info := range stateMap {
		if info.timestamp.Before(cutoff) {
			pruneSet[entryTS] = true
		}
	}

	if len(pruneSet) == 0 {
		return 0, nil
	}

	// Filter lines: keep those not in pruneSet
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			kept = append(kept, line)
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
			kept = append(kept, line) // keep malformed lines
			continue
		}

		// Check if this is a lore entry that should be pruned
		if e.StateChange == "" {
			tsStr := e.Timestamp.Format(time.RFC3339)
			if pruneSet[tsStr] {
				pruned++
				continue // prune this lore entry
			}
		}

		// Check if this is a state-change record referencing a pruned entry
		if e.StateChange != "" && e.EntryTS != "" {
			if pruneSet[e.EntryTS] {
				pruned++
				continue // prune this state-change record
			}
		}

		kept = append(kept, line)
	}

	if pruned == 0 {
		return 0, nil
	}

	// Write remaining lines to a temp file, then rename (atomic)
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "lore-prune-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	w := bufio.NewWriter(tmp)
	for _, line := range kept {
		if _, err := w.WriteString(line + "\n"); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return 0, fmt.Errorf("failed to write temp file: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return 0, fmt.Errorf("failed to flush temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("failed to rename temp file: %w", err)
	}

	return pruned, nil
}
