//go:build vendorlocked

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

func newBufLogger() (*log.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := log.NewWithOptions(buf, log.Options{Level: log.WarnLevel})
	return logger, buf
}

func TestWarnVendorLockedIgnoredKeys_AllKeys(t *testing.T) {
	enabled := true
	c := &Config{ConfigData: ConfigData{
		Network: &NetworkConfig{
			BindAddress:       "0.0.0.0",
			PublicBaseURL:     "https://example.com",
			DashboardHostname: "host.example.com",
			TLS:               &TLSConfig{CertPath: "/c", KeyPath: "/k"},
		},
		AccessControl: &AccessControlConfig{Enabled: true},
		RemoteAccess: &RemoteAccessConfig{
			Enabled:      &enabled,
			PasswordHash: "$2a$12$...",
		},
	}}
	logger, buf := newBufLogger()
	WarnVendorLockedIgnoredKeys(c, logger)
	out := buf.String()
	for _, key := range []string{
		"network.bind_address",
		"network.public_base_url",
		"network.dashboard_hostname",
		"network.tls.cert_path",
		"network.tls.key_path",
		"access_control.enabled",
		"remote_access.enabled",
		"remote_access.password_hash",
	} {
		if !strings.Contains(out, key) {
			t.Errorf("expected log to mention %q, got: %s", key, out)
		}
	}
}

func TestWarnVendorLockedIgnoredKeys_LoopbackBindIsClean(t *testing.T) {
	c := &Config{ConfigData: ConfigData{Network: &NetworkConfig{BindAddress: "127.0.0.1"}}}
	logger, buf := newBufLogger()
	WarnVendorLockedIgnoredKeys(c, logger)
	if buf.Len() > 0 {
		t.Fatalf("expected no warnings, got: %s", buf.String())
	}
}

func TestWarnVendorLockedIgnoredKeys_NilConfig(t *testing.T) {
	logger, _ := newBufLogger()
	WarnVendorLockedIgnoredKeys(nil, logger) // must not panic
}

func TestVendorLocked_LoadHostileConfig_DoesNotError(t *testing.T) {
	// The hostile config from end-to-end Step 21 used to make config.Load
	// fail because validators read struct fields directly instead of
	// through the locked getters. Verify load now succeeds and triggers
	// the warning logger.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	hostile := `{
	  "network": {
	    "bind_address": "0.0.0.0",
	    "public_base_url": "https://attacker.com",
	    "tls": {"cert_path": "/tmp/c", "key_path": "/tmp/k"}
	  },
	  "access_control": {"enabled": true, "provider": "github"},
	  "remote_access": {"enabled": true},
	  "workspace_path": "` + filepath.Join(tmpDir, "ws") + `",
	  "repos": [], "run_targets": [], "quick_launch": []
	}`
	if err := os.WriteFile(configPath, []byte(hostile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v (vendorlocked builds must accept hostile configs and ignore the locked fields)", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}
	// Sanity: locked getters return safe values.
	if cfg.GetBindAddress() != "127.0.0.1" {
		t.Errorf("GetBindAddress = %q, want 127.0.0.1", cfg.GetBindAddress())
	}
	if cfg.GetAuthEnabled() {
		t.Error("GetAuthEnabled = true, want false")
	}
	if cfg.GetRemoteAccessEnabled() {
		t.Error("GetRemoteAccessEnabled = true, want false")
	}
}
