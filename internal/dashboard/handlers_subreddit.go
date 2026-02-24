package dashboard

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

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

// TriggerSubredditGeneration starts async generation of the subreddit digest.
// This is called when config is saved with subreddit enabled.
func (s *Server) TriggerSubredditGeneration() {
	if !subreddit.IsEnabled(s.config) {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cache, err := s.generateSubreddit(ctx)
		if err != nil {
			log.Printf("subreddit generation failed: %v", err)
			return
		}

		cachePath := s.getSubredditCachePath()
		if err := subreddit.WriteCache(cachePath, cache); err != nil {
			log.Printf("failed to write subreddit cache: %v", err)
		}
	}()
}

// generateSubreddit generates a new subreddit digest.
func (s *Server) generateSubreddit(ctx context.Context) (subreddit.Cache, error) {
	cfg := s.config

	// Build repo info list
	var repos []subreddit.RepoInfo
	for _, r := range cfg.GetRepos() {
		repos = append(repos, subreddit.RepoInfo{
			Name:          r.Name,
			BarePath:      r.BarePath,
			DefaultBranch: "main",
		})
	}

	// Gather commits from all repos
	commits, err := subreddit.GatherCommits(ctx, repos, cfg.GetWorktreeBasePath(), cfg.GetSubredditHours())
	if err != nil {
		s.logger.Warn("failed to gather commits", "err", err)
		// Continue with empty commits - the digest will show "quiet period"
	}

	s.logger.Debug("generating digest", "commits", len(commits))

	// Generate new digest
	cache, err := subreddit.Generate(ctx, cfg, cfg, nil, commits, "", 0)
	if err != nil {
		return subreddit.Cache{}, err
	}

	return cache, nil
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
