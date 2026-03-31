package dashboard

import "testing"

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://github.com/user/repo.git", true},
		{"http://github.com/user/repo.git", true},
		{"git@github.com:user/repo.git", true},
		{"ssh://git@github.com/user/repo.git", true},
		{"git://github.com/user/repo.git", true},
		{"https://gitlab.com/user/repo", true},
		{"my-project", false},
		{"local:my-project", false},
		{"", false},
		{"https://", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isGitURL(tt.input); got != tt.want {
				t.Errorf("isGitURL(%q) = %v, want %v", tt.input, got, tt.want)
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
