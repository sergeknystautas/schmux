package config

import (
	"fmt"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/buildflags"
)

// WarnVendorLockedIgnoredKeys emits one structured log line per
// access-related config key that the vendorlocked build will ignore.
// No-op in non-vendor builds. Called from daemon.Run immediately after
// MigrateModes succeeds and before any listener is opened. Re-emitted
// on every daemon start — never silenced. Matches the existing
// security.allow_insecure_modes warning pattern.
func WarnVendorLockedIgnoredKeys(c *Config, logger *log.Logger) {
	if !buildflags.VendorLocked || c == nil {
		return
	}
	var ignored []string
	if c.Network != nil {
		if c.Network.BindAddress != "" && c.Network.BindAddress != "127.0.0.1" {
			ignored = append(ignored, fmt.Sprintf("network.bind_address=%q", c.Network.BindAddress))
		}
		if c.Network.PublicBaseURL != "" {
			ignored = append(ignored, "network.public_base_url")
		}
		if c.Network.DashboardHostname != "" {
			ignored = append(ignored, "network.dashboard_hostname")
		}
		if c.Network.TLS != nil {
			if c.Network.TLS.CertPath != "" {
				ignored = append(ignored, "network.tls.cert_path")
			}
			if c.Network.TLS.KeyPath != "" {
				ignored = append(ignored, "network.tls.key_path")
			}
		}
	}
	if c.AccessControl != nil && c.AccessControl.Enabled {
		ignored = append(ignored, "access_control.enabled")
	}
	if c.RemoteAccess != nil {
		if c.RemoteAccess.Enabled != nil && *c.RemoteAccess.Enabled {
			ignored = append(ignored, "remote_access.enabled")
		}
		if c.RemoteAccess.PasswordHash != "" {
			ignored = append(ignored, "remote_access.password_hash")
		}
	}
	for _, k := range ignored {
		logger.Warn("vendor build: ignoring access setting (binary is locked to 127.0.0.1)", "key", k)
	}
}
