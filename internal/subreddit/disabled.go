//go:build nosubreddit

package subreddit

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrDisabled is returned when the subreddit feature is not available.
	ErrDisabled = errors.New("subreddit module is not available in this build")
	// ErrInvalidResponse is returned when the LLM response cannot be parsed.
	ErrInvalidResponse = errors.New("invalid subreddit response")
)

// IsAvailable reports whether the subreddit module is included in this build.
func IsAvailable() bool { return false }

// IsEnabled returns false when the subreddit module is excluded.
func IsEnabled(_ interface{ GetSubredditTarget() string }) bool {
	return false
}

// PostCommit tracks a commit incorporated into a post.
type PostCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// Post is a single subreddit post for a repository.
type Post struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Content   string       `json:"content"`
	Upvotes   int          `json:"upvotes"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Revision  int          `json:"revision"`
	Commits   []PostCommit `json:"commits"`
}

// RepoFile is the on-disk format for a repo's subreddit posts.
type RepoFile struct {
	Repo  string `json:"repo"`
	Posts []Post `json:"posts"`
}

// Config provides the minimal interface needed for generation.
type Config interface {
	GetSubredditTarget() string
	GetSubredditCheckingRange() int
	GetSubredditInterval() int
	GetSubredditMaxPosts() int
	GetSubredditMaxAge() int
	GetSubredditRepoEnabled(repoSlug string) bool
}

// FullConfig provides the interface for LLM execution.
type FullConfig interface {
	Config
}

// ReadRepoFile returns ErrDisabled when the subreddit module is excluded.
func ReadRepoFile(_ string) (*RepoFile, error) {
	return nil, ErrDisabled
}

// GenerateRepoPosts returns ErrDisabled when the subreddit module is excluded.
func GenerateRepoPosts(_ context.Context, _ FullConfig, _, _, _, _, _, _ string) error {
	return ErrDisabled
}
