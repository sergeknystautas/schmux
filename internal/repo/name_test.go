package repo

import (
	"strings"
	"testing"
)

func TestSanitizeRepoName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "dotted name", input: "lordbalto.games", want: "lordbalto-games"},
		{name: "multiple dots", input: "a.b.c", want: "a-b-c"},
		{name: "leading dot trimmed", input: ".github", want: "github"},
		{name: "leading dots trimmed", input: "..x", want: "x"},
		{name: "valid characters kept", input: "Plain_Name-2", want: "Plain_Name-2"},
		{name: "empty name", input: "", want: ""},
		{name: "slash", input: "a/b", want: "a-b"},
		{name: "backslash", input: `a\b`, want: "a-b"},
		{name: "path traversal", input: "../etc", want: "etc"},
		{name: "space and colon", input: "my repo:v2", want: "my-repo-v2"},
		{name: "quotes and shell characters", input: `a"b'c$d`, want: "a-b-c-d"},
		{name: "non-ASCII letter", input: "café", want: "caf"},
		{name: "trailing invalid trimmed", input: "foo.", want: "foo"},
		{name: "only invalid characters", input: "...", want: ""},
		{name: "longer than 128 cut", input: strings.Repeat("a", 130), want: strings.Repeat("a", 128)},
		{name: "cut lands on dash", input: strings.Repeat("a", 127) + ".b", want: strings.Repeat("a", 127)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeRepoName(tt.input); got != tt.want {
				t.Errorf("SanitizeRepoName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		existingNames []string
		want          string
	}{
		{
			name:          "basic HTTPS URL",
			url:           "https://github.com/anthropics/claude-code.git",
			existingNames: nil,
			want:          "claude-code",
		},
		{
			name:          "SSH URL",
			url:           "git@github.com:anthropics/claude-code.git",
			existingNames: nil,
			want:          "claude-code",
		},
		{
			name:          "no .git suffix",
			url:           "https://github.com/anthropics/claude-code",
			existingNames: nil,
			want:          "claude-code",
		},
		{
			name:          "collision adds owner prefix",
			url:           "https://github.com/bob/claude-code.git",
			existingNames: []string{"claude-code"},
			want:          "bob-claude-code",
		},
		{
			name:          "owner truncated to 6 chars",
			url:           "https://github.com/very-long-org-name/utils.git",
			existingNames: []string{"utils"},
			want:          "very-l-utils",
		},
		{
			name:          "owner prefix also collides, numeric suffix",
			url:           "https://github.com/alice/claude-code.git",
			existingNames: []string{"claude-code", "alice-claude-code"},
			want:          "alice-claude-code-2",
		},
		{
			name:          "numeric suffix increments",
			url:           "https://github.com/alice/claude-code.git",
			existingNames: []string{"claude-code", "alice-claude-code", "alice-claude-code-2"},
			want:          "alice-claude-code-3",
		},
		{
			name:          "no owner in URL, straight to numeric suffix",
			url:           "https://example.com/repo.git",
			existingNames: []string{"repo"},
			want:          "repo-2",
		},
		{
			name:          "uppercase in URL lowercased",
			url:           "https://github.com/Owner/MyRepo.git",
			existingNames: nil,
			want:          "myrepo",
		},
		{
			name:          "SSH with colon separator",
			url:           "git@gitlab.com:myorg/my-project.git",
			existingNames: nil,
			want:          "my-project",
		},
		{
			name:          "owner exactly 6 chars, no truncation",
			url:           "https://github.com/abcdef/utils.git",
			existingNames: []string{"utils"},
			want:          "abcdef-utils",
		},
		{
			name:          "SSH collision adds owner prefix",
			url:           "git@github.com:bob/claude-code.git",
			existingNames: []string{"claude-code"},
			want:          "bob-claude-code",
		},
		{
			name:          "dotted repo",
			url:           "https://github.com/lordbalto/lordbalto.games.git",
			existingNames: nil,
			want:          "lordbalto-games",
		},
		{
			name:          "leading dot trimmed",
			url:           "https://github.com/acme/.github.git",
			existingNames: nil,
			want:          "github",
		},
		{
			name:          "dotted owner prefix",
			url:           "https://github.com/lord.balto/utils.git",
			existingNames: []string{"utils"},
			want:          "lord-b-utils",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoNameFromURL(tt.url, tt.existingNames)
			if got != tt.want {
				t.Errorf("repoNameFromURL(%q, %v) = %q, want %q", tt.url, tt.existingNames, got, tt.want)
			}
		})
	}
}

func TestExtractRepoAndOwner(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantRepo  string
		wantOwner string
	}{
		{name: "HTTPS URL", url: "https://github.com/anthropics/claude-code.git", wantRepo: "claude-code", wantOwner: "anthropics"},
		{name: "SSH URL", url: "git@github.com:anthropics/claude-code.git", wantRepo: "claude-code", wantOwner: "anthropics"},
		{name: "single segment", url: "https://example.com/repo.git", wantRepo: "repo", wantOwner: ""},
		{name: "uppercased", url: "https://github.com/Owner/MyRepo.git", wantRepo: "myrepo", wantOwner: "owner"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, owner := extractRepoAndOwner(tt.url)
			if repo != tt.wantRepo || owner != tt.wantOwner {
				t.Errorf("extractRepoAndOwner(%q) = (%q, %q), want (%q, %q)", tt.url, repo, owner, tt.wantRepo, tt.wantOwner)
			}
		})
	}
}
