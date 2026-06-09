package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestEnsureSessionSecret_RegeneratesUndecodable(t *testing.T) {
	dir := t.TempDir()
	schmuxdir.Set(dir)
	t.Cleanup(func() { schmuxdir.Set("") })

	// Non-empty but NOT valid RawStdEncoding base64 (contains '!').
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"),
		[]byte(`{"auth":{"session_secret":"not!base64!"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureSessionSecret()
	if err != nil {
		t.Fatalf("EnsureSessionSecret: %v", err)
	}
	if _, derr := base64.RawStdEncoding.DecodeString(got); derr != nil {
		t.Fatalf("returned secret still undecodable: %q (%v)", got, derr)
	}
}

func TestEnsureSessionSecret_KeepsDecodable(t *testing.T) {
	dir := t.TempDir()
	schmuxdir.Set(dir)
	t.Cleanup(func() { schmuxdir.Set("") })

	valid := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"),
		[]byte(`{"auth":{"session_secret":"`+valid+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureSessionSecret()
	if err != nil {
		t.Fatalf("EnsureSessionSecret: %v", err)
	}
	if got != valid {
		t.Fatalf("expected existing valid secret kept, got %q", got)
	}
}
