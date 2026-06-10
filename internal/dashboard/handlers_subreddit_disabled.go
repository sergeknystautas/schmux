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
	writeJSONError(w, "Subreddit feature is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) TriggerSubredditGeneration() {}

func (s *Server) GenerateSubredditForAllRepos(_ context.Context) error {
	return nil
}

func (s *Server) BroadcastSubreddit() {}

// repoSlug creates a URL-safe slug from a repo name.
func repoSlug(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32)
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}
