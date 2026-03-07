package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/subreddit"
)

// handleSubreddit returns the subreddit posts for all repos.
func (s *Server) handleSubreddit(w http.ResponseWriter, r *http.Request) {
	response := subredditResponse{
		Enabled: subreddit.IsEnabled(s.config),
	}

	if !response.Enabled {
		writeJSON(w, response)
		return
	}

	// Read all repo files from subreddit directory
	subredditDir := s.getSubredditDir()
	entries, err := os.ReadDir(subredditDir)
	if err != nil {
		// No subreddit directory yet - return empty with enabled=true
		writeJSON(w, response)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		slug := entry.Name()[:len(entry.Name())-5] // remove .json
		path := filepath.Join(subredditDir, entry.Name())
		rf, err := subreddit.ReadRepoFile(path)
		if err != nil || rf == nil {
			continue
		}

		repoEntry := subredditRepoEntry{
			Name:  rf.Repo,
			Slug:  slug,
			Posts: make([]subredditPostEntry, len(rf.Posts)),
		}
		for i, p := range rf.Posts {
			repoEntry.Posts[i] = subredditPostEntry{
				ID:        p.ID,
				Title:     p.Title,
				Content:   p.Content,
				Upvotes:   p.Upvotes,
				CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
				UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
				Revision:  p.Revision,
			}
		}
		response.Repos = append(response.Repos, repoEntry)
	}

	// Add next generation time if available
	if nextTime := s.GetNextSubredditGeneration(); nextTime != nil {
		response.NextGenerationAt = nextTime.UTC().Format(time.RFC3339)
	}

	writeJSON(w, response)
}

// TriggerSubredditGeneration starts async generation of subreddit posts.
// This is called when config is saved with subreddit enabled.
func (s *Server) TriggerSubredditGeneration() {
	if !subreddit.IsEnabled(s.config) {
		s.logger.Info("subreddit generation trigger skipped", "reason", "disabled")
		return
	}

	s.logger.Info("subreddit generation triggered", "source", "config_update")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if err := s.GenerateSubredditForAllRepos(ctx); err != nil {
			s.logger.Error("subreddit generation failed", "source", "config_update", "err", err)
			return
		}
	}()
}

// GenerateSubredditForAllRepos generates subreddit posts for all enabled repos.
// This is called by the daemon scheduler on a configured interval.
func (s *Server) GenerateSubredditForAllRepos(ctx context.Context) error {
	cfg := s.config
	subredditDir := s.getSubredditDir()

	// Ensure directory exists
	if err := os.MkdirAll(subredditDir, 0755); err != nil {
		return err
	}

	for _, repo := range cfg.GetRepos() {
		slug := repoSlug(repo.Name)
		if !cfg.GetSubredditRepoEnabled(slug) {
			continue
		}

		s.logger.Info("generating subreddit for repo", "repo", repo.Name, "slug", slug)

		err := subreddit.GenerateRepoPosts(ctx, cfg, repo.Name, slug, repo.BarePath, "main", subredditDir, cfg.GetWorktreeBasePath())
		if err != nil {
			if errors.Is(err, subreddit.ErrDisabled) {
				s.logger.Info("subreddit disabled, skipping")
				continue
			}
			s.logger.Warn("failed to generate subreddit for repo", "repo", repo.Name, "err", err)
			continue
		}

		s.logger.Info("generated subreddit for repo", "repo", repo.Name)

		// Broadcast subreddit update to WebSocket clients
		s.BroadcastSubreddit()
	}

	return nil
}

// getSubredditDir returns the path to the subreddit directory.
func (s *Server) getSubredditDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".schmux", "subreddit")
}

// repoSlug creates a URL-safe slug from a repo name.
func repoSlug(name string) string {
	// Simple slug: lowercase and replace spaces/special chars with dashes
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32) // lowercase
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}

type subredditResponse struct {
	Enabled          bool                 `json:"enabled"`
	Repos            []subredditRepoEntry `json:"repos,omitempty"`
	NextGenerationAt string               `json:"next_generation_at,omitempty"`
}

type subredditRepoEntry struct {
	Name  string               `json:"name"`
	Slug  string               `json:"slug"`
	Posts []subredditPostEntry `json:"posts"`
}

type subredditPostEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Upvotes   int    `json:"upvotes"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Revision  int    `json:"revision"`
}

// BroadcastSubreddit sends a subreddit_updated message to all WebSocket clients.
func (s *Server) BroadcastSubreddit() {
	payload, err := json.Marshal(map[string]interface{}{
		"type": "subreddit_updated",
	})
	if err != nil {
		logging.Sub(s.logger, "ws/dashboard").Error("failed to marshal subreddit_updated message", "err", err)
		return
	}

	s.sessionsConnsMu.RLock()
	defer s.sessionsConnsMu.RUnlock()

	for conn := range s.sessionsConns {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			logging.Sub(s.logger, "ws/dashboard").Error("failed to send subreddit_updated message", "err", err)
		}
	}
}
