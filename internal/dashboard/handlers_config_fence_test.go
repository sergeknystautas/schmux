package dashboard

import (
	"net/http"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

func TestConfigUpdateFenceMode(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		wantStatus int
		wantStored string // cfg.FenceMode (raw), after the update
		wantGetter string // cfg.GetFenceMode()
	}{
		{"disabled persists", config.FenceModeDisabled, http.StatusOK, "disabled", "disabled"},
		{"optional_on persists", config.FenceModeOptionalOn, http.StatusOK, "optional_on", "optional_on"},
		{"optional_off normalizes to empty", config.FenceModeOptionalOff, http.StatusOK, "", "optional_off"},
		{"unknown rejected", "bogus", http.StatusBadRequest, "", "optional_off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, cfg, _ := newTestServer(t)
			h := newTestConfigHandlers(server)

			rr := postConfig(t, h, contracts.ConfigUpdateRequest{FenceMode: ptr(tt.mode)})

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if cfg.FenceMode != tt.wantStored {
				t.Errorf("cfg.FenceMode = %q, want %q", cfg.FenceMode, tt.wantStored)
			}
			if cfg.GetFenceMode() != tt.wantGetter {
				t.Errorf("GetFenceMode() = %q, want %q", cfg.GetFenceMode(), tt.wantGetter)
			}
		})
	}
}

func TestConfigUpdateFenceCommit(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	if rr := postConfig(t, h, contracts.ConfigUpdateRequest{FenceCommit: ptr(true)}); rr.Code != http.StatusOK {
		t.Fatalf("enable status = %d (%s)", rr.Code, rr.Body.String())
	}
	if !cfg.FenceCommit {
		t.Fatal("cfg.FenceCommit = false after enabling, want true")
	}

	if rr := postConfig(t, h, contracts.ConfigUpdateRequest{FenceCommit: ptr(false)}); rr.Code != http.StatusOK {
		t.Fatalf("disable status = %d (%s)", rr.Code, rr.Body.String())
	}
	if cfg.FenceCommit {
		t.Fatal("cfg.FenceCommit = true after disabling, want false")
	}
}
