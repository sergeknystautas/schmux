# Repo-Centric Subreddit Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restructure the subreddit feature from a single rolling digest into per-repository, post-based news feeds with create/update semantics and importance scoring.

**Architecture:** Replace monolithic `subreddit.json` with per-repo JSON files containing arrays of posts. Change the generation flow from "regenerate everything" to "detect new commits, create or update posts per-repo." Add a dedicated config tab replacing the Advanced tab fields.

**Tech Stack:** Go backend, React/TypeScript frontend, LLM via oneshot, Vitest for frontend tests

**Design doc:** `docs/specs/2026-03-06-subreddit-repo-centric-design.md`

---

## Task 1: Data Model — Post and RepoFile Structs

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Test: `internal/subreddit/subreddit_test.go`

**Step 1: Write failing test for Post JSON round-trip**

Add to `subreddit_test.go`:

```go
func TestPostJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	post := subreddit.Post{
		ID:        "post-1709712000",
		Title:     "New workspace switching",
		Content:   "Great new feature for switching workspaces",
		Upvotes:   3,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
		Commits: []subreddit.PostCommit{
			{SHA: "abc1234", Subject: "feat: add workspace switcher"},
		},
	}
	data, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got subreddit.Post
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != post.ID || got.Title != post.Title || got.Upvotes != post.Upvotes || got.Revision != post.Revision {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if len(got.Commits) != 1 || got.Commits[0].SHA != "abc1234" {
		t.Errorf("commits mismatch: got %+v", got.Commits)
	}
}

func TestRepoFileJSONRoundTrip(t *testing.T) {
	rf := subreddit.RepoFile{
		Repo: "schmux",
		Posts: []subreddit.Post{
			{ID: "post-1", Title: "Test", Content: "Body", Upvotes: 2, Revision: 1},
		},
	}
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got subreddit.RepoFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Repo != "schmux" || len(got.Posts) != 1 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/ -run "TestPostJSON|TestRepoFile" -v`
Expected: FAIL — `Post` and `RepoFile` types don't exist yet

**Step 3: Implement structs**

Add to `subreddit.go` (after existing structs around line 111):

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/subreddit/ -run "TestPostJSON|TestRepoFile" -v`
Expected: PASS

---

## Task 2: Storage — ReadRepoFile / WriteRepoFile

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Test: `internal/subreddit/subreddit_test.go`

**Step 1: Write failing test for read/write round-trip**

```go
func TestRepoFileReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-repo.json")

	original := &RepoFile{
		Repo: "test-repo",
		Posts: []Post{
			{ID: "p1", Title: "First", Content: "Body", Upvotes: 2, Revision: 1},
			{ID: "p2", Title: "Second", Content: "Body2", Upvotes: 4, Revision: 2},
		},
	}
	if err := WriteRepoFile(path, original); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadRepoFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Repo != "test-repo" || len(got.Posts) != 2 {
		t.Errorf("mismatch: got %+v", got)
	}
}

func TestReadRepoFileNotFound(t *testing.T) {
	rf, err := ReadRepoFile("/nonexistent/path.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rf != nil {
		t.Errorf("expected nil for missing file, got %+v", rf)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/ -run "TestRepoFileReadWrite|TestReadRepoFileNotFound" -v`
Expected: FAIL

**Step 3: Implement ReadRepoFile and WriteRepoFile**

Add to `subreddit.go`:

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/subreddit/ -run "TestRepoFileReadWrite|TestReadRepoFileNotFound" -v`
Expected: PASS

---

## Task 3: Storage — Post Cleanup (max posts, max age)

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Test: `internal/subreddit/subreddit_test.go`

**Step 1: Write failing test for cleanup**

```go
func TestCleanupPosts(t *testing.T) {
	now := time.Now().UTC()

	posts := []Post{
		{ID: "new1", Title: "Recent", CreatedAt: now.Add(-1 * time.Hour), Revision: 1},
		{ID: "new2", Title: "Also Recent", CreatedAt: now.Add(-2 * time.Hour), Revision: 1},
		{ID: "old", Title: "Expired", CreatedAt: now.Add(-15 * 24 * time.Hour), Revision: 1},
	}

	// Max age 14 days — should drop "old"
	result := CleanupPosts(posts, 30, 14)
	if len(result) != 2 {
		t.Fatalf("expected 2 posts after age cleanup, got %d", len(result))
	}

	// Max posts 1 — should keep only most recent
	result = CleanupPosts(posts[:2], 1, 14)
	if len(result) != 1 || result[0].ID != "new1" {
		t.Fatalf("expected newest post, got %+v", result)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/ -run TestCleanupPosts -v`
Expected: FAIL

**Step 3: Implement CleanupPosts**

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/subreddit/ -run TestCleanupPosts -v`
Expected: PASS

---

## Task 4: Config — Update SubredditConfig Struct and Getters

**Files:**

- Modify: `internal/config/config.go` (SubredditConfig struct ~line 279, getters ~line 905)
- Modify: `internal/api/contracts/config.go` (Subreddit/SubredditUpdate ~lines 314-323)
- Test: `internal/config/config_test.go`

**Step 1: Write failing tests for new getters**

Add tests for `GetSubredditInterval()`, `GetSubredditCheckingRange()`, `GetSubredditMaxPosts()`, `GetSubredditMaxAge()`, `GetSubredditRepoEnabled()`.

```go
func TestSubredditConfigDefaults(t *testing.T) {
	cfg := &Config{}
	if cfg.GetSubredditInterval() != 30 {
		t.Errorf("expected default interval 30, got %d", cfg.GetSubredditInterval())
	}
	if cfg.GetSubredditCheckingRange() != 48 {
		t.Errorf("expected default checking range 48, got %d", cfg.GetSubredditCheckingRange())
	}
	if cfg.GetSubredditMaxPosts() != 30 {
		t.Errorf("expected default max posts 30, got %d", cfg.GetSubredditMaxPosts())
	}
	if cfg.GetSubredditMaxAge() != 14 {
		t.Errorf("expected default max age 14, got %d", cfg.GetSubredditMaxAge())
	}
	// Default: repo enabled if not in map
	if !cfg.GetSubredditRepoEnabled("any-repo") {
		t.Error("expected repo enabled by default")
	}
}

func TestSubredditConfigCustomValues(t *testing.T) {
	cfg := &Config{
		Subreddit: &SubredditConfig{
			Target:        "sonnet",
			Interval:      60,
			CheckingRange: 72,
			MaxPosts:      50,
			MaxAge:        7,
			Repos:         map[string]bool{"my-repo": false, "other": true},
		},
	}
	if cfg.GetSubredditInterval() != 60 {
		t.Errorf("expected 60, got %d", cfg.GetSubredditInterval())
	}
	if cfg.GetSubredditRepoEnabled("my-repo") {
		t.Error("my-repo should be disabled")
	}
	if !cfg.GetSubredditRepoEnabled("other") {
		t.Error("other should be enabled")
	}
	if !cfg.GetSubredditRepoEnabled("unknown") {
		t.Error("unknown repos should default to enabled")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run "TestSubredditConfigDefaults|TestSubredditConfigCustomValues" -v`
Expected: FAIL

**Step 3: Update SubredditConfig struct and add getters**

In `config.go`, update struct (~line 279):

```go
type SubredditConfig struct {
	Target        string          `json:"target,omitempty"`
	Interval      int             `json:"interval,omitempty"`
	CheckingRange int             `json:"checking_range,omitempty"`
	MaxPosts      int             `json:"max_posts,omitempty"`
	MaxAge        int             `json:"max_age,omitempty"`
	Repos         map[string]bool `json:"repos,omitempty"`
}
```

Remove the `Hours` field. Add getters after existing subreddit getters (~line 923):

```go
func (c *Config) GetSubredditInterval() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit != nil && c.Subreddit.Interval > 0 {
		return c.Subreddit.Interval
	}
	return 30
}

func (c *Config) GetSubredditCheckingRange() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit != nil && c.Subreddit.CheckingRange > 0 {
		return c.Subreddit.CheckingRange
	}
	return 48
}

func (c *Config) GetSubredditMaxPosts() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit != nil && c.Subreddit.MaxPosts > 0 {
		return c.Subreddit.MaxPosts
	}
	return 30
}

func (c *Config) GetSubredditMaxAge() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit != nil && c.Subreddit.MaxAge > 0 {
		return c.Subreddit.MaxAge
	}
	return 14
}

func (c *Config) GetSubredditRepoEnabled(repoSlug string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil || c.Subreddit.Repos == nil {
		return true
	}
	enabled, exists := c.Subreddit.Repos[repoSlug]
	if !exists {
		return true
	}
	return enabled
}
```

Remove `GetSubredditHours()` getter. Find all callers and update (Task 8 will handle daemon; Task 9 will handle handlers).

**Step 4: Update API contracts**

In `internal/api/contracts/config.go`, update Subreddit structs (~lines 314-323):

```go
type Subreddit struct {
	Target        string          `json:"target"`
	Interval      int             `json:"interval"`
	CheckingRange int             `json:"checking_range"`
	MaxPosts      int             `json:"max_posts"`
	MaxAge        int             `json:"max_age"`
	Repos         map[string]bool `json:"repos"`
}

type SubredditUpdate struct {
	Target        *string          `json:"target,omitempty"`
	Interval      *int             `json:"interval,omitempty"`
	CheckingRange *int             `json:"checking_range,omitempty"`
	MaxPosts      *int             `json:"max_posts,omitempty"`
	MaxAge        *int             `json:"max_age,omitempty"`
	Repos         map[string]bool  `json:"repos,omitempty"`
}
```

**Step 5: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/config/ -run "TestSubredditConfig" -v`
Expected: PASS

**Step 7: Fix compilation errors from removed Hours**

Search for all references to `GetSubredditHours` and `Hours` in subreddit-related code. Update:

- `internal/dashboard/handlers_config.go`: Update config get/set to use new fields
- `internal/subreddit/subreddit.go`: Update Config interface (remove `GetSubredditHours()`)
- `internal/subreddit/subreddit_test.go`: Update mockConfig

The goal here is compilation only — behavioral changes come in later tasks.

**Step 8: Run full test suite**

Run: `./test.sh --quick`
Expected: PASS (some tests may need temporary stubs — fix compile errors only)

---

## Task 5: LLM Prompt — Incremental Generation (Create/Update)

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Test: `internal/subreddit/subreddit_test.go`

**Step 1: Write failing test for new prompt builder**

```go
func TestBuildIncrementalPrompt(t *testing.T) {
	commits := []PostCommit{
		{SHA: "abc1234", Subject: "feat: add workspace switcher"},
		{SHA: "def5678", Subject: "fix: switcher hover state"},
	}
	existingPosts := []Post{
		{ID: "post-1", Title: "Dev mode improvements", Content: "Summary of dev mode changes..."},
	}
	prompt := BuildIncrementalPrompt(commits, existingPosts)
	if !strings.Contains(prompt, "abc1234") {
		t.Error("prompt should contain commit SHA")
	}
	if !strings.Contains(prompt, "post-1") {
		t.Error("prompt should contain existing post ID")
	}
	if !strings.Contains(prompt, "Dev mode improvements") {
		t.Error("prompt should contain existing post title")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/ -run TestBuildIncrementalPrompt -v`
Expected: FAIL

**Step 3: Implement new prompt templates and builder**

Add new system and user prompt templates to `subreddit.go`. The system prompt keeps the Reddit voice. The user prompt includes:

- New commits with SHAs
- Existing recent posts (id, title, content summary) as update candidates
- Instructions to return JSON with `action: "create"` or `action: "update"`

```go
var IncrementalSystemPrompt = `You are an enthusiastic user on r/schmux sharing news about a software project.
Write in a conversational, user-focused tone. Be casual and opinionated.
Do not mention authors or implementation details. Focus on what changed and why it matters.

You will receive new commits and optionally existing recent posts.
Decide whether the new commits represent a NEW topic (create a new post) or extend an EXISTING topic (update that post).

Return a JSON object with these fields:
- "action": "create" or "update"
- "post_id": (only if action is "update") the ID of the post to update
- "title": short headline (max ~80 chars)
- "content": markdown body (2-4 paragraphs, conversational)
- "upvotes": importance score 0-5 (logarithmic: 0=trivial, 1=minor, 2=moderate, 3=significant, 4=major, 5=landmark)

When updating, rewrite the entire post to incorporate both old and new changes seamlessly.`

func BuildIncrementalPrompt(newCommits []PostCommit, existingPosts []Post) string {
	var b strings.Builder
	b.WriteString("## New Commits\n\n")
	for _, c := range newCommits {
		fmt.Fprintf(&b, "- [%s] %s\n", c.SHA[:7], c.Subject)
	}
	if len(existingPosts) > 0 {
		b.WriteString("\n## Existing Recent Posts (candidates for update)\n\n")
		for _, p := range existingPosts {
			fmt.Fprintf(&b, "### %s (id: %s)\n%s\n\n", p.Title, p.ID, truncate(p.Content, 300))
		}
	}
	return b.String()
}
```

**Step 4: Write failing test for incremental response parsing**

````go
func TestParseIncrementalResult(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		action string
		postID string
		title  string
	}{
		{
			name:   "create action",
			input:  `{"action":"create","title":"New feature","content":"Body text","upvotes":3}`,
			action: "create",
			title:  "New feature",
		},
		{
			name:   "update action",
			input:  `{"action":"update","post_id":"post-1","title":"Updated title","content":"New body","upvotes":4}`,
			action: "update",
			postID: "post-1",
			title:  "Updated title",
		},
		{
			name:   "wrapped in markdown",
			input:  "```json\n{\"action\":\"create\",\"title\":\"Test\",\"content\":\"Body\",\"upvotes\":1}\n```",
			action: "create",
			title:  "Test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseIncrementalResult(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if result.Action != tt.action {
				t.Errorf("action: got %q, want %q", result.Action, tt.action)
			}
			if result.PostID != tt.postID {
				t.Errorf("post_id: got %q, want %q", result.PostID, tt.postID)
			}
			if result.Title != tt.title {
				t.Errorf("title: got %q, want %q", result.Title, tt.title)
			}
		})
	}
}
````

**Step 5: Implement ParseIncrementalResult**

```go
type IncrementalResult struct {
	Action  string `json:"action"`
	PostID  string `json:"post_id,omitempty"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Upvotes int    `json:"upvotes"`
}

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
```

Refactor existing `ParseResult` helpers (`stripMarkdownCodeBlock`, `extractJSONObject`) into reusable internal functions if not already.

**Step 6: Run tests**

Run: `go test ./internal/subreddit/ -run "TestBuildIncremental|TestParseIncremental" -v`
Expected: PASS

---

## Task 6: LLM Prompt — Bootstrap Generation

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Test: `internal/subreddit/subreddit_test.go`

**Step 1: Write failing test for bootstrap prompt**

```go
func TestBuildBootstrapPrompt(t *testing.T) {
	commits := []PostCommit{
		{SHA: "aaa1111", Subject: "feat: initial workspace support"},
		{SHA: "bbb2222", Subject: "fix: workspace cleanup"},
		{SHA: "ccc3333", Subject: "feat: add preview manager"},
	}
	prompt := BuildBootstrapPrompt(commits, 14)
	if !strings.Contains(prompt, "14 days") {
		t.Error("should mention time window")
	}
	if !strings.Contains(prompt, "aaa1111") {
		t.Error("should contain commits")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/subreddit/ -run TestBuildBootstrapPrompt -v`
Expected: FAIL

**Step 3: Implement bootstrap prompt**

```go
var BootstrapSystemPrompt = `You are an enthusiastic user on r/schmux sharing news about a software project.
Write in a conversational, user-focused tone. Be casual and opinionated.
Do not mention authors or implementation details. Focus on what changed and why it matters.

You will receive a batch of commits from the past several days.
Group them into distinct posts by topic — related changes should be in the same post.
Return a JSON array of posts.

Each post must have:
- "title": short headline (max ~80 chars)
- "content": markdown body (2-4 paragraphs, conversational)
- "upvotes": importance score 0-5 (logarithmic: 0=trivial, 1=minor, 2=moderate, 3=significant, 4=major, 5=landmark)
- "commit_shas": array of SHA strings that belong to this post

Order posts from most recent topic to oldest.`

func BuildBootstrapPrompt(commits []PostCommit, maxAgeDays int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Commits from the past %d days\n\n", maxAgeDays)
	for _, c := range commits {
		fmt.Fprintf(&b, "- [%s] %s\n", c.SHA[:min(7, len(c.SHA))], c.Subject)
	}
	return b.String()
}
```

**Step 4: Write failing test for bootstrap response parsing**

```go
func TestParseBootstrapResult(t *testing.T) {
	input := `[
		{"title":"Workspace support","content":"Big changes...","upvotes":3,"commit_shas":["aaa1111","bbb2222"]},
		{"title":"Preview manager","content":"New preview...","upvotes":2,"commit_shas":["ccc3333"]}
	]`
	posts, err := ParseBootstrapResult(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	if posts[0].Title != "Workspace support" {
		t.Errorf("title: got %q", posts[0].Title)
	}
	if len(posts[0].CommitSHAs) != 2 {
		t.Errorf("commit count: got %d", len(posts[0].CommitSHAs))
	}
}
```

**Step 5: Implement ParseBootstrapResult**

```go
type BootstrapPost struct {
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Upvotes    int      `json:"upvotes"`
	CommitSHAs []string `json:"commit_shas"`
}

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
```

**Step 6: Run tests**

Run: `go test ./internal/subreddit/ -run "TestBuildBootstrap|TestParseBootstrap" -v`
Expected: PASS

---

## Task 7: Generation Flow — Per-Repo Create/Update Orchestrator

**Files:**

- Modify: `internal/subreddit/subreddit.go`
- Test: `internal/subreddit/subreddit_test.go`

This task creates the core orchestration function that, given new commits and existing posts for a single repo, calls the LLM and applies the result (create or update).

**Step 1: Write failing test for ApplyIncrementalResult (create)**

```go
func TestApplyIncrementalResult_Create(t *testing.T) {
	now := time.Now().UTC()
	existing := &RepoFile{Repo: "test", Posts: []Post{
		{ID: "old", Title: "Old post", CreatedAt: now.Add(-10 * time.Hour)},
	}}
	result := &IncrementalResult{
		Action:  "create",
		Title:   "New post",
		Content: "Body",
		Upvotes: 3,
	}
	commits := []PostCommit{{SHA: "abc", Subject: "feat: something"}}

	updated := ApplyIncrementalResult(existing, result, commits)
	if len(updated.Posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(updated.Posts))
	}
	// Newest first
	if updated.Posts[0].Title != "New post" {
		t.Errorf("newest post should be first, got %q", updated.Posts[0].Title)
	}
	if updated.Posts[0].Revision != 1 {
		t.Errorf("new post revision should be 1")
	}
}
```

**Step 2: Write failing test for ApplyIncrementalResult (update)**

```go
func TestApplyIncrementalResult_Update(t *testing.T) {
	now := time.Now().UTC()
	existing := &RepoFile{Repo: "test", Posts: []Post{
		{ID: "target", Title: "Original", Content: "Old body", Revision: 1,
			CreatedAt: now.Add(-5 * time.Hour), UpdatedAt: now.Add(-5 * time.Hour),
			Commits: []PostCommit{{SHA: "old1", Subject: "first commit"}}},
	}}
	result := &IncrementalResult{
		Action:  "update",
		PostID:  "target",
		Title:   "Revised title",
		Content: "New body",
		Upvotes: 4,
	}
	newCommits := []PostCommit{{SHA: "new1", Subject: "second commit"}}

	updated := ApplyIncrementalResult(existing, result, newCommits)
	if len(updated.Posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(updated.Posts))
	}
	post := updated.Posts[0]
	if post.Title != "Revised title" {
		t.Errorf("title not updated: %q", post.Title)
	}
	if post.Revision != 2 {
		t.Errorf("revision should be 2, got %d", post.Revision)
	}
	if len(post.Commits) != 2 {
		t.Errorf("should have 2 commits, got %d", len(post.Commits))
	}
	if post.UpdatedAt.Equal(post.CreatedAt) {
		t.Error("updated_at should differ from created_at")
	}
}
```

**Step 3: Run tests to verify they fail**

Run: `go test ./internal/subreddit/ -run "TestApplyIncrementalResult" -v`
Expected: FAIL

**Step 4: Implement ApplyIncrementalResult**

```go
func ApplyIncrementalResult(rf *RepoFile, result *IncrementalResult, newCommits []PostCommit) *RepoFile {
	now := time.Now().UTC()
	if rf == nil {
		rf = &RepoFile{}
	}

	switch result.Action {
	case "create":
		post := Post{
			ID:        fmt.Sprintf("post-%d", now.UnixMilli()),
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
```

**Step 5: Run tests**

Run: `go test ./internal/subreddit/ -run "TestApplyIncrementalResult" -v`
Expected: PASS

---

## Task 8: Daemon — Updated Scheduler and Generation Flow

**Files:**

- Modify: `internal/daemon/daemon.go` (~lines 61, 1147-1152, 1373-1478)
- Modify: `internal/dashboard/handlers_subreddit.go` (full rewrite of generation logic)
- Modify: `internal/subreddit/subreddit.go` (update Config interface)

This task rewires the daemon's background scheduler and generation pipeline. Since the daemon code is integration-heavy and hard to unit test in isolation, focus on getting it to compile and work end-to-end. The core logic (prompt building, parsing, applying results) is already tested in Tasks 5-7.

**Step 1: Update the subreddit Config interface**

In `subreddit.go`, update the `Config` interface (~line 176) to match new config shape:

```go
type Config interface {
	GetSubredditTarget() string
	GetSubredditInterval() int
	GetSubredditCheckingRange() int
	GetSubredditMaxPosts() int
	GetSubredditMaxAge() int
	GetSubredditRepoEnabled(repoSlug string) bool
}
```

Update `mockConfig` in tests accordingly.

**Step 2: Update daemon scheduler interval**

In `daemon.go`:

- Remove the fixed `subredditDigestInterval` constant (~line 61)
- In `startSubredditHourlyGenerator()`, read interval from config: `time.Duration(cfg.GetSubredditInterval()) * time.Minute`
- Update the timer to use the configurable interval

**Step 3: Rewrite generateSubredditDigest to per-repo flow**

In `handlers_subreddit.go`, rewrite `generateSubreddit()` (~line 80):

1. Get subreddit directory: `~/.schmux/subreddit/`
2. For each configured repo where `cfg.GetSubredditRepoEnabled(slug)`:
   a. Get repo file path: `subreddit.RepoFilePath(subredditDir, slug)`
   b. Read existing `RepoFile` (or nil if bootstrap needed)
   c. Gather new commits (commits not already in any post's commit list)
   d. If no new commits, skip
   e. If no existing file (bootstrap): call LLM with bootstrap prompt, parse array, write file
   f. If existing file (incremental): filter posts within checking range, call LLM with incremental prompt, apply result, cleanup, write file
3. Update next generation time

**Step 4: Update getSubredditCachePath to return directory**

Rename or add `getSubredditDir()` returning `~/.schmux/subreddit/`. The old single-file path is no longer used.

**Step 5: Handle migration — delete old subreddit.json**

At daemon startup, check if `~/.schmux/subreddit.json` exists and delete it (one-time migration).

**Step 6: Compile and verify**

Run: `go build ./cmd/schmux`
Expected: Compiles cleanly

Run: `./test.sh --quick`
Expected: PASS (some old subreddit tests may need updating)

---

## Task 9: API Handler — New Response Shape

**Files:**

- Modify: `internal/dashboard/handlers_subreddit.go` (~lines 13-44, 118-125)
- Modify: `internal/dashboard/server.go` (route registration)
- Modify: `internal/api/contracts/` (add new response types if needed)
- Test: `internal/dashboard/handlers_subreddit_test.go` (if exists, or create)

**Step 1: Define new API response types**

In `handlers_subreddit.go`, update the response struct:

```go
type subredditResponse struct {
	Enabled bool                  `json:"enabled"`
	Repos   []subredditRepoEntry `json:"repos"`
}

type subredditRepoEntry struct {
	Name  string              `json:"name"`
	Slug  string              `json:"slug"`
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
```

**Step 2: Rewrite handleSubreddit handler**

Update `handleSubreddit()` to:

1. Check if enabled
2. List all repo JSON files in `~/.schmux/subreddit/`
3. Read each, convert posts to API format (omitting commits)
4. Return the aggregated response sorted by repo name

**Step 3: Compile and verify**

Run: `go build ./cmd/schmux`
Run: `./test.sh --quick`
Expected: PASS

---

## Task 10: TypeScript Types — Update Frontend Types

**Files:**

- Modify: `assets/dashboard/src/lib/types.ts` (~lines 501-507)
- Run: `go run ./cmd/gen-types` (if contract types were added)

**Step 1: Update SubredditResponse type**

Replace the existing `SubredditResponse` in `types.ts`:

```typescript
export interface SubredditPost {
  id: string;
  title: string;
  content: string;
  upvotes: number;
  created_at: string;
  updated_at: string;
  revision: number;
}

export interface SubredditRepo {
  name: string;
  slug: string;
  posts: SubredditPost[];
}

export interface SubredditResponse {
  enabled: boolean;
  repos: SubredditRepo[];
}
```

**Step 2: Verify no TypeScript errors**

Run: `./test.sh --quick`
Expected: PASS (frontend tests may fail if they reference old type shape — fix in next tasks)

---

## Task 11: Frontend Config — New Subreddit Tab

**Files:**

- Create: `assets/dashboard/src/routes/config/SubredditTab.tsx`
- Modify: `assets/dashboard/src/routes/config/ConfigPage.tsx` (add tab)
- Modify: `assets/dashboard/src/routes/config/AdvancedTab.tsx` (remove subreddit section, ~lines 175-216)
- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts` (update form state)
- Test: existing config tests via `./test.sh --quick`

**Step 1: Update useConfigForm state**

In `useConfigForm.ts`, replace `subredditTarget`/`subredditHours` with:

```typescript
subredditTarget: string;
subredditInterval: number;
subredditCheckingRange: number;
subredditMaxPosts: number;
subredditMaxAge: number;
subredditRepos: Record<string, boolean>;
```

Update defaults (~line 302):

```typescript
subredditTarget: '',
subredditInterval: 30,
subredditCheckingRange: 48,
subredditMaxPosts: 30,
subredditMaxAge: 14,
subredditRepos: {},
```

Update `hasChanges()`, `snapshotConfig()`, and the config load logic in `ConfigPage.tsx`.

**Step 2: Remove subreddit from AdvancedTab**

In `AdvancedTab.tsx`, remove the "Subreddit Digest" section (~lines 175-216). Remove related props.

**Step 3: Create SubredditTab component**

Create `SubredditTab.tsx` with:

- LLM Target dropdown (same `TargetSelect` component used elsewhere)
- Per-repo checkbox list (iterate over configured repos from the config)
- Polling interval input (minutes, default 30)
- Checking range input (hours, default 48)
- Max posts input (default 30)
- Max age input (days, default 14)
- All numeric inputs disabled when target is empty

**Step 4: Add SubredditTab to ConfigPage**

In `ConfigPage.tsx`, add a new tab entry for "Subreddit" and render `SubredditTab` with appropriate props.

Update the config save request to include all new subreddit fields.

**Step 5: Run tests**

Run: `./test.sh --quick`
Expected: PASS

---

## Task 12: Frontend Config — Update Config Save Handler

**Files:**

- Modify: `internal/dashboard/handlers_config.go` (~lines 512-522, 158-161)

**Step 1: Update config GET response**

In `handlers_config.go`, update the subreddit section of the config GET handler (~line 158):

```go
Subreddit: contracts.Subreddit{
	Target:        s.config.GetSubredditTarget(),
	Interval:      s.config.GetSubredditInterval(),
	CheckingRange: s.config.GetSubredditCheckingRange(),
	MaxPosts:      s.config.GetSubredditMaxPosts(),
	MaxAge:        s.config.GetSubredditMaxAge(),
	Repos:         s.config.GetSubredditRepos(),
},
```

(Add `GetSubredditRepos()` getter to config if not yet present — returns the repos map.)

**Step 2: Update config PUT handler**

In `handlers_config.go`, update the subreddit save section (~line 512):

```go
if req.Subreddit != nil {
	if cfg.Subreddit == nil {
		cfg.Subreddit = &config.SubredditConfig{}
	}
	if req.Subreddit.Target != nil {
		cfg.Subreddit.Target = strings.TrimSpace(*req.Subreddit.Target)
	}
	if req.Subreddit.Interval != nil {
		cfg.Subreddit.Interval = *req.Subreddit.Interval
	}
	if req.Subreddit.CheckingRange != nil {
		cfg.Subreddit.CheckingRange = *req.Subreddit.CheckingRange
	}
	if req.Subreddit.MaxPosts != nil {
		cfg.Subreddit.MaxPosts = *req.Subreddit.MaxPosts
	}
	if req.Subreddit.MaxAge != nil {
		cfg.Subreddit.MaxAge = *req.Subreddit.MaxAge
	}
	if req.Subreddit.Repos != nil {
		cfg.Subreddit.Repos = req.Subreddit.Repos
	}
}
```

**Step 3: Compile and test**

Run: `go build ./cmd/schmux`
Run: `./test.sh --quick`
Expected: PASS

---

## Task 13: Frontend Home — Subreddit Card with Repo Tabs and Posts

**Files:**

- Modify: `assets/dashboard/src/routes/HomePage.tsx` (~lines 276, 302-312, 663-695)
- Possible extract: `assets/dashboard/src/components/SubredditCard.tsx` (if HomePage is too large)

**Step 1: Update data fetching**

The `getSubreddit()` API call already returns the new shape (Task 9). Update the state type:

```typescript
const [subreddit, setSubreddit] = useState<SubredditResponse | null>(null);
const [activeRepoTab, setActiveRepoTab] = useState<string>('');
```

Set `activeRepoTab` to first repo slug when data loads.

**Step 2: Build the tabbed subreddit card**

Replace the existing subreddit rendering block (~lines 663-695) with:

- Tab bar showing repo names, active tab highlighted
- Post list for the active repo tab
- Each post renders:
  - **Left column**: Upvote count displayed vertically (Reddit-style arrow + number)
  - **Title**: Bold, larger text
  - **Content**: Markdown rendered via `ReactMarkdown`
  - **Footer**: Relative timestamp. If `revision > 1`, show "Updated {revision-1}x . {relative updated_at}" with a subtle "updated" badge
- Empty state when no posts for selected repo
- Disabled state when subreddit is not enabled

**Step 3: Style the upvote display**

Add styles for the Reddit-style upvote indicator — a vertical element on the left of each post with the number and an upward arrow/chevron. Use existing color variables. Scale visual weight with upvote count (e.g., color intensity).

**Step 4: Run tests and verify visually**

Run: `./test.sh --quick`
Expected: PASS

For visual verification: `./dev.sh` and check http://localhost:7337

---

## Task 14: Frontend Tests — Update Existing Subreddit Tests

**Files:**

- Modify: Any existing test files that reference the old `SubredditResponse` shape
- Search: `grep -r "SubredditResponse\|subreddit\|r/schmux" assets/dashboard/src/ --include="*.test.*"`

**Step 1: Find and update all affected tests**

Search for tests referencing:

- `SubredditResponse` (old shape with `content`, `generated_at`, etc.)
- Mock data for subreddit API calls
- Assertions about the subreddit card rendering

Update all mock data to use the new shape (`repos` array with `posts`).

**Step 2: Add test for tab switching**

```typescript
test('switches between repo tabs', async () => {
  // Mock subreddit with 2 repos
  // Render HomePage
  // Assert first repo tab is active
  // Click second repo tab
  // Assert second repo's posts are shown
});
```

**Step 3: Add test for updated post badge**

```typescript
test('shows updated badge for revised posts', async () => {
  // Mock subreddit with a post where revision > 1
  // Assert "Updated" text is visible
});
```

**Step 4: Run tests**

Run: `./test.sh --quick`
Expected: PASS

---

## Task 15: WebSocket — Broadcast Subreddit Updates

**Files:**

- Modify: `internal/dashboard/server.go` (WebSocket broadcast)
- Modify: `internal/dashboard/handlers_subreddit.go` (trigger broadcast after generation)
- Modify: `assets/dashboard/src/routes/HomePage.tsx` (listen for subreddit updates)

**Step 1: Add subreddit update broadcast**

After successful generation in `generateSubreddit()`, broadcast a WebSocket message so the dashboard updates without manual refresh. Follow the existing broadcast pattern used for session/workspace updates.

Message type: `"subreddit_updated"` with repo slug as payload.

**Step 2: Listen on frontend**

In `HomePage.tsx`, add a WebSocket listener for `subreddit_updated` events that triggers a re-fetch of the subreddit API.

**Step 3: Test manually**

Run: `./dev.sh`, trigger a subreddit generation, verify the dashboard updates live.

---

## Task 16: Update API Documentation

**Files:**

- Modify: `docs/api.md`

**Step 1: Update the subreddit API section**

Document the new `GET /api/subreddit` response shape with per-repo posts. Document the updated config fields.

---

## Task 17: Final Integration Test

**Step 1: Run full test suite**

Run: `./test.sh --all`
Expected: PASS

**Step 2: Manual smoke test**

1. Start with `./dev.sh`
2. Open config, go to Subreddit tab
3. Set an LLM target, verify repo checkboxes appear
4. Save config
5. Verify subreddit generates and appears on home page with tabs
6. Verify posts show titles, content, upvote counts
7. Wait for or trigger another generation cycle
8. Verify posts update (revision badge) or new posts appear

**Step 3: Clean up any remaining references to old subreddit format**

Search for `subreddit.json`, `GetSubredditHours`, `subredditDigestInterval`, and any other remnants of the old implementation.

Run: `./test.sh --quick`
Expected: PASS

**Step 4: Commit all changes**

Use `/commit` to commit everything as a single commit:

`feat(subreddit): restructure to repo-centric posts with importance scoring`
