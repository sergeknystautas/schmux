package lore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// scratchpadMu serializes JSONL mutations to prevent read-then-append race conditions.
var scratchpadMu sync.Mutex

// Entry represents a single lore scratchpad entry or state-change record.
type Entry struct {
	Timestamp    time.Time `json:"ts"`
	Workspace    string    `json:"ws,omitempty"`
	Session      string    `json:"session,omitempty"` // session ID for per-session tracking
	Agent        string    `json:"agent,omitempty"`
	Type         string    `json:"type,omitempty"` // "failure", "reflection", "friction", "operational", "codebase" (legacy)
	Text         string    `json:"text,omitempty"`
	Tool         string    `json:"tool,omitempty"`          // tool name for failure entries
	InputSummary string    `json:"input_summary,omitempty"` // summarized tool input for failure entries
	ErrorSummary string    `json:"error_summary,omitempty"` // summarized error for failure entries
	Category     string    `json:"category,omitempty"`      // error category for failure entries
	StateChange  string    `json:"state_change,omitempty"`  // "proposed", "applied", "dismissed"
	EntryTS      string    `json:"entry_ts,omitempty"`      // references the ts of the entry being changed
	ProposalID   string    `json:"proposal_id,omitempty"`
}

// EntryKey returns a canonical identifier string for matching entries across
// curator validation, state marking, and deduplication.
// For text-based entries (reflection, friction, operational, codebase): returns Text.
// For failure entries: returns "Tool: InputSummary" (or just InputSummary if Tool is empty).
func (e Entry) EntryKey() string {
	if e.Type == "failure" {
		if e.Tool != "" {
			return e.Tool + ": " + e.InputSummary
		}
		return e.InputSummary
	}
	return e.Text
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
// NOTE: This function does NOT acquire scratchpadMu. It is called both directly
// (lock-free, read-only) and from MarkEntriesByText/MarkEntriesByTextMulti
// (which already hold the lock). Do NOT add locking here without refactoring
// those callers to avoid deadlock.
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
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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

// appendEntryToFile performs the file I/O for appending an entry.
// Callers must hold scratchpadMu.
func appendEntryToFile(path string, entry Entry) error {
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
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync entry: %w", err)
	}
	return nil
}

// AppendEntry appends a single entry to a JSONL file. Creates the file if it doesn't exist.
// Protected by scratchpadMu to prevent data loss during concurrent append + prune operations.
func AppendEntry(path string, entry Entry) error {
	scratchpadMu.Lock()
	defer scratchpadMu.Unlock()
	return appendEntryToFile(path, entry)
}

// AppendStateChange records a state transition for an entry.
func AppendStateChange(path, stateChange, entryTS, proposalID string) error {
	scratchpadMu.Lock()
	defer scratchpadMu.Unlock()
	return appendEntryToFile(path, Entry{
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
	scratchpadMu.Lock()
	defer scratchpadMu.Unlock()

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
		if textSet[e.EntryKey()] {
			if err := appendEntryToFile(path, Entry{
				Timestamp:   time.Now().UTC(),
				StateChange: stateChange,
				EntryTS:     e.Timestamp.Format(time.RFC3339),
				ProposalID:  proposalID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// MarkEntriesByTextMulti reads entries from multiple source paths to find timestamps,
// then writes state-change records to a separate destination path.
// This supports the architecture where raw entries live in workspace directories
// but state-change records are stored in a central state file.
func MarkEntriesByTextMulti(sourcePaths []string, destPath string, stateChange string, entryTexts []string, proposalID string) error {
	scratchpadMu.Lock()
	defer scratchpadMu.Unlock()

	textSet := make(map[string]bool, len(entryTexts))
	for _, t := range entryTexts {
		textSet[t] = true
	}

	// Read entries from all source paths to find matching timestamps
	// Track which texts we've already marked to avoid duplicates across files
	marked := make(map[string]bool)
	for _, srcPath := range sourcePaths {
		entries, err := ReadEntries(srcPath, nil)
		if err != nil {
			continue // skip unreadable files
		}
		for _, e := range entries {
			if e.StateChange != "" {
				continue
			}
			key := e.EntryKey()
			if textSet[key] && !marked[key] {
				marked[key] = true
				if err := appendEntryToFile(destPath, Entry{
					Timestamp:   time.Now().UTC(),
					StateChange: stateChange,
					EntryTS:     e.Timestamp.Format(time.RFC3339),
					ProposalID:  proposalID,
				}); err != nil {
					return err
				}
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

// buildStateMap creates a map from entry timestamp to its latest resolved state.
// Assumes entries are in chronological order (append-only JSONL), so the last
// state-change record for a given EntryTS wins. This matches the file format
// where newer state overrides are appended after the original entry.
func buildStateMap(entries []Entry) map[string]string {
	stateMap := make(map[string]string)
	for _, e := range entries {
		if e.StateChange != "" && e.EntryTS != "" {
			stateMap[e.EntryTS] = e.StateChange
		}
	}
	return stateMap
}

// FilterByParams returns a filter that applies query parameter filters.
// Supported filters: state (raw/proposed/applied/dismissed), agent, type, limit.
// State resolution requires the full entry list to check state-change records.
func FilterByParams(state, agent, entryType string, limit int) EntryFilter {
	return func(entries []Entry) []Entry {
		sm := buildStateMap(entries)
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
				tsStr := e.Timestamp.Format(time.RFC3339)
				resolved := sm[tsStr]
				if resolved == "" {
					resolved = "raw"
				}
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
// Protected by scratchpadMu for safe concurrent use with AppendEntry.
// Returns the number of pruned lines and any error encountered.
func PruneEntries(path string, maxAge time.Duration) (pruned int, err error) {
	scratchpadMu.Lock()
	defer scratchpadMu.Unlock()

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
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	// Parse each line once, pairing raw text with parsed entry
	type parsedLine struct {
		raw   string
		entry Entry
		valid bool
	}
	var parsed []parsedLine
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
			parsed = append(parsed, parsedLine{raw: line, valid: false})
			continue
		}
		parsed = append(parsed, parsedLine{raw: line, entry: e, valid: true})
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

	for _, pl := range parsed {
		if !pl.valid {
			continue
		}
		e := pl.entry
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
	for _, pl := range parsed {
		if !pl.valid {
			kept = append(kept, pl.raw) // keep malformed lines
			continue
		}
		e := pl.entry

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

		kept = append(kept, pl.raw)
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

// ReadEntriesMulti reads entries from multiple JSONL files, concatenates them,
// deduplicates by ts+ws+text, then applies the optional filter.
// Files that don't exist are silently skipped.
func ReadEntriesMulti(paths []string, filter EntryFilter) ([]Entry, error) {
	type dedupKey struct {
		ts  string
		ws  string
		key string
	}
	seen := make(map[dedupKey]bool)
	var all []Entry

	for _, p := range paths {
		entries, err := ReadEntries(p, nil)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
		for _, e := range entries {
			key := dedupKey{
				ts:  e.Timestamp.Format(time.RFC3339),
				ws:  e.Workspace,
				key: e.EntryKey(),
			}
			// State-change records (no text, have StateChange) are always included
			// since they reference entries by EntryTS and don't have a text-based key.
			if e.StateChange != "" || !seen[key] {
				seen[key] = true
				all = append(all, e)
			}
		}
	}

	if filter != nil {
		all = filter(all)
	}
	return all, nil
}

// DeduplicateEntries removes duplicate lore entries from a slice.
// Duplicates are identified by the combination of timestamp, type, and content key
// (EntryKey: Text for reflection/friction, Tool+InputSummary for failure).
// State-change records are always kept since they don't carry duplicate content.
// The first occurrence of each entry is kept; subsequent duplicates are dropped.
func DeduplicateEntries(entries []Entry) []Entry {
	type dedupKey struct {
		ts      string
		entType string
		content string
	}
	seen := make(map[dedupKey]bool, len(entries))
	result := make([]Entry, 0, len(entries))
	for _, e := range entries {
		// State-change records are always included
		if e.StateChange != "" {
			result = append(result, e)
			continue
		}
		key := dedupKey{
			ts:      e.Timestamp.Format(time.RFC3339),
			entType: e.Type,
			content: e.EntryKey(),
		}
		if !seen[key] {
			seen[key] = true
			result = append(result, e)
		}
	}
	return result
}

// LoreStateDir returns the central lore state directory for a repo: ~/.schmux/lore/<repoName>/.
// Creates the directory if it doesn't exist.
func LoreStateDir(repoName string) (string, error) {
	// Validate repoName to prevent path traversal
	if strings.Contains(repoName, "..") || strings.Contains(repoName, "/") || strings.Contains(repoName, string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid repo name: %s", repoName)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	dir := filepath.Join(homeDir, ".schmux", "lore", repoName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create lore state dir: %w", err)
	}
	return dir, nil
}

// LoreStatePath returns the path to the central state JSONL file for a repo:
// ~/.schmux/lore/<repoName>/state.jsonl.
// Creates the parent directory if it doesn't exist.
func LoreStatePath(repoName string) (string, error) {
	// Validate repoName to prevent path traversal
	if strings.Contains(repoName, "..") || strings.Contains(repoName, "/") || strings.Contains(repoName, string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid repo name: %s", repoName)
	}
	dir, err := LoreStateDir(repoName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.jsonl"), nil
}
