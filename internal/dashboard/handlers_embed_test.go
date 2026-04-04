package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func newServerWithEmbedFS(t *testing.T, opts ServerOptions) *Server {
	t.Helper()
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, nil, opts)
	return server
}

func TestServeAppIndex_FromEmbeddedFS(t *testing.T) {
	server := newServerWithEmbedFS(t, ServerOptions{})
	server.dashboardFS = fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>embedded</html>")},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.serveAppIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	if body := rr.Body.String(); body != "<html>embedded</html>" {
		t.Errorf("expected embedded content, got %q", body)
	}
}

func TestServeAppIndex_FallsBackToDisk(t *testing.T) {
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html>disk</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	server := newServerWithEmbedFS(t, ServerOptions{DashboardDistPath: distDir})
	server.dashboardFS = nil // no embedded assets

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.serveAppIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if body := rr.Body.String(); body != "<html>disk</html>" {
		t.Errorf("expected disk content, got %q", body)
	}
}

func TestServeAppIndex_Returns404WhenNothingAvailable(t *testing.T) {
	server := newServerWithEmbedFS(t, ServerOptions{DashboardDistPath: t.TempDir()})
	server.dashboardFS = nil // no embedded assets

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.serveAppIndex(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	if body := rr.Body.String(); body == "" {
		t.Error("expected error message in body")
	}
}

func TestServeFileIfExists_FromEmbeddedFS(t *testing.T) {
	server := newServerWithEmbedFS(t, ServerOptions{})
	server.dashboardFS = fstest.MapFS{
		"favicon.svg": &fstest.MapFile{Data: []byte("<svg>icon</svg>")},
	}

	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	rr := httptest.NewRecorder()

	found := server.serveFileIfExists(rr, req, "/favicon.svg")

	if !found {
		t.Fatal("expected file to be found in embedded FS")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestServeFileIfExists_RejectsPathTraversal(t *testing.T) {
	server := newServerWithEmbedFS(t, ServerOptions{})
	server.dashboardFS = fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("secret")},
	}

	req := httptest.NewRequest(http.MethodGet, "/../../../etc/passwd", nil)
	rr := httptest.NewRecorder()

	found := server.serveFileIfExists(rr, req, "/../../../etc/passwd")

	if found {
		t.Fatal("path traversal should be rejected")
	}
}

func TestServeFileIfExists_FallsBackToDisk(t *testing.T) {
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "robots.txt"), []byte("User-agent: *"), 0644); err != nil {
		t.Fatal(err)
	}
	server := newServerWithEmbedFS(t, ServerOptions{DashboardDistPath: distDir})
	server.dashboardFS = fstest.MapFS{} // empty embedded FS

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rr := httptest.NewRecorder()

	found := server.serveFileIfExists(rr, req, "/robots.txt")

	if !found {
		t.Fatal("expected file to be found on disk")
	}
}

func TestServeFileIfExists_ReturnsFalseWhenMissing(t *testing.T) {
	server := newServerWithEmbedFS(t, ServerOptions{DashboardDistPath: t.TempDir()})
	server.dashboardFS = fstest.MapFS{} // empty

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.txt", nil)
	rr := httptest.NewRecorder()

	found := server.serveFileIfExists(rr, req, "/nonexistent.txt")

	if found {
		t.Fatal("expected file not to be found")
	}
}

func TestServeAppIndex_EmbeddedTakesPrecedenceOverDisk(t *testing.T) {
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html>disk-loses</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	server := newServerWithEmbedFS(t, ServerOptions{DashboardDistPath: distDir})

	// Set up embedded FS
	server.dashboardFS = fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>embedded-wins</html>")},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.serveAppIndex(rr, req)

	if body := rr.Body.String(); body != "<html>embedded-wins</html>" {
		t.Errorf("embedded FS should take precedence, got %q", body)
	}
}
