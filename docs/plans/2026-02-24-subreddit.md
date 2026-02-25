# Subreddit Digest Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a subreddit-style digest that summarizes recent commits across all configured repos from an enthusiastic user's perspective.

**Architecture:** Create a new `internal/subreddit/` package with schema registration and LLM generation. The daemon's existing background loop calls generation hourly. A new API endpoint serves the cached result to the home page.

**Tech Stack:** Go, oneshot LLM execution, JSON schema, React/TypeScript frontend

---

## Task 1: Add SubredditConfig to config package

**Files:**

- Modify: `internal/config/config.go`
- Modify: `internal/config/models.go`
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestGetSubredditTarget(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: "",
		},
		{
			name: "no subreddit config",
			cfg:  &Config{},
			want: "",
		},
		{
			name: "empty target",
			cfg:  &Config{Subreddit: &SubredditConfig{}},
			want: "",
		},
		{
			name: "target set",
			cfg:  &Config{Subreddit: &SubredditConfig{Target: "sonnet"}},
			want: "sonnet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetSubredditTarget(); got != tt.want {
				t.Errorf("GetSubredditTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetSubredditHours(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: 24,
		},
		{
			name: "no subreddit config",
			cfg:  &Config{},
			want: 24,
		},
		{
			name: "zero hours uses default",
			cfg:  &Config{Subreddit: &SubredditConfig{Hours: 0}},
			want: 24,
		},
		{
			name: "custom hours",
			cfg:  &Config{Subreddit: &SubredditConfig{Hours: 48}},
			want: 48,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetSubredditHours(); got != tt.want {
				t.Errorf("GetSubredditHours() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestGetSubreddit -v`
Expected: FAIL with "cfg.GetSubredditTarget undefined" or similar

**Step 3: Add SubredditConfig struct**

Add to `internal/config/models.go` (after other config structs):

```go
// SubredditConfig represents configuration for the subreddit digest feature.
type SubredditConfig struct {
	Target string `json:"target,omitempty"` // LLM target for generation, empty = disabled
	Hours  int    `json:"hours,omitempty"`  // Lookback window in hours, default 24
}
```

**Step 4: Add Subreddit field to Config struct**

In `internal/config/config.go`, add to the Config struct (around line 91, after other config fields):

```go
	Subreddit                   *SubredditConfig       `json:"subreddit,omitempty"`
```

**Step 5: Add getter methods**

Add to `internal/config/config.go` (after GetNudgenikTarget):

```go
// GetSubredditTarget returns the configured subreddit target name, if any.
func (c *Config) GetSubredditTarget() string {
	if c == nil || c.Subreddit == nil {
		return ""
	}
	return c.Subreddit.Target
}

// GetSubredditHours returns the lookback window in hours, defaulting to 24.
func (c *Config) GetSubredditHours() int {
	if c == nil || c.Subreddit == nil || c.Subreddit.Hours <= 0 {
		return 24
	}
	return c.Subreddit.Hours
}
```

**Step 6: Run test to verify it passes**

Run: `go test ./internal/config/... -run TestGetSubreddit -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/config/config.go internal/config/models.go internal/config/config_test.go
/commit
```

---

## Task 2: Create subreddit package with schema registration

**Files:**

- Create: `internal/subreddit/subreddit.go`
- Create: `internal/subreddit/subreddit_test.go`

**Step 1: Write the failing test**

Create `internal/subreddit/subreddit_test.go`:

````go
package subreddit

import (
	"testing"
)

func TestParseResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Result
		wantErr bool
	}{
		{
			name:  "valid json",
			input: `{"content": "Big day for schmux!"}`,
			want:  Result{Content: "Big day for schmux!"},
		},
		{
			name:  "json in code block",
			input: "```json\n{\"content\": \"Hello world\"}\n```",
			want:  Result{Content: "Hello world"},
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   "not json at all",
			wantErr: true,
		},
		{
			name:    "missing content field",
			input:   `{"other": "field"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResult(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseResult() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Content != tt.want.Content {
				t.Errorf("ParseResult().Content = %v, want %v", got.Content, tt.want.Content)
			}
		})
	}
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  subredditConfig
		want bool
	}{
		{
			name: "empty target",
			cfg:  subredditConfig{target: ""},
			want: false,
		},
		{
			name: "target set",
			cfg:  subredditConfig{target: "sonnet"},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEnabled(tt.cfg); got != tt.want {
				t.Errorf("isEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// subredditConfig is a minimal interface for testing
type subredditConfig interface {
	GetSubredditTarget() string
}

// mockConfig implements subredditConfig for testing
type mockConfig struct {
	target string
}

func (m mockConfig) GetSubredditTarget() string {
	return m.target
}

func isEnabled(cfg subredditConfig) bool {
	return cfg.GetSubredditTarget() != ""
}
````

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create the subreddit package**

Create `internal/subreddit/subreddit.go`:

````go
package subreddit

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

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

Write a casual subreddit-style post summarizing the recent changes. You're sharing what's new with fellow users — not as a developer, but as someone who uses the product daily and is excited about improvements.

Guidelines:
- Conversational tone, like talking to peers who also use the tool
- Focus on what users would care about: new features, quality-of-life fixes, bugs squashed
- Light opinions are fine ("finally fixed that annoying bug", "solid quality-of-life win")
- Don't name authors or get technical about implementation
- Keep it concise — a few paragraphs at most
- If there are no commits, write a brief "quiet period" message

Here are the commits from the last {{HOURS}} hours:

{{COMMITS}}
`
)

var (
	ErrDisabled       = errors.New("subreddit digest is disabled")
	ErrNoCommits      = errors.New("no commits to summarize")
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
````

**Step 4: Add schema label**

In `internal/schema/schema.go`, add to the const block:

```go
	LabelSubreddit = "subreddit"
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/subreddit/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/subreddit/subreddit.go internal/subreddit/subreddit_test.go internal/schema/schema.go
/commit
```

---

## Task 3: Add commit gathering and generation logic

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Modify: `internal/subreddit/subreddit_test.go`

**Step 1: Write the failing test**

Add to `internal/subreddit/subreddit_test.go`:

```go
func TestBuildPrompt(t *testing.T) {
	commits := []CommitInfo{
		{Repo: "schmux", Subject: "feat: add new feature"},
		{Repo: "schmux", Subject: "fix: resolve bug"},
		{Repo: "other", Subject: "docs: update readme"},
	}

	prompt := BuildPrompt(commits, 24)

	if !strings.Contains(prompt, "add new feature") {
		t.Error("prompt should contain first commit subject")
	}
	if !strings.Contains(prompt, "resolve bug") {
		t.Error("prompt should contain second commit subject")
	}
	if !strings.Contains(prompt, "other") {
		t.Error("prompt should contain repo name")
	}
	if !strings.Contains(prompt, "24 hours") {
		t.Error("prompt should contain hours value")
	}
}

func TestFormatCommits(t *testing.T) {
	commits := []CommitInfo{
		{Repo: "schmux", Subject: "feat: add feature"},
		{Repo: "other", Subject: "fix: bug fix"},
	}

	got := formatCommits(commits)

	if !strings.Contains(got, "[schmux] feat: add feature") {
		t.Errorf("formatted commits should include repo prefix, got: %s", got)
	}
	if !strings.Contains(got, "[other] fix: bug fix") {
		t.Errorf("formatted commits should include repo prefix, got: %s", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/... -run TestBuild -v`
Expected: FAIL with undefined errors

**Step 3: Add CommitInfo struct and helper functions**

Add to `internal/subreddit/subreddit.go`:

```go
import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	var lines []string
	for _, c := range commits {
		lines = append(lines, fmt.Sprintf("[%s] %s", c.Repo, c.Subject))
	}
	return strings.Join(lines, "\n")
}

// GatherCommits collects commits from all configured repos within the time window.
// Uses the bare clones in ~/.schmux/query/ for efficient querying.
func GatherCommits(ctx context.Context, queryPath string, repos []RepoInfo, hours int, getDefaultBranch func(repoURL string) string) ([]CommitInfo, error) {
	var allCommits []CommitInfo

	since := fmt.Sprintf("%d hours ago", hours)

	for _, repo := range repos {
		if repo.BarePath == "" {
			continue
		}

		repoPath := filepath.Join(queryPath, repo.BarePath)
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			continue
		}

		defaultBranch := getDefaultBranch(repo.URL)
		if defaultBranch == "" {
			defaultBranch = "main"
		}

		commits, err := getCommitsFromBare(ctx, repoPath, repo.Name, defaultBranch, since)
		if err != nil {
			continue // Skip repos with errors
		}
		allCommits = append(allCommits, commits...)
	}

	return allCommits, nil
}

// getCommitsFromBare runs git log against a bare clone.
func getCommitsFromBare(ctx context.Context, repoPath, repoName, defaultBranch, since string) ([]CommitInfo, error) {
	ref := fmt.Sprintf("origin/%s", defaultBranch)
	cmd := exec.CommandContext(ctx, "git", "log", "--format=%s", "--since="+since, ref)
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var commits []CommitInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
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

// RepoInfo provides the minimal info needed to gather commits.
type RepoInfo struct {
	Name     string
	URL      string
	BarePath string
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/subreddit/... -run TestBuild -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/subreddit/subreddit.go internal/subreddit/subreddit_test.go
/commit
```

---

## Task 4: Add cache file handling

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Modify: `internal/subreddit/subreddit_test.go`

**Step 1: Write the failing test**

Add to `internal/subreddit/subreddit_test.go`:

```go
import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "subreddit.json")

	cache := Cache{
		Content:     "Test content",
		GeneratedAt: time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC),
		Hours:       24,
		CommitCount: 5,
	}

	// Write
	if err := WriteCache(cachePath, cache); err != nil {
		t.Fatalf("WriteCache() error = %v", err)
	}

	// Read
	got, err := ReadCache(cachePath)
	if err != nil {
		t.Fatalf("ReadCache() error = %v", err)
	}

	if got.Content != cache.Content {
		t.Errorf("Content = %v, want %v", got.Content, cache.Content)
	}
	if got.Hours != cache.Hours {
		t.Errorf("Hours = %v, want %v", got.Hours, cache.Hours)
	}
	if got.CommitCount != cache.CommitCount {
		t.Errorf("CommitCount = %v, want %v", got.CommitCount, cache.CommitCount)
	}
}

func TestReadCacheNotFound(t *testing.T) {
	_, err := ReadCache("/nonexistent/path/subreddit.json")
	if err == nil {
		t.Error("ReadCache() should return error for missing file")
	}
}

func TestCacheAge(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		generated time.Time
		maxAge    time.Duration
		wantStale bool
	}{
		{
			name:      "fresh cache",
			generated: now.Add(-30 * time.Minute),
			maxAge:    time.Hour,
			wantStale: false,
		},
		{
			name:      "stale cache",
			generated: now.Add(-2 * time.Hour),
			maxAge:    time.Hour,
			wantStale: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := Cache{GeneratedAt: tt.generated}
			if got := cache.IsStale(tt.maxAge); got != tt.wantStale {
				t.Errorf("IsStale() = %v, want %v", got, tt.wantStale)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/... -run TestCache -v`
Expected: FAIL with undefined errors

**Step 3: Add Cache struct and functions**

Add to `internal/subreddit/subreddit.go`:

```go
import (
	"encoding/json"
	"os"
	"time"
)

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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/subreddit/... -run TestCache -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/subreddit/subreddit.go internal/subreddit/subreddit_test.go
/commit
```

---

## Task 5: Add Generate function with LLM integration

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Modify: `internal/subreddit/subreddit_test.go`

**Step 1: Write the failing test**

Add to `internal/subreddit/subreddit_test.go`:

```go
func TestGenerateDisabled(t *testing.T) {
	cfg := mockConfig{target: ""}
	_, err := Generate(context.Background(), cfg, nil, nil, "", 24)
	if !errors.Is(err, ErrDisabled) {
		t.Errorf("Generate() error = %v, want ErrDisabled", err)
	}
}

func TestGenerateNoCommits(t *testing.T) {
	cfg := mockConfig{target: "sonnet"}
	gatherer := func(ctx context.Context, queryPath string, repos []RepoInfo, hours int, getDefaultBranch func(repoURL string) string) ([]CommitInfo, error) {
		return nil, nil
	}
	_, err := Generate(context.Background(), cfg, gatherer, nil, "", 24)
	// With no commits, we still generate (LLM writes "quiet period" message)
	// This test just verifies the function doesn't crash
	if err == ErrDisabled {
		t.Error("should not return ErrDisabled when target is set")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/... -run TestGenerate -v`
Expected: FAIL with undefined errors

**Step 3: Add Generate function**

Add to `internal/subreddit/subreddit.go`:

```go
import (
	"context"

	"github.com/sergeknystautas/schmux/internal/oneshot"
)

// Config provides the minimal interface needed for generation.
type Config interface {
	GetSubredditTarget() string
	GetSubredditHours() int
}

// WorkspaceManager provides repo and default branch info.
type WorkspaceManager interface {
	GetRepos() []RepoInfo
	GetDefaultBranch(ctx context.Context, repoURL string) string
	GetQueryRepoPath() string
}

// Generate creates a new subreddit digest.
func Generate(ctx context.Context, cfg Config, gatherFunc func(ctx context.Context, queryPath string, repos []RepoInfo, hours int, getDefaultBranch func(repoURL string) string) ([]CommitInfo, error), wm WorkspaceManager, cachePath string, hours int) (Cache, error) {
	target := cfg.GetSubredditTarget()
	if target == "" {
		return Cache{}, ErrDisabled
	}

	// Use configured hours or override
	if hours <= 0 {
		hours = cfg.GetSubredditHours()
	}

	// Gather commits
	var commits []CommitInfo
	var err error
	if gatherFunc != nil && wm != nil {
		commits, err = gatherFunc(ctx, wm.GetQueryRepoPath(), wm.GetRepos(), hours, func(repoURL string) string {
			branch, _ := wm.GetDefaultBranch(ctx, repoURL)
			return branch
		})
		if err != nil {
			return Cache{}, fmt.Errorf("gather commits: %w", err)
		}
	}

	// Build prompt and call LLM
	prompt := BuildPrompt(commits, hours)
	timeout := DefaultTimeout

	response, err := oneshot.ExecuteTarget(ctx, nil, target, prompt, schema.LabelSubreddit, timeout, "")
	if err != nil {
		return Cache{}, fmt.Errorf("LLM execution: %w", err)
	}

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

	// Write cache if path provided
	if cachePath != "" {
		if err := WriteCache(cachePath, cache); err != nil {
			// Log but don't fail - cache is best effort
		}
	}

	return cache, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/subreddit/... -run TestGenerate -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/subreddit/subreddit.go internal/subreddit/subreddit_test.go
/commit
```

---

## Task 6: Add API endpoint

**Files:**

- Modify: `internal/dashboard/server.go`
- Create: `internal/dashboard/handlers_subreddit.go`
- Test: `internal/dashboard/handlers_test.go`

**Step 1: Write the failing test**

Add to `internal/dashboard/handlers_test.go`:

```go
func TestHandleSubreddit(t *testing.T) {
	tests := []struct {
		name       string
		config     *config.Config
		wantStatus int
		wantBody   string
	}{
		{
			name:       "disabled - no config",
			config:     &config.Config{},
			wantStatus: http.StatusOK,
			wantBody:   `"enabled":false`,
		},
		{
			name:       "disabled - empty target",
			config:     &config.Config{Subreddit: &config.SubredditConfig{}},
			wantStatus: http.StatusOK,
			wantBody:   `"enabled":false`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{config: tt.config}
			req := httptest.NewRequest("GET", "/api/subreddit", nil)
			w := httptest.NewRecorder()

			s.handleSubreddit(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if !strings.Contains(w.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want to contain %s", w.Body.String(), tt.wantBody)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/... -run TestHandleSubreddit -v`
Expected: FAIL with undefined errors

**Step 3: Create handler file**

Create `internal/dashboard/handlers_subreddit.go`:

```go
package dashboard

import (
	"encoding/json"
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
```

**Step 4: Register route in server.go**

In `internal/dashboard/server.go`, add to the route registrations (around line 465):

```go
	mux.HandleFunc("/api/subreddit", s.withCORS(s.withAuth(s.handleSubreddit)))
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/dashboard/... -run TestHandleSubreddit -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/dashboard/server.go internal/dashboard/handlers_subreddit.go internal/dashboard/handlers_test.go
/commit
```

---

## Task 7: Add daemon integration for hourly generation

**Files:**

- Modify: `internal/daemon/daemon.go`

**Step 1: Add subreddit generation to background loop**

In `internal/daemon/daemon.go`, find the background goroutine that calls `FetchOriginQueries` (around line 912). Add subreddit generation after `BroadcastSessions()`:

```go
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(d.shutdownCtx, cfg.GitStatusTimeout())
				// Ensure origin query repos exist (in case new repos were added)
				if err := wm.EnsureOriginQueries(ctx); err != nil {
					logger.Warn("failed to ensure origin queries", "err", err)
				}
				// Fetch origin query repos to get latest branch info
				wm.FetchOriginQueries(ctx)
				wm.UpdateAllGitStatus(ctx)
				cancel()
				server.BroadcastSessions()

				// Generate subreddit digest if enabled (hourly)
				if subreddit.IsEnabled(cfg) {
					go d.generateSubredditDigest()
				}
```

**Step 2: Add generateSubredditDigest method**

Add to `internal/daemon/daemon.go`:

```go
// generateSubredditDigest generates a new subreddit digest in the background.
func (d *Daemon) generateSubredditDigest() {
	ctx, cancel := context.WithTimeout(d.shutdownCtx, 60*time.Second)
	defer cancel()

	cfg := d.cfg
	wm := d.workspaceManager

	// Get cache path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cachePath := filepath.Join(homeDir, ".schmux", "subreddit.json")

	// Check if cache is fresh (less than 1 hour old)
	if cache, err := subreddit.ReadCache(cachePath); err == nil {
		if !cache.IsStale(time.Hour) {
			return // Cache is still fresh
		}
	}

	// Generate new digest
	_, err = subreddit.Generate(ctx, cfg, subreddit.GatherCommits, &subredditWorkspaceManager{wm}, cachePath, 0)
	if err != nil {
		logger.Warn("failed to generate subreddit digest", "err", err)
	}
}

// subredditWorkspaceManager adapts workspace.Manager to the subreddit interface.
type subredditWorkspaceManager struct {
	*workspace.Manager
}

func (s *subredditWorkspaceManager) GetRepos() []subreddit.RepoInfo {
	// Get repos from config - this requires access to config
	// We'll need to pass config through or add a method to workspace.Manager
	return nil // TODO: implement
}

func (s *subredditWorkspaceManager) GetQueryRepoPath() string {
	return s.Manager.GetConfig().GetQueryRepoPath()
}
```

Note: This will require some refactoring to properly wire up the config access. The key insight is that we need to iterate over `cfg.GetRepos()` and convert them to `RepoInfo` structs.

**Step 3: Add import**

Add to imports in `internal/daemon/daemon.go`:

```go
	"github.com/sergeknystautas/schmux/internal/subreddit"
```

**Step 4: Run tests**

Run: `go test ./internal/daemon/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go
/commit
```

---

## Task 8: Add frontend API client and types

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts`
- Modify: `assets/dashboard/src/lib/types.ts`

**Step 1: Add type definition**

Add to `assets/dashboard/src/lib/types.ts`:

```typescript
export interface SubredditResponse {
  content: string;
  generated_at: string;
  hours: number;
  commit_count: number;
  enabled: boolean;
}
```

**Step 2: Add API function**

Add to `assets/dashboard/src/lib/api.ts`:

```typescript
export async function getSubreddit(): Promise<SubredditResponse> {
  const response = await fetch('/api/subreddit');
  if (!response.ok) {
    throw new Error(`Failed to fetch subreddit: ${response.statusText}`);
  }
  return response.json();
}
```

**Step 3: Run frontend tests**

Run: `./test.sh`
Expected: PASS

**Step 4: Commit**

```bash
git add assets/dashboard/src/lib/api.ts assets/dashboard/src/lib/types.ts
/commit
```

---

## Task 9: Add subreddit card to HomePage

**Files:**

- Modify: `assets/dashboard/src/routes/HomePage.tsx`
- Modify: `assets/dashboard/src/styles/home.module.css`

**Step 1: Add subreddit section to HomePage**

In `assets/dashboard/src/routes/HomePage.tsx`, add state and fetch:

```typescript
const [subreddit, setSubreddit] = useState<SubredditResponse | null>(null);
const [subredditLoading, setSubredditLoading] = useState(true);

// Fetch subreddit on mount
useEffect(() => {
  (async () => {
    setSubredditLoading(true);
    try {
      const data = await getSubreddit();
      setSubreddit(data);
    } catch (err) {
      console.debug('Failed to fetch subreddit:', err);
    } finally {
      setSubredditLoading(false);
    }
  })();
}, []);
```

**Step 2: Add subreddit card below Pull Requests**

Add after the Pull Requests section (around line 663):

```tsx
{
  /* Subreddit Digest Section */
}
{
  subreddit?.enabled && (
    <div className={styles.sectionCard}>
      <div className={styles.sectionHeader}>
        <h2 className={styles.sectionTitle}>
          <ChatIcon />
          What's New
        </h2>
      </div>
      <div className={styles.sectionContent}>
        {subredditLoading ? (
          <div className={styles.loadingState}>
            <div className="spinner spinner--small" />
            <span>Loading digest...</span>
          </div>
        ) : subreddit.content ? (
          <div className={styles.subredditContent}>
            <p>{subreddit.content}</p>
            <span className={styles.subredditMeta}>
              {subreddit.commit_count} commits • {subreddit.hours}h window •{' '}
              {formatRelativeDate(subreddit.generated_at)}
            </span>
          </div>
        ) : (
          <div className={styles.placeholderState}>
            <p className={styles.placeholderText}>No digest yet.</p>
            <p className={styles.placeholderHint}>Check back after the next hourly refresh.</p>
          </div>
        )}
      </div>
    </div>
  );
}
```

**Step 3: Add ChatIcon**

Add the icon component:

```tsx
const ChatIcon = () => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M2 3a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v7a1 1 0 0 1-1 1H5l-3 3V3z" />
  </svg>
);
```

**Step 4: Add CSS styles**

Add to `assets/dashboard/src/styles/home.module.css`:

```css
.subredditContent {
  padding: var(--spacing-sm) 0;
}

.subredditContent p {
  margin: 0;
  line-height: 1.6;
  color: var(--color-text);
}

.subredditMeta {
  display: block;
  margin-top: var(--spacing-sm);
  font-size: 0.75rem;
  color: var(--color-text-muted);
}
```

**Step 5: Run frontend tests**

Run: `./test.sh`
Expected: PASS

**Step 6: Commit**

```bash
git add assets/dashboard/src/routes/HomePage.tsx assets/dashboard/src/styles/home.module.css
/commit
```

---

## Task 10: Add config UI for subreddit settings

**Files:**

- Modify: `assets/dashboard/src/routes/config/AdvancedTab.tsx`

**Step 1: Add subreddit config section**

Add to the AdvancedTab component, after the existing config sections:

```tsx
{
  /* Subreddit Settings */
}
<div className={styles.configSection}>
  <h3>Subreddit Digest</h3>
  <p className={styles.sectionDescription}>
    Generate a casual summary of recent changes, written from an enthusiastic user's perspective.
  </p>

  <div className={styles.formGroup}>
    <label htmlFor="subreddit-target">LLM Target</label>
    <select
      id="subreddit-target"
      value={config.subreddit?.target || ''}
      onChange={(e) =>
        updateConfig('subreddit', {
          ...config.subreddit,
          target: e.target.value || undefined,
          hours: config.subreddit?.hours || 24,
        })
      }
    >
      <option value="">Disabled</option>
      {promptableTargets.map((t) => (
        <option key={t.name} value={t.name}>
          {t.name}
        </option>
      ))}
    </select>
    <span className={styles.hint}>Leave empty to disable the subreddit digest feature.</span>
  </div>

  <div className={styles.formGroup}>
    <label htmlFor="subreddit-hours">Lookback Window (hours)</label>
    <input
      id="subreddit-hours"
      type="number"
      min="1"
      max="168"
      value={config.subreddit?.hours || 24}
      onChange={(e) =>
        updateConfig('subreddit', {
          ...config.subreddit,
          target: config.subreddit?.target || '',
          hours: parseInt(e.target.value, 10) || 24,
        })
      }
    />
    <span className={styles.hint}>
      How many hours of commits to include in the digest. Default: 24.
    </span>
  </div>
</div>;
```

**Step 2: Update TypeScript types if needed**

Ensure `SubredditConfig` is defined in `types.ts` or generated types.

**Step 3: Run frontend tests**

Run: `./test.sh`
Expected: PASS

**Step 4: Commit**

```bash
git add assets/dashboard/src/routes/config/AdvancedTab.tsx
/commit
```

---

## Task 11: Update API documentation

**Files:**

- Modify: `docs/api.md`

**Step 1: Add subreddit endpoint documentation**

Add to `docs/api.md` in the API endpoints section:

````markdown
### GET /api/subreddit

Returns the cached subreddit digest.

**Response:**

```json
{
  "content": "Big day for schmux — the diff viewer finally landed...",
  "generated_at": "2026-02-24T10:00:00Z",
  "hours": 24,
  "commit_count": 12,
  "enabled": true
}
```
````

**Fields:**

- `content` (string): The digest narrative, or empty if not yet generated
- `generated_at` (string): ISO 8601 timestamp when digest was generated
- `hours` (int): Lookback window used for this digest
- `commit_count` (int): Number of commits included
- `enabled` (boolean): Whether the subreddit feature is configured

**Status Codes:**

- `200 OK`: Always returns 200, even when disabled or empty

````

**Step 2: Commit**

```bash
git add docs/api.md
/commit
````

---

## Task 12: Final verification and integration test

**Step 1: Run all tests**

Run: `./test.sh --all`
Expected: All tests pass

**Step 2: Manual verification**

1. Add subreddit config to `~/.schmux/config.json`:

   ```json
   {
     "subreddit": {
       "target": "sonnet",
       "hours": 24
     }
   }
   ```

2. Restart daemon: `./schmux stop && ./schmux start`

3. Wait for background loop to run (or trigger manually)

4. Check home page for subreddit card

5. Verify `/api/subreddit` returns content

**Step 3: Final commit if any fixes needed**

```bash
git add -A
/commit
```
