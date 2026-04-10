package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestIsValidSocketName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"alphanumeric", "mySocket1", true},
		{"with hyphens", "my-socket", true},
		{"with underscores", "my_socket", true},
		{"mixed", "My-Socket_v2", true},
		{"empty", "", false},
		{"spaces", "my socket", false},
		{"dots", "my.socket", false},
		{"slashes", "my/socket", false},
		{"path traversal", "../etc/passwd", false},
		{"semicolon", "sock;rm -rf", false},
		{"unicode", "sock\u00e9t", false},
		{"single char", "a", true},
		{"numbers only", "12345", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidSocketName(tt.input); got != tt.want {
				t.Errorf("isValidSocketName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestReposEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []config.Repo
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []config.Repo{}, []config.Repo{}, true},
		{"same single", []config.Repo{{Name: "r1", URL: "u1"}}, []config.Repo{{Name: "r1", URL: "u1"}}, true},
		{"different length", []config.Repo{{Name: "r1"}}, []config.Repo{}, false},
		{"different name", []config.Repo{{Name: "r1", URL: "u1"}}, []config.Repo{{Name: "r2", URL: "u1"}}, false},
		{"different url", []config.Repo{{Name: "r1", URL: "u1"}}, []config.Repo{{Name: "r1", URL: "u2"}}, false},
		{"order matters", []config.Repo{{Name: "a"}, {Name: "b"}}, []config.Repo{{Name: "b"}, {Name: "a"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reposEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("reposEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
