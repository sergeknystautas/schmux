package subreddit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		{
			name:    "empty content",
			input:   `{"content": ""}`,
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
		cfg  mockConfig
		want bool
	}{
		{
			name: "empty target",
			cfg:  mockConfig{target: ""},
			want: false,
		},
		{
			name: "target set",
			cfg:  mockConfig{target: "sonnet"},
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
	target string
	hours  int
}

func (m mockConfig) GetSubredditTarget() string {
	return m.target
}

func (m mockConfig) GetSubredditHours() int {
	if m.hours <= 0 {
		return 24
	}
	return m.hours
}

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
		{
			name:      "just under threshold",
			generated: now.Add(-time.Hour + time.Millisecond),
			maxAge:    time.Hour,
			wantStale: false,
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

func TestGenerateDisabled(t *testing.T) {
	cfg := mockConfig{target: ""}
	_, err := Generate(context.Background(), cfg, nil, nil, nil, "", 24)
	if !errors.Is(err, ErrDisabled) {
		t.Errorf("Generate() error = %v, want ErrDisabled", err)
	}
}

func TestGenerateWithCommits(t *testing.T) {
	// Note: This test verifies the disabled check and basic flow.
	// Full LLM integration is tested via daemon integration tests.
	// With nil fullCfg, the type assertion will fail,
	// so we test that the function properly propagates that error.
	cfg := mockConfig{target: "sonnet"}
	commits := []CommitInfo{
		{Repo: "test", Subject: "test commit"},
	}
	_, err := Generate(context.Background(), cfg, nil, nil, commits, "", 24)
	// Expect error because nil fullCfg fails type assertion
	if err == nil {
		t.Error("Generate() expected error with nil fullCfg, got nil")
	}
}
