package config

import (
	"testing"
)

func TestExpandNetworkPaths(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		homeDir  string
		wantCert string
		wantKey  string
	}{
		{
			name:    "nil network",
			cfg:     &Config{},
			homeDir: "/home/user",
		},
		{
			name:    "nil TLS",
			cfg:     &Config{Network: &NetworkConfig{}},
			homeDir: "/home/user",
		},
		{
			name:    "empty home dir",
			cfg:     &Config{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "~/cert.pem", KeyPath: "~/key.pem"}}},
			homeDir: "",
		},
		{
			name:     "no tilde prefix",
			cfg:      &Config{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "/etc/cert.pem", KeyPath: "/etc/key.pem"}}},
			homeDir:  "/home/user",
			wantCert: "/etc/cert.pem",
			wantKey:  "/etc/key.pem",
		},
		{
			name:     "tilde expansion",
			cfg:      &Config{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "~/certs/cert.pem", KeyPath: "~/certs/key.pem"}}},
			homeDir:  "/home/user",
			wantCert: "/home/user/certs/cert.pem",
			wantKey:  "/home/user/certs/key.pem",
		},
		{
			name:     "only cert has tilde",
			cfg:      &Config{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "~/cert.pem", KeyPath: "/abs/key.pem"}}},
			homeDir:  "/home/user",
			wantCert: "/home/user/cert.pem",
			wantKey:  "/abs/key.pem",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.expandNetworkPaths(tt.homeDir)
			if tt.wantCert != "" && tt.cfg.Network.TLS.CertPath != tt.wantCert {
				t.Errorf("CertPath = %q, want %q", tt.cfg.Network.TLS.CertPath, tt.wantCert)
			}
			if tt.wantKey != "" && tt.cfg.Network.TLS.KeyPath != tt.wantKey {
				t.Errorf("KeyPath = %q, want %q", tt.cfg.Network.TLS.KeyPath, tt.wantKey)
			}
		})
	}
}

func TestGetTelemetryEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{name: "nil telemetry stanza defaults true", cfg: &Config{}, want: true},
		{name: "nil enabled defaults true", cfg: &Config{Telemetry: &TelemetryConfig{}}, want: true},
		{name: "explicitly true", cfg: &Config{Telemetry: &TelemetryConfig{Enabled: &trueVal}}, want: true},
		{name: "explicitly false", cfg: &Config{Telemetry: &TelemetryConfig{Enabled: &falseVal}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetTelemetryEnabled(); got != tt.want {
				t.Errorf("GetTelemetryEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTelemetryCommand(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil telemetry config", cfg: &Config{}, want: ""},
		{name: "empty command", cfg: &Config{Telemetry: &TelemetryConfig{Command: ""}}, want: ""},
		{name: "set command", cfg: &Config{Telemetry: &TelemetryConfig{Command: "my-telemetry-sink"}}, want: "my-telemetry-sink"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetTelemetryCommand(); got != tt.want {
				t.Errorf("GetTelemetryCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}
