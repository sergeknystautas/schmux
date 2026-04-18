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
			cfg:     &Config{ConfigData: ConfigData{Network: &NetworkConfig{}}},
			homeDir: "/home/user",
		},
		{
			name:    "empty home dir",
			cfg:     &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "~/cert.pem", KeyPath: "~/key.pem"}}}},
			homeDir: "",
		},
		{
			name:     "no tilde prefix",
			cfg:      &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "/etc/cert.pem", KeyPath: "/etc/key.pem"}}}},
			homeDir:  "/home/user",
			wantCert: "/etc/cert.pem",
			wantKey:  "/etc/key.pem",
		},
		{
			name:     "tilde expansion",
			cfg:      &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "~/certs/cert.pem", KeyPath: "~/certs/key.pem"}}}},
			homeDir:  "/home/user",
			wantCert: "/home/user/certs/cert.pem",
			wantKey:  "/home/user/certs/key.pem",
		},
		{
			name:     "only cert has tilde",
			cfg:      &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "~/cert.pem", KeyPath: "/abs/key.pem"}}}},
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
		{name: "nil enabled defaults true", cfg: &Config{ConfigData: ConfigData{Telemetry: &TelemetryConfig{}}}, want: true},
		{name: "explicitly true", cfg: &Config{ConfigData: ConfigData{Telemetry: &TelemetryConfig{Enabled: &trueVal}}}, want: true},
		{name: "explicitly false", cfg: &Config{ConfigData: ConfigData{Telemetry: &TelemetryConfig{Enabled: &falseVal}}}, want: false},
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
		want ShellCommand
	}{
		{name: "nil telemetry config", cfg: &Config{}, want: nil},
		{name: "nil command", cfg: &Config{ConfigData: ConfigData{Telemetry: &TelemetryConfig{Command: nil}}}, want: nil},
		{name: "set command", cfg: &Config{ConfigData: ConfigData{Telemetry: &TelemetryConfig{Command: ShellCommand{"my-telemetry-sink"}}}}, want: ShellCommand{"my-telemetry-sink"}},
		{name: "argv command", cfg: &Config{ConfigData: ConfigData{Telemetry: &TelemetryConfig{Command: ShellCommand{"jq", "-c", "."}}}}, want: ShellCommand{"jq", "-c", "."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetTelemetryCommand()
			if len(got) != len(tt.want) {
				t.Fatalf("GetTelemetryCommand() len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("GetTelemetryCommand()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
