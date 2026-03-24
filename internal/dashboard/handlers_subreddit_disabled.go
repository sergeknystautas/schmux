//go:build nosubreddit

package dashboard

import (
	"context"
	"net/http"
)

type subredditPostEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Upvotes   int    `json:"upvotes"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Revision  int    `json:"revision"`
}

func (s *Server) handleSubreddit(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Subreddit feature is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) TriggerSubredditGeneration() {}

func (s *Server) GenerateSubredditForAllRepos(_ context.Context) error {
	return nil
}

func (s *Server) BroadcastSubreddit() {}
