package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// A global quick-launch preset must round-trip every field through
// PATCH /api/config then GET /api/config. persona_id, fence, and kind
// were lost by hand-written copy loops before the contract type became
// the single schema owner.
func TestConfigQuickLaunchRoundTrip(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)

	prompt := "review the diff"
	update := contracts.ConfigUpdateRequest{
		QuickLaunch: []contracts.QuickLaunch{{
			Name:      "review",
			Target:    "claude",
			Prompt:    &prompt,
			PersonaID: "reviewer",
			Fence:     true,
			Kind:      "chat",
		}},
	}
	raw, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("marshal update: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	configH.handleConfigUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body = %s", rr.Code, rr.Body.String())
	}

	getRR := httptest.NewRecorder()
	configH.handleConfigGet(getRR, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRR.Code, getRR.Body.String())
	}
	var got contracts.ConfigResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.QuickLaunch) != 1 {
		t.Fatalf("quick_launch length = %d, want 1", len(got.QuickLaunch))
	}
	ql := got.QuickLaunch[0]
	if ql.PersonaID != "reviewer" {
		t.Errorf("persona_id = %q, want %q", ql.PersonaID, "reviewer")
	}
	if !ql.Fence {
		t.Errorf("fence = false, want true")
	}
	if ql.Kind != "chat" {
		t.Errorf("kind = %q, want %q", ql.Kind, "chat")
	}
}

func TestMinFreeDiskSpace_ConfigAPI_DefaultsToZero(t *testing.T) {
	server, _, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	h.handleConfigGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d %s", rr.Code, rr.Body.String())
	}
	var resp contracts.ConfigResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.MinFreeDiskSpaceMiB != 0 {
		t.Errorf("default min_free_disk_space_mib = %d, want 0", resp.MinFreeDiskSpaceMiB)
	}
}

func TestMinFreeDiskSpace_ConfigAPI_PostSavesValue(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	value := int64(5120)
	rr := postConfig(t, h, contracts.ConfigUpdateRequest{MinFreeDiskSpaceMiB: &value})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", rr.Code, rr.Body.String())
	}
	if cfg.MinFreeDiskSpaceMiB != 5120 {
		t.Errorf("live config min_free_disk_space_mib = %d, want 5120", cfg.MinFreeDiskSpaceMiB)
	}
	// Reload from disk to verify persisted.
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if cfg.MinFreeDiskSpaceMiB != 5120 {
		t.Errorf("persisted min_free_disk_space_mib = %d, want 5120", cfg.MinFreeDiskSpaceMiB)
	}
}

func TestMinFreeDiskSpace_ConfigAPI_ExplicitZeroClearsValue(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	cfg.MinFreeDiskSpaceMiB = 5120
	if err := cfg.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	zero := int64(0)
	rr := postConfig(t, h, contracts.ConfigUpdateRequest{MinFreeDiskSpaceMiB: &zero})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST zero: %d %s", rr.Code, rr.Body.String())
	}
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if cfg.MinFreeDiskSpaceMiB != 0 {
		t.Errorf("after explicit zero, persisted = %d, want 0", cfg.MinFreeDiskSpaceMiB)
	}
}

func TestMinFreeDiskSpace_ConfigAPI_OmittedLeavesValueUnchanged(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	cfg.MinFreeDiskSpaceMiB = 5120
	if err := cfg.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	// Empty body must NOT touch the existing value.
	rr := postConfig(t, h, contracts.ConfigUpdateRequest{})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST empty: %d %s", rr.Code, rr.Body.String())
	}
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if cfg.MinFreeDiskSpaceMiB != 5120 {
		t.Errorf("after omitted, persisted = %d, want 5120 (unchanged)", cfg.MinFreeDiskSpaceMiB)
	}
}

func TestMinFreeDiskSpace_ConfigAPI_NegativeRejected(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	cfg.MinFreeDiskSpaceMiB = 0
	if err := cfg.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	neg := int64(-1)
	rr := postConfig(t, h, contracts.ConfigUpdateRequest{MinFreeDiskSpaceMiB: &neg})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST negative: %d %s", rr.Code, rr.Body.String())
	}
	if cfg.MinFreeDiskSpaceMiB != 0 {
		t.Errorf("live config mutated after rejected save: %d, want 0", cfg.MinFreeDiskSpaceMiB)
	}
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if cfg.MinFreeDiskSpaceMiB != 0 {
		t.Errorf("persisted after rejected save: %d, want 0", cfg.MinFreeDiskSpaceMiB)
	}
}
