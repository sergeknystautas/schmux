package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/state"
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
		eventsDir := filepath.Join(state.SchmuxDataDir(ws.Path), "events")
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

// handleGetSessionEvents returns events for a single session.
func (s *Server) handleGetSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	// Parse query params
	typeFilter := r.URL.Query().Get("type")
	lastN := 0
	if lastStr := r.URL.Query().Get("last"); lastStr != "" {
		if n, err := strconv.Atoi(lastStr); err == nil && n > 0 {
			lastN = n
		}
	}

	// Find the session to get workspace path
	sess, ok := s.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}

	var lines [][]byte

	if sess.RemoteHostID != "" {
		// Remote: cat the events file via RunCommand
		if s.remoteManager == nil {
			writeJSONError(w, "remote manager not available", http.StatusServiceUnavailable)
			return
		}
		conn := s.remoteManager.GetConnection(sess.RemoteHostID)
		if conn == nil {
			writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
			return
		}
		ws, wsOk := s.state.GetWorkspace(sess.WorkspaceID)
		if !wsOk {
			writeJSONError(w, "workspace not found", http.StatusNotFound)
			return
		}
		eventsPath := filepath.Join(state.SchmuxDataDirForVCS(ws.RemotePath, ws.VCS), "events", sessionID+".jsonl")
		output, err := conn.RunCommand(r.Context(), ws.RemotePath, fmt.Sprintf("cat %s 2>/dev/null || true", eventsPath))
		if err != nil {
			writeJSONError(w, fmt.Sprintf("failed to read events: %v", err), http.StatusInternalServerError)
			return
		}
		for _, line := range bytes.Split([]byte(output), []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 {
				lines = append(lines, line)
			}
		}
	} else {
		// Local: read the JSONL file directly
		ws, wsOk := s.state.GetWorkspace(sess.WorkspaceID)
		if !wsOk {
			writeJSONError(w, "workspace not found", http.StatusNotFound)
			return
		}
		eventsPath := filepath.Join(state.SchmuxDataDir(ws.Path), "events", sessionID+".jsonl")
		data, err := os.ReadFile(eventsPath)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, []interface{}{})
				return
			}
			writeJSONError(w, fmt.Sprintf("failed to read events: %v", err), http.StatusInternalServerError)
			return
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 {
				lines = append(lines, line)
			}
		}
	}

	// Filter by type if specified
	var result []json.RawMessage
	for _, line := range lines {
		if typeFilter != "" {
			var raw struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(line, &raw); err != nil {
				continue
			}
			if raw.Type != typeFilter {
				continue
			}
		}
		result = append(result, json.RawMessage(line))
	}

	// Apply --last N
	if lastN > 0 && len(result) > lastN {
		result = result[len(result)-lastN:]
	}

	if result == nil {
		result = []json.RawMessage{}
	}
	writeJSON(w, result)
}
