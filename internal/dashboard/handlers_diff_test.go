package dashboard

// Note: TestHandleDiff_RejectsNonGitWorkspace was removed — the diff handler
// is now VCS-agnostic via CommandBuilder and works with any VCS type.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if err := os.WriteFile(filepath.Join(workspacePath, "diagram.mmd"), []byte("graph TD; A-->B\n"), 0644); err != nil {
		t.Fatalf("write mmd: %v", err)
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
		{"mermaid", "diagram.mmd"},
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

// fileRequest builds a request to /api/file/{wsID}/{file} with the chi
// wildcard param set the way the router would set it. rawQuery is appended
// verbatim (e.g. "download=1"); pass "" for none.
func fileRequest(wsID, file, rawQuery string) *http.Request {
	target := "/api/file/" + wsID + "/" + url.PathEscape(file)
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", wsID+"/"+file)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// newDownloadWorkspace creates a git-initialised workspace containing:
//
//	hello.md      — allowlisted text
//	blob.bin      — non-allowlisted, contains NUL bytes
//	page.html     — allowlisted, gets CSP inline
//	sub/          — a directory
//	ignored.bin   — listed in .gitignore
//	it's a "file".bin — quote and space in the name
//
// and registers it in state under wsID.
func newDownloadWorkspace(t *testing.T, st state.StateStore, wsID string) string {
	t.Helper()
	workspacePath := filepath.Join(t.TempDir(), wsID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "sub"), 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	files := map[string][]byte{
		"hello.md":            []byte("# hi\n"),
		"blob.bin":            {0x00, 0x01, 0x02, 0xff, 'x'},
		"page.html":           []byte("<h1>hello</h1>\n"),
		"ignored.bin":         []byte("secret"),
		".gitignore":          []byte("ignored.bin\n"),
		"it's a \"file\".bin": []byte("q"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(workspacePath, name), data, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := st.AddWorkspace(state.Workspace{
		ID:     wsID,
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
	}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	return workspacePath
}

func TestServeWorkspaceFile_DownloadMode(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)
	newDownloadWorkspace(t, st, "ws-dl")

	cases := []struct {
		name      string
		file      string
		wantBody  []byte
		wantDispo string
	}{
		{"allowlisted markdown", "hello.md", []byte("# hi\n"), `attachment; filename=hello.md`},
		{"non-allowlisted binary", "blob.bin", []byte{0x00, 0x01, 0x02, 0xff, 'x'}, `attachment; filename=blob.bin`},
		{"html gets no CSP", "page.html", []byte("<h1>hello</h1>\n"), `attachment; filename=page.html`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			gitH.handleFile(rr, fileRequest("ws-dl", tc.file, "download=1"))

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("Content-Type"); got != "application/octet-stream" {
				t.Fatalf("Content-Type = %q, want application/octet-stream", got)
			}
			if got := rr.Header().Get("Content-Disposition"); got != tc.wantDispo {
				t.Fatalf("Content-Disposition = %q, want %q", got, tc.wantDispo)
			}
			if got := rr.Header().Get("Cache-Control"); got != "no-cache" {
				t.Fatalf("Cache-Control = %q, want no-cache", got)
			}
			if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := rr.Header().Get("Content-Security-Policy"); got != "" {
				t.Fatalf("Content-Security-Policy = %q, want unset in download mode", got)
			}
			if !bytes.Equal(rr.Body.Bytes(), tc.wantBody) {
				t.Fatalf("body = %v, want %v", rr.Body.Bytes(), tc.wantBody)
			}
		})
	}
}

func TestServeWorkspaceFile_DownloadMode_QuotesFilename(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)
	newDownloadWorkspace(t, st, "ws-dl-quote")

	rr := httptest.NewRecorder()
	gitH.handleFile(rr, fileRequest("ws-dl-quote", "it's a \"file\".bin", "download=1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	want := `attachment; filename="it's a \"file\".bin"`
	if got := rr.Header().Get("Content-Disposition"); got != want {
		t.Fatalf("Content-Disposition = %q, want %q", got, want)
	}
}

func TestServeWorkspaceFile_InlineModeUnchanged(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)
	newDownloadWorkspace(t, st, "ws-inline")

	t.Run("non-allowlisted still 403 without download", func(t *testing.T) {
		rr := httptest.NewRecorder()
		gitH.handleFile(rr, fileRequest("ws-inline", "blob.bin", ""))
		if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "file type not allowed") {
			t.Fatalf("expected 403 file type not allowed, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("non-allowlisted still 403 with download=0", func(t *testing.T) {
		rr := httptest.NewRecorder()
		gitH.handleFile(rr, fileRequest("ws-inline", "blob.bin", "download=0"))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("html keeps text/html and CSP", func(t *testing.T) {
		rr := httptest.NewRecorder()
		gitH.handleFile(rr, fileRequest("ws-inline", "page.html", ""))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		if got := rr.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("Content-Type = %q", got)
		}
		if got := rr.Header().Get("Content-Security-Policy"); got != "sandbox allow-same-origin" {
			t.Fatalf("Content-Security-Policy = %q", got)
		}
		if got := rr.Header().Get("Content-Disposition"); got != "" {
			t.Fatalf("Content-Disposition = %q, want unset in inline mode", got)
		}
	})
}

func TestServeWorkspaceFile_DownloadMode_SecurityChecksStillApply(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)
	newDownloadWorkspace(t, st, "ws-sec")

	cases := []struct {
		name     string
		file     string
		wantCode int
		wantMsg  string
	}{
		{"traversal", "../../etc/passwd", http.StatusForbidden, "invalid file path"},
		{"directory", "sub", http.StatusForbidden, "cannot serve directory"},
		{"missing", "nope.bin", http.StatusNotFound, "file not found"},
		{"gitignored", "ignored.bin", http.StatusForbidden, "file is ignored by git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			gitH.handleFile(rr, fileRequest("ws-sec", tc.file, "download=1"))
			if rr.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d: %s", tc.wantCode, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.wantMsg) {
				t.Fatalf("expected body to contain %q, got %s", tc.wantMsg, rr.Body.String())
			}
			if got := rr.Header().Get("Content-Disposition"); got != "" {
				t.Fatalf("Content-Disposition = %q, want unset on error", got)
			}
		})
	}
}

func TestHandleFile_DownloadMode_RejectsRemoteWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)
	if err := st.AddWorkspace(state.Workspace{
		ID:           "ws-remote",
		Repo:         "test",
		Branch:       "main",
		RemoteHostID: "host-1",
		RemotePath:   "/remote/ws",
	}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	rr := httptest.NewRecorder()
	gitH.handleFile(rr, fileRequest("ws-remote", "hello.md", "download=1"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "download not supported for remote workspaces") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}
