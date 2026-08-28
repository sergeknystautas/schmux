package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

// The API field is named "lore" for backward compatibility, but the autolearn
// config section is the single source of truth. An update must write the
// autolearn section and drop the legacy lore section so the two cannot drift.
func TestConfigUpdate_LoreWritesAutolearnAndDropsLegacySection(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	// Seed the pre-unification drift from real user configs: legacy lore
	// disabled, autolearn still enabled.
	loreEnabled := false
	cfg.Lore = &config.LoreConfig{Enabled: &loreEnabled}
	autolearnEnabled := true
	cfg.Autolearn = &config.AutolearnConfig{Enabled: &autolearnEnabled}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	rr := postConfig(t, h, contracts.ConfigUpdateRequest{
		Lore: &contracts.LoreUpdate{Enabled: ptr(false)},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	if cfg.Lore != nil {
		t.Errorf("legacy lore section should be dropped after update, got %+v", cfg.Lore)
	}
	if cfg.GetAutolearnEnabled() {
		t.Error("autolearn should be disabled after update")
	}

	// Round-trip through disk to prove the saved file changed, not just memory.
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload after save failed: %v", err)
	}
	if cfg.Lore != nil {
		t.Errorf("legacy lore section should stay dropped after reload, got %+v", cfg.Lore)
	}
	if cfg.GetAutolearnEnabled() {
		t.Error("autolearn should still be disabled after reload")
	}
}

// A config that only has the legacy lore section must still drive the API
// response, via the load-time Lore→Autolearn alias.
func TestConfigGet_LegacyLoreSectionStillHonored(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	loreEnabled := false
	cfg.Lore = &config.LoreConfig{Enabled: &loreEnabled, Target: "claude-opus-4-6"}
	cfg.Autolearn = nil
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Reload(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	h.handleConfigGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	var resp contracts.ConfigResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Lore.Enabled {
		t.Error("expected lore.enabled=false from legacy section")
	}
	if resp.Lore.LLMTarget != "claude-opus-4-6" {
		t.Errorf("expected llm_target from legacy section, got %q", resp.Lore.LLMTarget)
	}
}
