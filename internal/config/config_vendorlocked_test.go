//go:build vendorlocked

package config

import (
	"fmt"
	"testing"
)

func TestVendorLocked_GetBindAddress_AlwaysLoopback(t *testing.T) {
	c := &Config{ConfigData: ConfigData{Network: &NetworkConfig{BindAddress: "0.0.0.0"}}}
	if got := c.GetBindAddress(); got != "127.0.0.1" {
		t.Fatalf("GetBindAddress = %q, want 127.0.0.1", got)
	}
}

func TestVendorLocked_GetNetworkAccess_AlwaysFalse(t *testing.T) {
	c := &Config{ConfigData: ConfigData{Network: &NetworkConfig{BindAddress: "0.0.0.0"}}}
	if c.GetNetworkAccess() {
		t.Fatal("GetNetworkAccess = true, want false")
	}
}

func TestVendorLocked_GetPublicBaseURL_AlwaysLoopback(t *testing.T) {
	c := &Config{
		ConfigData: ConfigData{Network: &NetworkConfig{PublicBaseURL: "https://evil.com", Port: 7337}},
	}
	want := fmt.Sprintf("http://127.0.0.1:%d", c.GetPort())
	if got := c.GetPublicBaseURL(); got != want {
		t.Fatalf("GetPublicBaseURL = %q, want %q", got, want)
	}
}

func TestVendorLocked_GetTLSCertPath_AlwaysEmpty(t *testing.T) {
	c := &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "/etc/ssl/cert.pem"}}}}
	if got := c.GetTLSCertPath(); got != "" {
		t.Fatalf("GetTLSCertPath = %q, want empty", got)
	}
}

func TestVendorLocked_GetTLSKeyPath_AlwaysEmpty(t *testing.T) {
	c := &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{KeyPath: "/etc/ssl/key.pem"}}}}
	if got := c.GetTLSKeyPath(); got != "" {
		t.Fatalf("GetTLSKeyPath = %q, want empty", got)
	}
}

func TestVendorLocked_GetTLSEnabled_AlwaysFalse(t *testing.T) {
	c := &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{
		CertPath: "/etc/ssl/cert.pem", KeyPath: "/etc/ssl/key.pem",
	}}}}
	if c.GetTLSEnabled() {
		t.Fatal("GetTLSEnabled = true, want false")
	}
}

func TestVendorLocked_GetDashboardHostname_AlwaysEmpty(t *testing.T) {
	c := &Config{ConfigData: ConfigData{Network: &NetworkConfig{DashboardHostname: "example.com"}}}
	if got := c.GetDashboardHostname(); got != "" {
		t.Fatalf("GetDashboardHostname = %q, want empty", got)
	}
}

func TestVendorLocked_GetDashboardURL_UsesLocalhost(t *testing.T) {
	c := &Config{ConfigData: ConfigData{
		Network: &NetworkConfig{
			DashboardHostname: "evil.example.com",
			PublicBaseURL:     "https://attacker.com",
			Port:              7337,
		},
	}}
	want := fmt.Sprintf("http://127.0.0.1:%d", c.GetPort())
	if got := c.GetDashboardURL(); got != want {
		t.Fatalf("GetDashboardURL = %q, want %q", got, want)
	}
}

func TestVendorLocked_GetAuthEnabled_AlwaysFalse(t *testing.T) {
	c := &Config{ConfigData: ConfigData{
		AccessControl: &AccessControlConfig{Enabled: true},
	}}
	if c.GetAuthEnabled() {
		t.Fatal("GetAuthEnabled = true, want false")
	}
}

func TestVendorLocked_GetRemoteAccessEnabled_AlwaysFalse(t *testing.T) {
	enabled := true
	c := &Config{ConfigData: ConfigData{
		RemoteAccess: &RemoteAccessConfig{Enabled: &enabled},
	}}
	if c.GetRemoteAccessEnabled() {
		t.Fatal("GetRemoteAccessEnabled = true, want false")
	}
}
