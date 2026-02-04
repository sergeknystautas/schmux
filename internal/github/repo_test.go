package github

import "testing"

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    RepoInfo
		wantErr bool
	}{
		{
			name: "SSH with .git",
			url:  "git@github.com:user/repo.git",
			want: RepoInfo{Owner: "user", Repo: "repo"},
		},
		{
			name: "SSH without .git",
			url:  "git@github.com:user/repo",
			want: RepoInfo{Owner: "user", Repo: "repo"},
		},
		{
			name: "HTTPS with .git",
			url:  "https://github.com/user/repo.git",
			want: RepoInfo{Owner: "user", Repo: "repo"},
		},
		{
			name: "HTTPS without .git",
			url:  "https://github.com/user/repo",
			want: RepoInfo{Owner: "user", Repo: "repo"},
		},
		{
			name:    "not GitHub",
			url:     "git@gitlab.com:user/repo.git",
			wantErr: true,
		},
		{
			name:    "local repo",
			url:     "local:myproject",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRepoURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRepoURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseRepoURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsGitHubURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"git@github.com:user/repo.git", true},
		{"https://github.com/user/repo.git", true},
		{"git@gitlab.com:user/repo.git", false},
		{"local:myproject", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := IsGitHubURL(tt.url); got != tt.want {
				t.Errorf("IsGitHubURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestRepoInfoAPIPath(t *testing.T) {
	info := RepoInfo{Owner: "user", Repo: "repo"}
	if got := info.APIPath(); got != "user/repo" {
		t.Errorf("APIPath() = %q, want %q", got, "user/repo")
	}
}
