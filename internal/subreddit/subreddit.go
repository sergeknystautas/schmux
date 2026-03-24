//go:build !nosubreddit

package subreddit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
)

const (
	// DefaultTimeout is the default timeout for LLM generation.
	DefaultTimeout = 90 * time.Second
)

var (
	// ErrDisabled is returned when the subreddit feature is not configured.
	ErrDisabled = errors.New("subreddit digest is disabled")
	// ErrInvalidResponse is returned when the LLM response cannot be parsed.
	ErrInvalidResponse = errors.New("invalid subreddit response")

	postIDCounter atomic.Uint64
)

// IsAvailable reports whether the subreddit module is included in this build.
func IsAvailable() bool { return true }

// IsEnabled returns true if the subreddit feature is enabled.
func IsEnabled(getter interface{ GetSubredditTarget() string }) bool {
	return getter.GetSubredditTarget() != ""
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

// RepoFilePath returns the path for a repo's subreddit JSON file.
func RepoFilePath(subredditDir, repoSlug string) string {
	return filepath.Join(subredditDir, repoSlug+".json")
}

// ReadRepoFile reads a repo's subreddit posts from disk. Returns nil, nil if file doesn't exist.
func ReadRepoFile(path string) (*RepoFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rf RepoFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, err
	}
	return &rf, nil
}

// WriteRepoFile writes a repo's subreddit posts to disk atomically.
func WriteRepoFile(path string, rf *RepoFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func newPostID() string {
	seq := postIDCounter.Add(1)
	return fmt.Sprintf("post-%d-%d", time.Now().UTC().UnixMilli(), seq)
}

// CleanupPosts removes posts exceeding maxPosts count or maxAgeDays age.
// Posts are assumed to be sorted by recency (newest first).
func CleanupPosts(posts []Post, maxPosts, maxAgeDays int) []Post {
	cutoff := time.Now().UTC().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	var kept []Post
	for _, p := range posts {
		if p.CreatedAt.Before(cutoff) {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) > maxPosts {
		kept = kept[:maxPosts]
	}
	return kept
}

// IncrementalSystemPrompt is the system prompt for incremental generation.
var IncrementalSystemPrompt = `You are an enthusiastic user on r/schmux sharing news about a software project.
Write in a conversational, user-focused tone. Be casual and opinionated.
Do not mention authors or implementation details. Focus on what changed and why it matters.

You will receive new commits and optionally existing recent posts.
Decide whether the new commits represent a NEW topic (create a new post) or extend an EXISTING topic (update that post).

CRITICAL: Synthesize ALL commits into ONE cohesive summary. Do NOT describe each commit separately.
Combine related changes into a single narrative. Be concise.

Return a JSON object with these fields:
- "action": "create" or "update"
- "post_id": (only if action is "update") the ID of the post to update
- "title": short headline (max ~80 chars)
- "content": markdown body (1-2 short paragraphs max, 3-5 sentences total)
- "upvotes": importance score 0-5 (logarithmic: 0=trivial, 1=minor, 2=moderate, 3=significant, 4=major, 5=landmark)

When updating, rewrite the entire post to incorporate both old and new changes seamlessly.`

// IncrementalResult represents the parsed result of incremental generation.
type IncrementalResult struct {
	Action  string `json:"action"`
	PostID  string `json:"post_id,omitempty"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Upvotes int    `json:"upvotes"`
}

// BuildIncrementalPrompt constructs the user prompt for incremental generation.
func BuildIncrementalPrompt(newCommits []PostCommit, existingPosts []Post) string {
	var b strings.Builder
	b.WriteString("## New Commits\n\n")
	for _, c := range newCommits {
		fmt.Fprintf(&b, "- [%s] %s\n", c.SHA[:min(7, len(c.SHA))], c.Subject)
	}
	if len(existingPosts) > 0 {
		b.WriteString("\n## Existing Recent Posts (candidates for update)\n\n")
		for _, p := range existingPosts {
			fmt.Fprintf(&b, "### %s (id: %s)\n%s\n\n", p.Title, p.ID, truncate(p.Content, 300))
		}
	}
	return b.String()
}

// ParseIncrementalResult parses the LLM response for incremental generation.
func ParseIncrementalResult(raw string) (*IncrementalResult, error) {
	cleaned := stripMarkdownCodeBlock(raw)
	cleaned = extractJSONObject(cleaned)
	var result IncrementalResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if result.Action != "create" && result.Action != "update" {
		return nil, fmt.Errorf("%w: invalid action %q", ErrInvalidResponse, result.Action)
	}
	if result.Action == "update" && result.PostID == "" {
		return nil, fmt.Errorf("%w: update action requires post_id", ErrInvalidResponse)
	}
	if result.Title == "" || result.Content == "" {
		return nil, fmt.Errorf("%w: title and content required", ErrInvalidResponse)
	}
	return &result, nil
}

// BootstrapSystemPrompt is the system prompt for bootstrap generation.
var BootstrapSystemPrompt = `You are an enthusiastic user on r/schmux sharing news about a software project.
Write in a conversational, user-focused tone. Be casual and opinionated.
Do not mention authors or implementation details. Focus on what changed and why it matters.

You will receive a batch of commits from the past several days.
Group them into distinct posts by topic — related changes should be in the same post.
Return a JSON array of posts.

CRITICAL: Synthesize commits into concise summaries. Do NOT describe each commit separately.
Combine related changes into a single narrative. Be brief.

Each post must have:
- "title": short headline (max ~80 chars)
- "content": markdown body (1-2 short paragraphs max, 3-5 sentences total)
- "upvotes": importance score 0-5 (logarithmic: 0=trivial, 1=minor, 2=moderate, 3=significant, 4=major, 5=landmark)
- "commit_shas": array of SHA strings that belong to this post

Order posts from most recent topic to oldest.`

// BootstrapPost represents a post returned from bootstrap generation.
type BootstrapPost struct {
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Upvotes    int      `json:"upvotes"`
	CommitSHAs []string `json:"commit_shas"`
}

// BuildBootstrapPrompt constructs the user prompt for bootstrap generation.
func BuildBootstrapPrompt(commits []PostCommit, maxAgeDays int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Commits from the past %d days\n\n", maxAgeDays)
	for _, c := range commits {
		fmt.Fprintf(&b, "- [%s] %s\n", c.SHA[:min(7, len(c.SHA))], c.Subject)
	}
	return b.String()
}

// ParseBootstrapResult parses the LLM response for bootstrap generation.
func ParseBootstrapResult(raw string) ([]BootstrapPost, error) {
	cleaned := stripMarkdownCodeBlock(raw)
	cleaned = strings.TrimSpace(cleaned)
	// Find the JSON array
	start := strings.Index(cleaned, "[")
	end := strings.LastIndex(cleaned, "]")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("%w: no JSON array found", ErrInvalidResponse)
	}
	cleaned = cleaned[start : end+1]
	var posts []BootstrapPost
	if err := json.Unmarshal([]byte(cleaned), &posts); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if len(posts) == 0 {
		return nil, fmt.Errorf("%w: empty post array", ErrInvalidResponse)
	}
	return posts, nil
}

// ApplyIncrementalResult applies an incremental LLM result to a repo file.
// It creates a new post or updates an existing one based on the action.
func ApplyIncrementalResult(rf *RepoFile, result *IncrementalResult, newCommits []PostCommit) *RepoFile {
	now := time.Now().UTC()
	if rf == nil {
		rf = &RepoFile{}
	}

	switch result.Action {
	case "create":
		post := Post{
			ID:        newPostID(),
			Title:     result.Title,
			Content:   result.Content,
			Upvotes:   result.Upvotes,
			CreatedAt: now,
			UpdatedAt: now,
			Revision:  1,
			Commits:   newCommits,
		}
		// Prepend (newest first)
		rf.Posts = append([]Post{post}, rf.Posts...)

	case "update":
		for i, p := range rf.Posts {
			if p.ID == result.PostID {
				rf.Posts[i].Title = result.Title
				rf.Posts[i].Content = result.Content
				rf.Posts[i].Upvotes = result.Upvotes
				rf.Posts[i].UpdatedAt = now
				rf.Posts[i].Revision++
				rf.Posts[i].Commits = append(rf.Posts[i].Commits, newCommits...)
				break
			}
		}
	}

	return rf
}

// stripMarkdownCodeBlock removes markdown code block wrappers.
func stripMarkdownCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "```json"))
		s = strings.TrimSpace(strings.TrimPrefix(s, "```"))
		s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	}
	return s
}

// extractJSONObject finds and extracts a JSON object from a string.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
	// Embed config.Config behavior for oneshot execution
}

// GenerateRepoPosts generates subreddit posts for a single repo.
// It handles both bootstrap (no existing posts) and incremental (update existing) cases.
func GenerateRepoPosts(ctx context.Context, cfg FullConfig, repoName, repoSlug string, barePath, defaultBranch string, subredditDir string, worktreeBasePath string) error {
	target := cfg.GetSubredditTarget()
	if target == "" {
		return ErrDisabled
	}

	checkingRange := cfg.GetSubredditCheckingRange()
	maxPosts := cfg.GetSubredditMaxPosts()
	maxAge := cfg.GetSubredditMaxAge()

	// Read existing repo file
	path := RepoFilePath(subredditDir, repoSlug)
	rf, err := ReadRepoFile(path)
	if err != nil {
		return fmt.Errorf("read repo file: %w", err)
	}

	// Gather commits with SHA
	commits, err := GatherRepoCommitsWithSHA(ctx, worktreeBasePath+"/"+barePath, repoName, defaultBranch, checkingRange)
	if err != nil {
		return fmt.Errorf("gather commits: %w", err)
	}

	if len(commits) == 0 {
		return nil // No new commits, nothing to do
	}

	// Determine if bootstrap or incremental
	isBootstrap := rf == nil || len(rf.Posts) == 0

	var newRF *RepoFile
	if isBootstrap {
		newRF, err = generateBootstrap(ctx, cfg, target, repoName, commits, maxPosts, maxAge)
	} else {
		newRF, err = generateIncremental(ctx, cfg, target, repoName, commits, rf, maxPosts, maxAge)
	}
	if err != nil {
		return err
	}

	// Write updated file
	if newRF == nil {
		return nil // No changes needed
	}
	newRF.Repo = repoName
	if err := WriteRepoFile(path, newRF); err != nil {
		return fmt.Errorf("write repo file: %w", err)
	}

	return nil
}

// generateBootstrap creates initial posts from a batch of commits.
func generateBootstrap(ctx context.Context, cfg FullConfig, target, repoName string, commits []PostCommit, maxPosts, maxAge int) (*RepoFile, error) {
	prompt := BuildBootstrapPrompt(commits, maxAge)

	// Type assert to *config.Config for oneshot execution
	cfgPtr, ok := any(cfg).(*config.Config)
	if !ok {
		return nil, fmt.Errorf("invalid config type for oneshot execution")
	}

	fullPrompt := BootstrapSystemPrompt + "\n\n" + prompt
	response, err := oneshot.ExecuteTarget(ctx, cfgPtr, target, fullPrompt, "", DefaultTimeout, "")
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	bootstrapPosts, err := ParseBootstrapResult(response)
	if err != nil {
		return nil, fmt.Errorf("parse bootstrap response: %w", err)
	}

	now := time.Now().UTC()
	rf := &RepoFile{Repo: repoName}

	// Convert bootstrap posts to stored posts
	for _, bp := range bootstrapPosts {
		// Find commit subjects for this post's SHAs
		var postCommits []PostCommit
		shaSet := make(map[string]bool)
		for _, sha := range bp.CommitSHAs {
			shaSet[sha] = true
		}
		for _, c := range commits {
			if shaSet[c.SHA] {
				postCommits = append(postCommits, c)
			}
		}

		post := Post{
			ID:        newPostID(),
			Title:     bp.Title,
			Content:   bp.Content,
			Upvotes:   bp.Upvotes,
			CreatedAt: now,
			UpdatedAt: now,
			Revision:  1,
			Commits:   postCommits,
		}
		rf.Posts = append(rf.Posts, post)
	}

	rf.Posts = CleanupPosts(rf.Posts, maxPosts, maxAge)
	return rf, nil
}

// generateIncremental updates existing posts with new commits.
func generateIncremental(ctx context.Context, cfg FullConfig, target, repoName string, commits []PostCommit, existing *RepoFile, maxPosts, maxAge int) (*RepoFile, error) {
	// Build set of already-processed SHAs
	processedSHAs := make(map[string]bool)
	for _, p := range existing.Posts {
		for _, c := range p.Commits {
			processedSHAs[c.SHA] = true
		}
	}

	// Filter to only new commits
	var newCommits []PostCommit
	for _, c := range commits {
		if !processedSHAs[c.SHA] {
			newCommits = append(newCommits, c)
		}
	}

	if len(newCommits) == 0 {
		return nil, nil // No new commits
	}

	// Get recent posts within checking range for update candidates
	var recentPosts []Post
	cutoff := time.Now().UTC().Add(-time.Duration(48) * time.Hour) // Use checking range default
	for _, p := range existing.Posts {
		if p.CreatedAt.After(cutoff) {
			recentPosts = append(recentPosts, p)
		}
	}

	prompt := BuildIncrementalPrompt(newCommits, recentPosts)

	// Type assert to *config.Config for oneshot execution
	cfgPtr, ok := any(cfg).(*config.Config)
	if !ok {
		return nil, fmt.Errorf("invalid config type for oneshot execution")
	}

	fullPrompt := IncrementalSystemPrompt + "\n\n" + prompt
	response, err := oneshot.ExecuteTarget(ctx, cfgPtr, target, fullPrompt, "", DefaultTimeout, "")
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	result, err := ParseIncrementalResult(response)
	if err != nil {
		return nil, fmt.Errorf("parse incremental response: %w", err)
	}

	// Apply result to existing file
	updated := ApplyIncrementalResult(existing, result, newCommits)
	updated.Posts = CleanupPosts(updated.Posts, maxPosts, maxAge)

	return updated, nil
}

// GatherRepoCommitsWithSHA gathers commits with SHA for a single repo.
// This is used for incremental subreddit generation.
func GatherRepoCommitsWithSHA(ctx context.Context, bareDir, repoName, defaultBranch string, hours int) ([]PostCommit, error) {
	since := fmt.Sprintf("%d.hours.ago", hours)
	branch := defaultBranch
	if branch == "" {
		branch = "main"
	}

	cmd := exec.CommandContext(ctx, "git", "log",
		"--since="+since,
		"--pretty=format:%H %s",
		"origin/"+branch,
	)
	cmd.Dir = bareDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var commits []PostCommit
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "SHA subject"
		if len(line) < 41 {
			continue
		}
		sha := line[:40]
		subject := strings.TrimSpace(line[41:])
		commits = append(commits, PostCommit{
			SHA:     sha,
			Subject: subject,
		})
	}

	return commits, nil
}
