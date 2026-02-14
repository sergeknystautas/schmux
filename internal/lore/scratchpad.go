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
