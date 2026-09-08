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
