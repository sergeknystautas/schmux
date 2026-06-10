package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
)

// When GitHub OAuth is enabled and the request is unauthenticated, the app shell
// must be served (so the SPA can render its own sign-in gate) rather than
// redirected to /auth/login.
func TestRequireAuthOrRedirect_GitHubAuthUnauthenticated_ServesSPA(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	cfg.AccessControl = &config.AccessControlConfig{Enabled: true, Provider: "github", SessionTTLMinutes: 60}

	req := httptest.NewRequest(http.MethodGet, "/", nil) // no auth cookie
	rr := httptest.NewRecorder()

	proceed := server.requireAuthOrRedirect(rr, req)

	if !proceed {
		t.Fatal("expected requireAuthOrRedirect to return true (serve SPA) for unauthenticated GitHub-auth request")
	}
	if rr.Code == http.StatusFound {
		t.Fatalf("expected no redirect, got %d -> %q", rr.Code, rr.Header().Get("Location"))
	}
}
