//go:build !nosubreddit

package subreddit

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  mockConfig
		want bool
	}{
		{
			name: "disabled",
			cfg:  mockConfig{enabled: false, target: "sonnet"},
			want: false,
		},
		{
			name: "enabled",
			cfg:  mockConfig{enabled: true, target: "sonnet"},
			want: true,
		},
		{
			name: "enabled without target",
			cfg:  mockConfig{enabled: true, target: ""},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEnabled(tt.cfg); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// mockConfig implements the interface for testing
type mockConfig struct {
	enabled       bool
	target        string
	checkingRange int
}

func (m mockConfig) GetSubredditEnabled() bool {
	return m.enabled
}

func (m mockConfig) GetSubredditTarget() string {
	return m.target
}

func (m mockConfig) GetSubredditCheckingRange() int {
	if m.checkingRange <= 0 {
		return 48
	}
	return m.checkingRange
}

func (m mockConfig) GetSubredditInterval() int {
	return 30
}

func (m mockConfig) GetSubredditMaxPosts() int {
	return 30
}

func (m mockConfig) GetSubredditMaxAge() int {
	return 14
}

func (m mockConfig) GetSubredditRepoEnabled(repoSlug string) bool {
	return true
}

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

func TestNewPostID_Unique(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := newPostID()
		if !strings.HasPrefix(id, "post-") {
			t.Fatalf("expected ID to start with post-, got %q", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate ID generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}

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

func TestPostJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	post := Post{
		ID:        "post-1709712000",
		Title:     "New workspace switching",
		Content:   "Great new feature for switching workspaces",
		Upvotes:   3,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
		Commits: []PostCommit{
			{SHA: "abc1234", Subject: "feat: add workspace switcher"},
		},
	}
	data, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Post
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != post.ID || got.Title != post.Title || got.Upvotes != post.Upvotes || got.Revision != post.Revision {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if len(got.Commits) != 1 || got.Commits[0].SHA != "abc1234" {
		t.Errorf("commits mismatch: got %+v", got)
	}
}

func TestRepoFileJSONRoundTrip(t *testing.T) {
	rf := RepoFile{
		Repo: "schmux",
		Posts: []Post{
			{ID: "post-1", Title: "Test", Content: "Body", Upvotes: 2, Revision: 1},
		},
	}
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RepoFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Repo != "schmux" || len(got.Posts) != 1 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

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
