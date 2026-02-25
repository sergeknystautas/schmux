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
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelSubreddit, Result{})
}

const (
	// DefaultTimeout is the default timeout for LLM generation.
	DefaultTimeout = 30 * time.Second

	// Prompt is the subreddit digest prompt.
	Prompt = `You are an enthusiastic user of schmux, a multi-agent AI orchestration tool.

Write a casual subreddit-style post summarizing what just landed. You're sharing what's new with fellow users — not as a developer, but as someone who uses the product daily and is excited about improvements.

Guidelines:
- Conversational tone, like talking to peers who also use the tool
- Focus on what users would care about: new features, quality-of-life fixes, bugs squashed
- Light opinions are fine ("finally fixed that annoying bug", "solid quality-of-life win")
- Don't name authors or get technical about implementation
- Keep it concise — a few paragraphs at most
- DO NOT use phrases like "this week" or "weekly recap" — this is just the last {{HOURS}} hours
- If there are no commits, write a brief "quiet period" message

Here are the commits from the last {{HOURS}} hours:

{{COMMITS}}
`
)

var (
	// ErrDisabled is returned when the subreddit feature is not configured.
	ErrDisabled = errors.New("subreddit digest is disabled")
	// ErrInvalidResponse is returned when the LLM response cannot be parsed.
	ErrInvalidResponse = errors.New("invalid subreddit response")
)

// Result is the parsed subreddit digest response.
type Result struct {
	Content string `json:"content" required:"true"`
}

// IsEnabled returns true if the subreddit feature is enabled.
func IsEnabled(getter interface{ GetSubredditTarget() string }) bool {
	return getter.GetSubredditTarget() != ""
}

// ParseResult extracts and parses the Result from a raw LLM response.
func ParseResult(raw string) (Result, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Result{}, ErrInvalidResponse
	}

	// Strip markdown code blocks if present
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```json"))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
	}

	// Find JSON object bounds
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end == -1 || end <= start {
		return Result{}, ErrInvalidResponse
	}

	payload := trimmed[start : end+1]
	var result Result
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		// Try normalizing common issues
		payload = oneshot.NormalizeJSONPayload(payload)
		if payload == "" {
			return Result{}, ErrInvalidResponse
		}
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			return Result{}, ErrInvalidResponse
		}
	}

	// Validate required field
	if result.Content == "" {
		return Result{}, ErrInvalidResponse
	}

	return result, nil
}

// CommitInfo represents a single commit for the digest.
type CommitInfo struct {
	Repo    string
	Subject string
}

// BuildPrompt constructs the full prompt for the LLM.
func BuildPrompt(commits []CommitInfo, hours int) string {
	commitsStr := formatCommits(commits)
	prompt := strings.ReplaceAll(Prompt, "{{HOURS}}", fmt.Sprintf("%d", hours))
	prompt = strings.ReplaceAll(prompt, "{{COMMITS}}", commitsStr)
	return prompt
}

// formatCommits formats commits with repo prefixes for clarity.
func formatCommits(commits []CommitInfo) string {
	if len(commits) == 0 {
		return "(no commits in this period)"
	}
	var lines []string
	for _, c := range commits {
		lines = append(lines, "["+c.Repo+"] "+c.Subject)
	}
	return strings.Join(lines, "\n")
}

// Cache represents the cached subreddit digest.
type Cache struct {
	Content     string    `json:"content"`
	GeneratedAt time.Time `json:"generated_at"`
	Hours       int       `json:"hours"`
	CommitCount int       `json:"commit_count"`
}

// IsStale returns true if the cache is older than maxAge.
func (c Cache) IsStale(maxAge time.Duration) bool {
	return time.Since(c.GeneratedAt) > maxAge
}

// ReadCache loads the cache from disk. Returns error if file doesn't exist.
func ReadCache(path string) (Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Cache{}, err
	}

	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return Cache{}, fmt.Errorf("invalid cache file: %w", err)
	}

	return cache, nil
}

// WriteCache saves the cache to disk.
func WriteCache(path string, cache Cache) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Config provides the minimal interface needed for generation.
type Config interface {
	GetSubredditTarget() string
	GetSubredditHours() int
}

// RepoInfo contains the info needed to gather commits from a repo.
type RepoInfo struct {
	Name          string
	BarePath      string
	DefaultBranch string
}

// Generate creates a new subreddit digest.
// The fullCfg parameter must be a *config.Config for oneshot execution.
func Generate(ctx context.Context, cfg Config, fullCfg any, gatherFunc GatherFunc, repos []CommitInfo, cachePath string, hours int) (Cache, error) {
	target := cfg.GetSubredditTarget()
	if target == "" {
		return Cache{}, ErrDisabled
	}

	// Use configured hours or override
	if hours <= 0 {
		hours = cfg.GetSubredditHours()
	}

	// Gather commits if a gather function is provided
	var commits []CommitInfo
	if gatherFunc != nil {
		var err error
		commits, err = gatherFunc(hours)
		if err != nil {
			return Cache{}, fmt.Errorf("gather commits: %w", err)
		}
	} else {
		commits = repos
	}

	// Build prompt
	prompt := BuildPrompt(commits, hours)

	// Type assert fullCfg to *config.Config for oneshot
	cfgPtr, ok := fullCfg.(*config.Config)
	if !ok {
		return Cache{}, fmt.Errorf("invalid config type for oneshot execution")
	}

	// Call LLM via oneshot (use DefaultTimeout)
	response, err := oneshot.ExecuteTarget(ctx, cfgPtr, target, prompt, schema.LabelSubreddit, DefaultTimeout, "")
	if err != nil {
		return Cache{}, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse response
	result, err := ParseResult(response)
	if err != nil {
		return Cache{}, fmt.Errorf("parse response: %w", err)
	}

	cache := Cache{
		Content:     result.Content,
		GeneratedAt: time.Now(),
		Hours:       hours,
		CommitCount: len(commits),
	}

	return cache, nil
}

// GatherFunc is a function that gathers commits for the digest.
type GatherFunc func(hours int) ([]CommitInfo, error)

// GatherCommits collects commits from all configured repos over the given hours.
func GatherCommits(ctx context.Context, repos []RepoInfo, worktreeBasePath string, hours int) ([]CommitInfo, error) {
	var allCommits []CommitInfo

	for _, repo := range repos {
		if repo.BarePath == "" {
			continue
		}
		bareDir := worktreeBasePath + "/" + repo.BarePath
		commits, err := gatherRepoCommits(ctx, bareDir, repo.Name, repo.DefaultBranch, hours)
		if err != nil {
			// Log but continue - don't fail the whole digest for one repo
			continue
		}
		allCommits = append(allCommits, commits...)
	}

	return allCommits, nil
}

// gatherRepoCommits gets commits from a single bare repo.
func gatherRepoCommits(ctx context.Context, bareDir, repoName, defaultBranch string, hours int) ([]CommitInfo, error) {
	since := fmt.Sprintf("%d.hours.ago", hours)
	branch := defaultBranch
	if branch == "" {
		branch = "main"
	}

	cmd := exec.CommandContext(ctx, "git", "log",
		"--since="+since,
		"--pretty=format:%s",
		"origin/"+branch,
	)
	cmd.Dir = bareDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var commits []CommitInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		commits = append(commits, CommitInfo{
			Repo:    repoName,
			Subject: line,
		})
	}

	return commits, nil
}
