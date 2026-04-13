package config

import (
	"testing"
)

func TestGetAuthEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{
			name: "nil AccessControl defaults to false",
			cfg:  &Config{},
			want: false,
		},
		{
			name: "explicit enabled",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{Enabled: true}}},
			want: true,
		},
		{
			name: "explicit disabled",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{Enabled: false}}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetAuthEnabled(); got != tt.want {
				t.Errorf("GetAuthEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAuthProvider(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "nil AccessControl returns empty",
			cfg:  &Config{},
			want: "",
		},
		{
			name: "empty provider defaults to github",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{}}},
			want: "github",
		},
		{
			name: "whitespace-only provider defaults to github",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{Provider: "   "}}},
			want: "github",
		},
		{
			name: "custom provider used",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{Provider: "oidc"}}},
			want: "oidc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetAuthProvider(); got != tt.want {
				t.Errorf("GetAuthProvider() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetAuthSessionTTLMinutes(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{
			name: "nil AccessControl defaults to 1440",
			cfg:  &Config{},
			want: DefaultAuthSessionTTLMinutes,
		},
		{
			name: "zero TTL defaults to 1440",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{SessionTTLMinutes: 0}}},
			want: DefaultAuthSessionTTLMinutes,
		},
		{
			name: "negative TTL defaults to 1440",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{SessionTTLMinutes: -1}}},
			want: DefaultAuthSessionTTLMinutes,
		},
		{
			name: "custom TTL used",
			cfg:  &Config{ConfigData: ConfigData{AccessControl: &AccessControlConfig{SessionTTLMinutes: 60}}},
			want: 60,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetAuthSessionTTLMinutes(); got != tt.want {
				t.Errorf("GetAuthSessionTTLMinutes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPreviewMaxPerWorkspace(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil Network", cfg: &Config{}, want: DefaultPreviewMaxPerWorkspace},
		{name: "zero value", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewMaxPerWorkspace: 0}}}, want: DefaultPreviewMaxPerWorkspace},
		{name: "negative", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewMaxPerWorkspace: -1}}}, want: DefaultPreviewMaxPerWorkspace},
		{name: "custom", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewMaxPerWorkspace: 10}}}, want: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetPreviewMaxPerWorkspace(); got != tt.want {
				t.Errorf("GetPreviewMaxPerWorkspace() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPreviewMaxGlobal(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil Network", cfg: &Config{}, want: DefaultPreviewMaxGlobal},
		{name: "zero value", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewMaxGlobal: 0}}}, want: DefaultPreviewMaxGlobal},
		{name: "custom", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewMaxGlobal: 50}}}, want: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetPreviewMaxGlobal(); got != tt.want {
				t.Errorf("GetPreviewMaxGlobal() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPreviewPortBase(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil Network", cfg: &Config{}, want: DefaultPreviewPortBase},
		{name: "zero value", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewPortBase: 0}}}, want: DefaultPreviewPortBase},
		{name: "custom", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewPortBase: 60000}}}, want: 60000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetPreviewPortBase(); got != tt.want {
				t.Errorf("GetPreviewPortBase() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPreviewPortBlockSize(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil Network", cfg: &Config{}, want: DefaultPreviewPortBlockSize},
		{name: "zero value", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewPortBlockSize: 0}}}, want: DefaultPreviewPortBlockSize},
		{name: "custom", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PreviewPortBlockSize: 5}}}, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetPreviewPortBlockSize(); got != tt.want {
				t.Errorf("GetPreviewPortBlockSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPublicBaseURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil Network", cfg: &Config{}, want: ""},
		{name: "empty URL", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PublicBaseURL: ""}}}, want: ""},
		{name: "whitespace URL trimmed", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PublicBaseURL: "  https://example.com  "}}}, want: "https://example.com"},
		{name: "set URL", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{PublicBaseURL: "https://my.domain.com"}}}, want: "https://my.domain.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetPublicBaseURL(); got != tt.want {
				t.Errorf("GetPublicBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetGitStatusWatchEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{name: "nil Sessions defaults to true", cfg: &Config{}, want: true},
		{name: "nil Enabled defaults to true", cfg: &Config{ConfigData: ConfigData{Sessions: &SessionsConfig{}}}, want: true},
		{name: "explicit true", cfg: &Config{ConfigData: ConfigData{Sessions: &SessionsConfig{GitStatusWatchEnabled: &trueVal}}}, want: true},
		{name: "explicit false", cfg: &Config{ConfigData: ConfigData{Sessions: &SessionsConfig{GitStatusWatchEnabled: &falseVal}}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetGitStatusWatchEnabled(); got != tt.want {
				t.Errorf("GetGitStatusWatchEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetGitStatusWatchDebounceMs(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil Sessions", cfg: &Config{}, want: DefaultGitStatusWatchDebounceMs},
		{name: "zero value", cfg: &Config{ConfigData: ConfigData{Sessions: &SessionsConfig{GitStatusWatchDebounceMs: 0}}}, want: DefaultGitStatusWatchDebounceMs},
		{name: "negative", cfg: &Config{ConfigData: ConfigData{Sessions: &SessionsConfig{GitStatusWatchDebounceMs: -1}}}, want: DefaultGitStatusWatchDebounceMs},
		{name: "custom", cfg: &Config{ConfigData: ConfigData{Sessions: &SessionsConfig{GitStatusWatchDebounceMs: 2000}}}, want: 2000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetGitStatusWatchDebounceMs(); got != tt.want {
				t.Errorf("GetGitStatusWatchDebounceMs() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetTLSEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{name: "nil Network", cfg: &Config{}, want: false},
		{name: "nil TLS", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{}}}, want: false},
		{name: "empty paths", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{}}}}, want: false},
		{name: "only cert", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "/cert.pem"}}}}, want: false},
		{name: "only key", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{KeyPath: "/key.pem"}}}}, want: false},
		{name: "both paths set", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "/cert.pem", KeyPath: "/key.pem"}}}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetTLSEnabled(); got != tt.want {
				t.Errorf("GetTLSEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTLSCertPath(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil Network", cfg: &Config{}, want: ""},
		{name: "nil TLS", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{}}}, want: ""},
		{name: "set", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{CertPath: "/etc/ssl/cert.pem"}}}}, want: "/etc/ssl/cert.pem"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetTLSCertPath(); got != tt.want {
				t.Errorf("GetTLSCertPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetTLSKeyPath(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil Network", cfg: &Config{}, want: ""},
		{name: "nil TLS", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{}}}, want: ""},
		{name: "set", cfg: &Config{ConfigData: ConfigData{Network: &NetworkConfig{TLS: &TLSConfig{KeyPath: "/etc/ssl/key.pem"}}}}, want: "/etc/ssl/key.pem"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetTLSKeyPath(); got != tt.want {
				t.Errorf("GetTLSKeyPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
