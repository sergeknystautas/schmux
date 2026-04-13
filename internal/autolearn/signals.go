//go:build !noautolearn

package autolearn

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

// signalsMu serializes JSONL mutations to prevent read-then-append race conditions.
var signalsMu sync.Mutex

// pkgLogger is the package-level logger for autolearn operations.
// Set via SetLogger from the daemon initialization.
var pkgLogger *log.Logger

// SetLogger sets the package-level logger for autolearn operations.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

// Entry represents a single signal entry or state-change record.
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
		// Return only entries (not state changes) whose ts is not in changed set
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
// NOTE: This function does NOT acquire signalsMu. It is called both directly
// (lock-free, read-only) and from MarkEntriesByTextFromEntries/MarkEntriesDirect
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
			if pkgLogger != nil {
				pkgLogger.Warn("skipping malformed entry", "err", err)
			}
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
// Callers must hold signalsMu.
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

// MarkEntriesDirect writes state-change records for the given entries directly
// to destPath. Unlike MarkEntriesByTextFromEntries, this does not match by text —
// it marks each entry by its timestamp. Use this when you already have the exact
// entries to mark (e.g., all entries sent to a curation run).
func MarkEntriesDirect(entries []Entry, destPath string, stateChange string, proposalID string) error {
	signalsMu.Lock()
	defer signalsMu.Unlock()

	seen := make(map[string]bool)
	for _, e := range entries {
		if e.StateChange != "" {
			continue
		}
		tsStr := e.Timestamp.Format(time.RFC3339)
		if seen[tsStr] {
			continue
		}
		seen[tsStr] = true
		if err := appendEntryToFile(destPath, Entry{
			Timestamp:   time.Now().UTC(),
			StateChange: stateChange,
			EntryTS:     tsStr,
			ProposalID:  proposalID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// MarkEntriesByTextFromEntries marks entries matching the given texts by writing
// state-change records to destPath. Unlike MarkEntriesDirect, this takes
// pre-read entries and matches by entry key text.
func MarkEntriesByTextFromEntries(sourceEntries []Entry, destPath string, stateChange string, entryTexts []string, proposalID string) error {
	signalsMu.Lock()
	defer signalsMu.Unlock()

	textSet := make(map[string]bool, len(entryTexts))
	for _, t := range entryTexts {
		textSet[t] = true
	}

	marked := make(map[string]bool)
	for _, e := range sourceEntries {
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
	return nil
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

// signalEventTypes are the event types that map to signal entries.
var signalEventTypes = map[string]bool{"failure": true, "reflection": true, "friction": true}

// ReadEntriesFromEvents reads signal-relevant events from per-session event files.
// It scans <workspacePath>/.schmux/events/*.jsonl for failure, reflection, friction events
// and converts them to Entry values.
func ReadEntriesFromEvents(workspacePath, workspaceID string, filter EntryFilter) ([]Entry, error) {
	pattern := filepath.Join(state.SchmuxDataDir(workspacePath), "events", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, f := range files {
		sessionID := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		eventLines, err := events.ReadEvents(f, func(raw events.RawEvent) bool {
			return signalEventTypes[raw.Type]
		})
		if err != nil {
			continue
		}
		for _, el := range eventLines {
			entry := eventLineToEntry(el, sessionID, workspaceID)
			entries = append(entries, entry)
		}
	}
	if filter != nil {
		entries = filter(entries)
	}
	return entries, nil
}

// eventLineToEntry converts an event line to an Entry.
func eventLineToEntry(el events.EventLine, sessionID, workspaceID string) Entry {
	ts, _ := time.Parse(time.RFC3339, el.Ts)
	entry := Entry{
		Timestamp: ts,
		Workspace: workspaceID,
		Session:   sessionID,
		Type:      el.Type,
	}
	switch el.Type {
	case "failure":
		var fe events.FailureEvent
		if err := json.Unmarshal(el.Data, &fe); err == nil {
			entry.Tool = fe.Tool
			entry.InputSummary = fe.Input
			entry.ErrorSummary = fe.Error
			entry.Category = fe.Category
		}
	case "reflection", "friction":
		var re events.ReflectionEvent
		if err := json.Unmarshal(el.Data, &re); err == nil {
			entry.Text = re.Text
		}
	}
	return entry
}

// StateDir returns the central autolearn state directory for a repo: ~/.schmux/lore/<repoName>/.
// Creates the directory if it doesn't exist.
func StateDir(repoName string) (string, error) {
	// Validate repoName to prevent path traversal
	if strings.Contains(repoName, "..") || strings.Contains(repoName, "/") || strings.Contains(repoName, string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid repo name: %s", repoName)
	}
	dir := filepath.Join(schmuxdir.Get(), "lore", repoName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create state dir: %w", err)
	}
	return dir, nil
}

// StatePath returns the path to the central state JSONL file for a repo:
// ~/.schmux/lore/<repoName>/state.jsonl.
// Creates the parent directory if it doesn't exist.
func StatePath(repoName string) (string, error) {
	// Validate repoName to prevent path traversal
	if strings.Contains(repoName, "..") || strings.Contains(repoName, "/") || strings.Contains(repoName, string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid repo name: %s", repoName)
	}
	dir, err := StateDir(repoName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.jsonl"), nil
}

// IntentSignal represents a single user intent captured from event logs.
type IntentSignal struct {
	Text      string    `json:"text"`
	Timestamp time.Time `json:"ts"`
	Target    string    `json:"target,omitempty"`
	Persona   string    `json:"persona,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	Session   string    `json:"session,omitempty"`
	Count     int       `json:"count"`
}

// CollectIntentSignals reads event JSONL files from workspace paths and extracts
// intent signals for the autolearn curator. Unlike prompt history collection, this
// preserves workspace context needed for skill distillation.
func CollectIntentSignals(workspacePaths []string) ([]IntentSignal, error) {
	type signalKey struct {
		text      string
		workspace string
	}
	seen := make(map[signalKey]*IntentSignal)

	for _, wsPath := range workspacePaths {
		wsName := filepath.Base(wsPath)
		eventsDir := filepath.Join(state.SchmuxDataDir(wsPath), "events")
		entries, err := os.ReadDir(eventsDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				continue
			}
			eventPath := filepath.Join(eventsDir, entry.Name())
			eventLines, err := events.ReadEvents(eventPath, func(raw events.RawEvent) bool {
				return raw.Type == "status"
			})
			if err != nil {
				continue
			}

			for _, line := range eventLines {
				var status events.StatusEvent
				if err := json.Unmarshal(line.Data, &status); err != nil {
					continue
				}
				if status.Intent == "" {
					continue
				}

				ts, _ := time.Parse(time.RFC3339, status.Ts)
				key := signalKey{text: status.Intent, workspace: wsName}

				if sig, ok := seen[key]; ok {
					sig.Count++
					if ts.After(sig.Timestamp) {
						sig.Timestamp = ts
					}
				} else {
					seen[key] = &IntentSignal{
						Text:      status.Intent,
						Timestamp: ts,
						Workspace: wsName,
						Count:     1,
					}
				}
			}
		}
	}

	result := make([]IntentSignal, 0, len(seen))
	for _, sig := range seen {
		result = append(result, *sig)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	return result, nil
}
