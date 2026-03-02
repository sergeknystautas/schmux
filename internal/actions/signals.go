package actions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sergeknystautas/schmux/internal/events"
)

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

// spawnEvent is the JSONL structure for spawn events.
type spawnEvent struct {
	Ts      string `json:"ts"`
	Type    string `json:"type"`
	Target  string `json:"target,omitempty"`
	Persona string `json:"persona,omitempty"`
	Intent  string `json:"intent,omitempty"`
}

// CollectIntentSignals reads all event files from workspace paths and returns
// deduplicated intent signals sorted by count descending.
func CollectIntentSignals(workspacePaths []string) ([]IntentSignal, error) {
	type signalKey struct {
		text   string
		target string
	}
	type accumulator struct {
		signal IntentSignal
		count  int
	}
	seen := make(map[signalKey]*accumulator)

	for _, wsPath := range workspacePaths {
		eventsDir := filepath.Join(wsPath, ".schmux", "events")
		entries, err := os.ReadDir(eventsDir)
		if err != nil {
			continue
		}

		wsName := filepath.Base(wsPath)

		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				continue
			}

			sessionID := entry.Name()[:len(entry.Name())-6] // strip .jsonl
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

				// Try to get target from a spawn event in the same file.
				// For now, we don't have per-line target info from status events,
				// so we leave it empty unless enriched later.
				key := signalKey{text: status.Intent}
				if acc, ok := seen[key]; ok {
					acc.count++
					if ts.After(acc.signal.Timestamp) {
						acc.signal.Timestamp = ts
					}
				} else {
					seen[key] = &accumulator{
						signal: IntentSignal{
							Text:      status.Intent,
							Timestamp: ts,
							Workspace: wsName,
							Session:   sessionID,
						},
						count: 1,
					}
				}
			}
		}
	}

	// Convert to result slice.
	result := make([]IntentSignal, 0, len(seen))
	for _, acc := range seen {
		acc.signal.Count = acc.count
		result = append(result, acc.signal)
	}

	// Sort by count descending, then by timestamp descending.
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	return result, nil
}
