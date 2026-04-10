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

func TestCloneNetwork(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		if got := cloneNetwork(nil); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("without TLS", func(t *testing.T) {
		src := &config.NetworkConfig{
			BindAddress:            "0.0.0.0",
			Port:                   8080,
			PublicBaseURL:          "https://example.com",
			PreviewMaxPerWorkspace: 3,
			PreviewMaxGlobal:       10,
			PreviewPortBase:        9000,
			PreviewPortBlockSize:   100,
			DashboardHostname:      "dash.local",
		}
		dst := cloneNetwork(src)

		if dst == src {
			t.Error("clone returned same pointer")
		}
		if dst.BindAddress != src.BindAddress {
			t.Errorf("BindAddress: got %q, want %q", dst.BindAddress, src.BindAddress)
		}
		if dst.Port != src.Port {
			t.Errorf("Port: got %d, want %d", dst.Port, src.Port)
		}

		// Mutate source, verify copy unaffected
		src.Port = 9999
		src.BindAddress = "MUTATED"
		if dst.Port == 9999 {
			t.Error("mutating source Port affected copy")
		}
		if dst.BindAddress == "MUTATED" {
			t.Error("mutating source BindAddress affected copy")
		}
	})

	t.Run("with TLS", func(t *testing.T) {
		src := &config.NetworkConfig{
			Port: 443,
			TLS: &config.TLSConfig{
				CertPath: "/etc/ssl/cert.pem",
				KeyPath:  "/etc/ssl/key.pem",
			},
		}
		dst := cloneNetwork(src)

		if dst.TLS == nil {
			t.Fatal("TLS is nil in copy")
		}
		if dst.TLS == src.TLS {
			t.Error("TLS pointer not deep-copied")
		}
		if dst.TLS.CertPath != "/etc/ssl/cert.pem" {
			t.Errorf("CertPath: got %q, want %q", dst.TLS.CertPath, "/etc/ssl/cert.pem")
		}

		// Mutate source TLS, verify copy unaffected
		src.TLS.CertPath = "MUTATED"
		if dst.TLS.CertPath == "MUTATED" {
			t.Error("mutating source TLS CertPath affected copy")
		}
	})

	t.Run("nil TLS", func(t *testing.T) {
		src := &config.NetworkConfig{Port: 8080, TLS: nil}
		dst := cloneNetwork(src)
		if dst.TLS != nil {
			t.Error("expected nil TLS in copy")
		}
	})
}

func TestCloneAccessControl(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		if got := cloneAccessControl(nil); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("populated struct", func(t *testing.T) {
		src := &config.AccessControlConfig{
			Enabled:           true,
			Provider:          "oauth",
			SessionTTLMinutes: 60,
		}
		dst := cloneAccessControl(src)

		if dst == src {
			t.Error("clone returned same pointer")
		}
		if dst.Enabled != true {
			t.Error("Enabled: got false, want true")
		}
		if dst.Provider != "oauth" {
			t.Errorf("Provider: got %q, want %q", dst.Provider, "oauth")
		}
		if dst.SessionTTLMinutes != 60 {
			t.Errorf("SessionTTLMinutes: got %d, want %d", dst.SessionTTLMinutes, 60)
		}

		// Mutate source, verify copy unaffected
		src.Enabled = false
		src.Provider = "MUTATED"
		src.SessionTTLMinutes = 999
		if !dst.Enabled {
			t.Error("mutating source Enabled affected copy")
		}
		if dst.Provider == "MUTATED" {
			t.Error("mutating source Provider affected copy")
		}
		if dst.SessionTTLMinutes == 999 {
			t.Error("mutating source SessionTTLMinutes affected copy")
		}
	})
}
