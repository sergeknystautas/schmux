package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

func TestNetworkWarningsInConfigResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		bindAddress  string
		tlsCert      string
		tlsKey       string
		authEnabled  bool
		wantWarnings []string
	}{
		{
			name:         "loopback has no warnings",
			bindAddress:  "127.0.0.1",
			wantWarnings: nil,
		},
		{
			name:        "network bind without TLS or auth",
			bindAddress: "0.0.0.0",
			wantWarnings: []string{
				"Dashboard is network-accessible without TLS. Traffic including terminal I/O is unencrypted.",
				"Dashboard is network-accessible without authentication. Anyone on your network can access terminal sessions.",
			},
		},
		{
			name:        "network bind with TLS but no auth",
			bindAddress: "0.0.0.0",
			tlsCert:     "/tmp/cert.pem",
			tlsKey:      "/tmp/key.pem",
			wantWarnings: []string{
				"Dashboard is network-accessible without authentication. Anyone on your network can access terminal sessions.",
			},
		},
		{
			name:         "network bind with TLS and auth",
			bindAddress:  "0.0.0.0",
			tlsCert:      "/tmp/cert.pem",
			tlsKey:       "/tmp/key.pem",
			authEnabled:  true,
			wantWarnings: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, cfg, _ := newTestServer(t)
			cfg.Network = &config.NetworkConfig{
				BindAddress: tt.bindAddress,
				Port:        7337,
			}
			if tt.tlsCert != "" {
				cfg.Network.TLS = &config.TLSConfig{
					CertPath: tt.tlsCert,
					KeyPath:  tt.tlsKey,
				}
			}
			if tt.authEnabled {
				cfg.AccessControl = &config.AccessControlConfig{
					Enabled:  true,
					Provider: "github",
				}
			}
			if err := cfg.Save(); err != nil {
				t.Fatalf("save config: %v", err)
			}

			handlers := newTestConfigHandlers(server)
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			handlers.handleConfigGet(rr, req)

			var resp contracts.ConfigResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if len(tt.wantWarnings) == 0 {
				if len(resp.NetworkWarnings) != 0 {
					t.Errorf("got warnings %v, want none", resp.NetworkWarnings)
				}
			} else {
				if len(resp.NetworkWarnings) != len(tt.wantWarnings) {
					t.Fatalf("got %d warnings, want %d: %v", len(resp.NetworkWarnings), len(tt.wantWarnings), resp.NetworkWarnings)
				}
				for i, w := range tt.wantWarnings {
					if resp.NetworkWarnings[i] != w {
						t.Errorf("warning[%d] = %q, want %q", i, resp.NetworkWarnings[i], w)
					}
				}
			}
		})
	}
}
