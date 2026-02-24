package dashboard

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/subreddit"
)

// handleSubreddit returns the cached subreddit digest.
func (s *Server) handleSubreddit(w http.ResponseWriter, r *http.Request) {
	response := subredditResponse{
		Enabled: subreddit.IsEnabled(s.config),
	}

	if !response.Enabled {
		writeJSON(w, response)
		return
	}

	// Read cache
	cachePath := s.getSubredditCachePath()
	cache, err := subreddit.ReadCache(cachePath)
	if err != nil {
		// No cache yet - return empty with enabled=true
		writeJSON(w, response)
		return
	}

	response.Content = cache.Content
	response.GeneratedAt = cache.GeneratedAt.Format("2006-01-02T15:04:05Z")
	response.Hours = cache.Hours
	response.CommitCount = cache.CommitCount

	writeJSON(w, response)
}

// getSubredditCachePath returns the path to the subreddit cache file.
func (s *Server) getSubredditCachePath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".schmux", "subreddit.json")
}

type subredditResponse struct {
	Content     string `json:"content,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Hours       int    `json:"hours,omitempty"`
	CommitCount int    `json:"commit_count,omitempty"`
	Enabled     bool   `json:"enabled"`
}
