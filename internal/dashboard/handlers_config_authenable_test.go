package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func ptr[T any](v T) *T { return &v }

func postConfig(t *testing.T, h *ConfigHandlers, body contracts.ConfigUpdateRequest) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	h.handleConfigUpdate(rr, req)
	return rr
}

func TestEnableAuth_RejectedWithoutPrereqs_NoInMemoryEnable(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)
	// schmuxdir is set by newTestServer; ensure no GitHub creds exist.
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	rr := postConfig(t, h, contracts.ConfigUpdateRequest{
		AccessControl: &contracts.AccessControlUpdate{Enabled: ptr(true)},
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rr.Code, rr.Body.String())
	}
	if cfg.GetAuthEnabled() {
		t.Fatal("live config must NOT read auth-enabled after a rejected enable")
	}
}

func TestEnableAuth_RemovingTLSWhileEnabled_Rejected(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	dir := t.TempDir()
	schmuxdir.Set(dir)
	t.Cleanup(func() { schmuxdir.Set("") })
	// Dummy TLS cert/key fixtures; validation only stats the paths. testdata
	// rather than runtime writes keeps this runnable inside the fence sandbox,
	// which blocks writing .pem/.key files.
	cert := filepath.Join("testdata", "tls", "cert")
	key := filepath.Join("testdata", "tls", "key")
	_ = os.WriteFile(filepath.Join(dir, "secrets.json"),
		[]byte(`{"auth":{"github":{"client_id":"Ov23liabcdef","client_secret":"deadbeefdeadbeef"}}}`), 0o600)

	// Put the live config in a valid enabled state directly.
	cfg.AccessControl = &config.AccessControlConfig{Enabled: true, Provider: "github"}
	cfg.Network = &config.NetworkConfig{
		PublicBaseURL: "https://example.com:7337",
		TLS:           &config.TLSConfig{CertPath: cert, KeyPath: key},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Now clear TLS while auth stays enabled (what the HTTPS-uncheck autosave does).
	rr := postConfig(t, h, contracts.ConfigUpdateRequest{
		Network: &contracts.NetworkUpdate{
			TLS: &contracts.TLSUpdate{CertPath: ptr(""), KeyPath: ptr("")},
		},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 clearing TLS while auth on, got %d (%s)", rr.Code, rr.Body.String())
	}
}

func TestEnableAuth_UnchangedFullFormRepost_NotBlocked(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	h := newTestConfigHandlers(server)

	dir := t.TempDir()
	schmuxdir.Set(dir)
	t.Cleanup(func() { schmuxdir.Set("") })
	// Dummy TLS cert/key fixtures (see note above); testdata, not runtime writes,
	// so this runs inside the fence sandbox which blocks writing .pem/.key files.
	cert := filepath.Join("testdata", "tls", "cert")
	key := filepath.Join("testdata", "tls", "key")
	_ = os.WriteFile(filepath.Join(dir, "secrets.json"),
		[]byte(`{"auth":{"github":{"client_id":"Ov23liabcdef","client_secret":"deadbeefdeadbeef"}}}`), 0o600)

	cfg.AccessControl = &config.AccessControlConfig{Enabled: true, Provider: "github"}
	cfg.Network = &config.NetworkConfig{
		PublicBaseURL: "https://example.com:7337",
		TLS:           &config.TLSConfig{CertPath: cert, KeyPath: key},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Full-form re-post echoing the SAME effective values, plus one unrelated
	// change — exactly what the dashboard autosave sends. Must NOT be rejected.
	recycle := true
	pub := "https://example.com:7337"
	rr := postConfig(t, h, contracts.ConfigUpdateRequest{
		RecycleWorkspaces: &recycle,
		AccessControl:     &contracts.AccessControlUpdate{Enabled: ptr(true), Provider: ptr("github")},
		Network: &contracts.NetworkUpdate{
			PublicBaseURL: &pub,
			TLS:           &contracts.TLSUpdate{CertPath: &cert, KeyPath: &key},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("unchanged full-form re-post must succeed, got %d (%s)", rr.Code, rr.Body.String())
	}
}
