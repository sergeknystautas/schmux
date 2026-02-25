package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type monitorEvent struct {
	SessionID string          `json:"session_id"`
	Event     json.RawMessage `json:"event"`
	Ts        string          `json:"ts"`
}

func (s *Server) handleEventsHistory(w http.ResponseWriter, r *http.Request) {
	const maxEvents = 200

	var allEvents []monitorEvent

	workspaces := s.state.GetWorkspaces()
	for _, ws := range workspaces {
		eventsDir := filepath.Join(ws.Path, ".schmux", "events")
		entries, err := os.ReadDir(eventsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
			filePath := filepath.Join(eventsDir, entry.Name())
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// Extract timestamp for sorting
				var envelope struct {
					Ts string `json:"ts"`
				}
				if err := json.Unmarshal([]byte(line), &envelope); err != nil {
					continue
				}
				allEvents = append(allEvents, monitorEvent{
					SessionID: sessionID,
					Event:     json.RawMessage(line),
					Ts:        envelope.Ts,
				})
			}
		}
	}

	// Sort by timestamp descending, take most recent maxEvents
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Ts > allEvents[j].Ts
	})
	if len(allEvents) > maxEvents {
		allEvents = allEvents[:maxEvents]
	}

	// Reverse so oldest is first (chronological order)
	for i, j := 0, len(allEvents)-1; i < j; i, j = i+1, j-1 {
		allEvents[i], allEvents[j] = allEvents[j], allEvents[i]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allEvents)
}
