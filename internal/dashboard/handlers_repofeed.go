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
