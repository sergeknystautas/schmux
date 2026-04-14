//go:build !norepofeed

package dashboard

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/repofeed"
)

type repofeedListResponse struct {
	Repos     []repofeedRepoSummary `json:"repos"`
	LastFetch string                `json:"last_fetch,omitempty"`
}

type repofeedRepoSummary struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	ActiveIntents int    `json:"active_intents"`
	LandedCount   int    `json:"landed_count"`
}

type repofeedRepoResponse struct {
	Name      string                `json:"name"`
	Slug      string                `json:"slug"`
	Intents   []repofeedIntentEntry `json:"intents"`
	Landed    []subredditPostEntry  `json:"landed"`
	LastFetch string                `json:"last_fetch,omitempty"`
}

type repofeedIntentEntry struct {
	Developer    string   `json:"developer"`
	DisplayName  string   `json:"display_name"`
	Intent       string   `json:"intent"`
	Status       string   `json:"status"`
	Started      string   `json:"started"`
	Branches     []string `json:"branches"`
	SessionCount int      `json:"session_count"`
	Agents       []string `json:"agents"`
}

// handleRepofeedList handles GET /api/repofeed — returns repo list with summary counts.
func (s *Server) handleRepofeedList(w http.ResponseWriter, r *http.Request) {
	response := repofeedListResponse{
		Repos: []repofeedRepoSummary{},
	}

	if s.repofeedConsumer == nil {
		writeJSON(w, response)
		return
	}

	slugs := s.repofeedConsumer.GetAllRepoSlugs()
	sort.Strings(slugs)

	for _, slug := range slugs {
		intents := s.repofeedConsumer.GetIntentsForRepo(slug)
		activeCount := 0
		for _, intent := range intents {
			if intent.Status == repofeed.StatusActive {
				activeCount++
			}
		}
		response.Repos = append(response.Repos, repofeedRepoSummary{
			Name:          slug,
			Slug:          slug,
			ActiveIntents: activeCount,
		})
	}

	writeJSON(w, response)
}

// handleRepofeedRepo handles GET /api/repofeed/{slug} — returns full intents for one repo.
func (s *Server) handleRepofeedRepo(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeJSONError(w, "missing slug", http.StatusBadRequest)
		return
	}

	response := repofeedRepoResponse{
		Name:    slug,
		Slug:    slug,
		Intents: []repofeedIntentEntry{},
		Landed:  []subredditPostEntry{},
	}

	if s.repofeedConsumer != nil {
		intents := s.repofeedConsumer.GetIntentsForRepo(slug)
		for _, intent := range intents {
			response.Intents = append(response.Intents, repofeedIntentEntry{
				Developer:    intent.Developer,
				DisplayName:  intent.DisplayName,
				Intent:       intent.Intent,
				Status:       string(intent.Status),
				Started:      intent.Started,
				Branches:     intent.Branches,
				SessionCount: intent.SessionCount,
				Agents:       intent.Agents,
			})
		}
	}

	writeJSON(w, response)
}

// SetRepofeedPublisher sets the repofeed publisher reference.
func (s *Server) SetRepofeedPublisher(p *repofeed.Publisher) {
	s.repofeedPublisher = p
}

// SetRepofeedConsumer sets the repofeed consumer reference.
func (s *Server) SetRepofeedConsumer(c *repofeed.Consumer) {
	s.repofeedConsumer = c
}

// SetRepofeedDismissed sets the repofeed dismissed store reference.
func (s *Server) SetRepofeedDismissed(d *repofeed.DismissedStore) {
	s.repofeedDismissed = d
}

// SetRepofeedSummaryCache sets the repofeed summary cache reference.
func (s *Server) SetRepofeedSummaryCache(sc *repofeed.SummaryCache) {
	s.repofeedSummaryCache = sc
}

// handleRepofeedOutgoing handles GET /api/repofeed/outgoing — returns per-workspace LLM summaries.
func (s *Server) handleRepofeedOutgoing(w http.ResponseWriter, r *http.Request) {
	type outgoingEntry struct {
		WorkspaceID string `json:"workspace_id"`
		Summary     string `json:"summary,omitempty"`
	}

	var entries []outgoingEntry
	if s.repofeedSummaryCache != nil {
		for _, wsID := range s.repofeedSummaryCache.AllKeys() {
			entry := s.repofeedSummaryCache.Get(wsID)
			if entry != nil {
				entries = append(entries, outgoingEntry{
					WorkspaceID: wsID,
					Summary:     entry.Summary,
				})
			}
		}
	}
	if entries == nil {
		entries = []outgoingEntry{}
	}
	writeJSON(w, map[string]interface{}{"entries": entries})
}

// handleRepofeedIncoming handles GET /api/repofeed/incoming — returns all intents from other developers (v1+v2 normalized).
func (s *Server) handleRepofeedIncoming(w http.ResponseWriter, r *http.Request) {
	type incomingEntry struct {
		Developer      string `json:"developer"`
		DisplayName    string `json:"display_name"`
		Intent         string `json:"intent"`
		Status         string `json:"status"`
		Started        string `json:"started,omitempty"`
		LastActiveDate string `json:"last_active_date,omitempty"`
		WorkspaceID    string `json:"workspace_id,omitempty"`
	}

	var entries []incomingEntry
	if s.repofeedConsumer != nil {
		for _, intent := range s.repofeedConsumer.GetAllIntents() {
			entries = append(entries, incomingEntry{
				Developer:      intent.Developer,
				DisplayName:    intent.DisplayName,
				Intent:         intent.Intent,
				Status:         string(intent.Status),
				Started:        intent.Started,
				LastActiveDate: intent.LastActiveDate,
				WorkspaceID:    intent.WorkspaceID,
			})
		}
	}
	if entries == nil {
		entries = []incomingEntry{}
	}
	writeJSON(w, map[string]interface{}{"entries": entries})
}

// handleRepofeedDismiss handles POST /api/repofeed/dismiss — marks a completed intent as dismissed.
func (s *Server) handleRepofeedDismiss(w http.ResponseWriter, r *http.Request) {
	if s.repofeedDismissed == nil {
		writeJSONError(w, "repofeed dismissed store not available", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Developer   string `json:"developer"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Developer == "" || req.WorkspaceID == "" {
		writeJSONError(w, "developer and workspace_id are required", http.StatusBadRequest)
		return
	}

	s.repofeedDismissed.Dismiss(req.Developer, req.WorkspaceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// BroadcastRepofeed sends a repofeed_updated message to all WebSocket clients.
func (s *Server) BroadcastRepofeed() {
	payload, err := json.Marshal(map[string]interface{}{
		"type": "repofeed_updated",
	})
	if err != nil {
		logging.Sub(s.logger, "ws/dashboard").Error("failed to marshal repofeed_updated message", "err", err)
		return
	}

	s.sessionsConnsMu.RLock()
	defer s.sessionsConnsMu.RUnlock()

	for conn := range s.sessionsConns {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			logging.Sub(s.logger, "ws/dashboard").Error("failed to send repofeed_updated message", "err", err)
		}
	}
}
