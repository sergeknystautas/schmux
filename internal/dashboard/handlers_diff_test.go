package dashboard

// Note: TestHandleDiff_RejectsNonGitWorkspace was removed — the diff handler
// is now VCS-agnostic via CommandBuilder and works with any VCS type.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/state"
)

// TestServeWorkspaceFile_AlwaysNoCache pins that /api/file/{id}/{path} returns
// Cache-Control: no-cache for both markdown and image responses. Without an
// explicit Cache-Control the browser applies heuristic freshness to the
// Last-Modified header http.ServeFile sets and serves stale bytes from its
// HTTP cache on subsequent requests — including after a reload.
func TestServeWorkspaceFile_AlwaysNoCache(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)

	workspacePath := filepath.Join(t.TempDir(), "ws-cache")
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(workspacePath, "hello.md"), []byte("# hi\n"), 0644); err != nil {
		t.Fatalf("write md: %v", err)
	}
	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if err := os.WriteFile(filepath.Join(workspacePath, "pic.png"), pngBytes, 0644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "page.html"), []byte("<h1>hello</h1>\n"), 0644); err != nil {
		t.Fatalf("write html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "style.css"), []byte("body { color: red; }\n"), 0644); err != nil {
		t.Fatalf("write css: %v", err)
	}

	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-cache",
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
	}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	cases := []struct{ name, file string }{
		{"markdown", "hello.md"},
		{"image", "pic.png"},
		{"html", "page.html"},
		{"css", "style.css"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/file/ws-cache/"+tc.file, nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", "ws-cache/"+tc.file)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()
			gitH.handleFile(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("Cache-Control"); got != "no-cache" {
				t.Fatalf("expected Cache-Control=no-cache, got %q", got)
			}
		})
	}
}

func TestServeWorkspaceFile_HtmlServedAsTextHtml(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)

	workspacePath := filepath.Join(t.TempDir(), "ws-html")
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(workspacePath, "page.html"), []byte("<script>alert('xss')</script>"), 0644); err != nil {
		t.Fatalf("write html: %v", err)
	}

	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-html",
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
	}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/ws-html/page.html", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "ws-html/page.html")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	gitH.handleFile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected Content-Type text/html, got %q", ct)
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if csp != "sandbox allow-same-origin" {
		t.Fatalf("expected Content-Security-Policy %q, got %q", "sandbox allow-same-origin", csp)
	}
}

func TestServeWorkspaceFile_CssServedAsTextCss(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)

	workspacePath := filepath.Join(t.TempDir(), "ws-css")
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(workspacePath, "style.css"), []byte("body { color: red; }\n"), 0644); err != nil {
		t.Fatalf("write css: %v", err)
	}

	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-css",
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
	}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/ws-css/style.css", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "ws-css/style.css")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	gitH.handleFile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("expected Content-Type text/css, got %q", ct)
	}
}
