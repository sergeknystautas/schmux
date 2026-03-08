package emergence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/events"
)

// promptInfo tracks deduplicated prompt occurrences.
type promptInfo struct {
	count    int
	lastSeen time.Time
}

// CollectPromptHistory reads event JSONL files from workspace paths and extracts
// unique prompts (from status events with non-empty intent fields).
// Returns at most maxResults entries, sorted by last_seen descending.
func CollectPromptHistory(workspacePaths []string, maxResults int) []contracts.PromptHistoryEntry {
	seen := make(map[string]*promptInfo)

	for _, wsPath := range workspacePaths {
		eventsDir := filepath.Join(wsPath, ".schmux", "events")
		entries, err := os.ReadDir(eventsDir)
		if err != nil {
			continue // workspace may not have events yet
		}

		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				continue
			}
			eventPath := filepath.Join(eventsDir, entry.Name())
			collectPromptsFromFile(eventPath, seen)
		}
	}

	// Convert to response entries.
	result := make([]contracts.PromptHistoryEntry, 0, len(seen))
	for text, info := range seen {
		result = append(result, contracts.PromptHistoryEntry{
			Text:     text,
			LastSeen: info.lastSeen,
			Count:    info.count,
		})
	}

	// Sort by last_seen descending.
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastSeen.After(result[j].LastSeen)
	})

	if maxResults > 0 && len(result) > maxResults {
		result = result[:maxResults]
	}
	return result
}

// collectPromptsFromFile reads a single JSONL event file and adds intents to the seen map.
func collectPromptsFromFile(path string, seen map[string]*promptInfo) {
	eventLines, err := events.ReadEvents(path, func(raw events.RawEvent) bool {
		return raw.Type == "status"
	})
	if err != nil {
		return
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

		info, ok := seen[status.Intent]
		if !ok {
			seen[status.Intent] = &promptInfo{count: 1, lastSeen: ts}
		} else {
			info.count++
			if ts.After(info.lastSeen) {
				info.lastSeen = ts
			}
		}
	}
}

// CollectIntentSignals reads event JSONL files from workspace paths and extracts
// intent signals for the emergence curator. Unlike CollectPromptHistory, this
// preserves workspace context needed for skill distillation.
func CollectIntentSignals(workspacePaths []string) ([]IntentSignal, error) {
	type signalKey struct {
		text      string
		workspace string
	}
	seen := make(map[signalKey]*IntentSignal)

	for _, wsPath := range workspacePaths {
		wsName := filepath.Base(wsPath)
		eventsDir := filepath.Join(wsPath, ".schmux", "events")
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
