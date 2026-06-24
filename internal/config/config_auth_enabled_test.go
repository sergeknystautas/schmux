package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestValidateAuthEnabled(t *testing.T) {
	dir := t.TempDir()
	schmuxdir.Set(dir)
	t.Cleanup(func() { schmuxdir.Set("") })

	// Dummy TLS cert/key fixtures; ValidateAuthEnabled only stats the paths.
	// They live in testdata rather than being written at runtime so the suite
	// runs inside the fence sandbox, which blocks writing .pem/.key files.
	cert := filepath.Join("testdata", "tls", "cert")
	key := filepath.Join("testdata", "tls", "key")
	// secrets.json with valid GitHub creds
	writeFile(t, filepath.Join(dir, "secrets.json"),
		`{"auth":{"github":{"client_id":"Ov23liabcdef","client_secret":"deadbeefdeadbeefdeadbeef"}}}`)

	base := func() *Config {
		c := CreateDefault(filepath.Join(dir, "config.json"))
		c.AccessControl = &AccessControlConfig{Enabled: true, Provider: "github"}
		c.Network = &NetworkConfig{
			PublicBaseURL: "https://example.com:7337",
			TLS:           &TLSConfig{CertPath: cert, KeyPath: key},
		}
		return c
	}

	t.Run("valid enabled config passes", func(t *testing.T) {
		if err := base().ValidateAuthEnabled(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
	t.Run("missing public_base_url fails", func(t *testing.T) {
		c := base()
		c.Network.PublicBaseURL = ""
		if err := c.ValidateAuthEnabled(); err == nil {
			t.Fatal("expected error for missing public_base_url")
		}
	})
	t.Run("disabled config passes regardless", func(t *testing.T) {
		c := base()
		c.AccessControl.Enabled = false
		c.Network.TLS = nil
		if err := c.ValidateAuthEnabled(); err != nil {
			t.Fatalf("expected nil for disabled, got %v", err)
		}
	})
}
