package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleFileJump_ValidLocalFiles(t *testing.T) {
	server, _, st := newTestServer(t)
	workspacePath := filepath.Join(t.TempDir(), "ws-jump")
	if err := os.MkdirAll(filepath.Join(workspacePath, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	files := []string{
		"docs.md",
		"diagram.mmd",
		"image.png",
		"report.html",
		"docs/main.go",
		"percent%2Fname.md",
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(workspacePath, file), []byte("content"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	if err := st.AddWorkspace(state.Workspace{ID: "ws-jump", Path: workspacePath}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	tests := []struct {
		path     string
		location string
	}{
		{"docs.md", "/diff/ws-jump/md/docs.md"},
		{"diagram.mmd", "/diff/ws-jump/mmd/diagram.mmd"},
		{"image.png", "/diff/ws-jump/img/image.png"},
		{"report.html", "/diff/ws-jump/html/report.html"},
		{"docs%2Fmain.go", "/diff/ws-jump?file=docs%2Fmain.go"},
		{"percent%252Fname.md", "/diff/ws-jump/md/percent%252Fname.md"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/jump/ws-jump/"+tt.path, nil)
			rr := httptest.NewRecorder()
			server.handleFileJump(rr, req)

			if rr.Code != http.StatusFound {
				t.Fatalf("expected 302, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("Location"); got != tt.location {
				t.Fatalf("expected location %q, got %q", tt.location, got)
			}
		})
	}
}

func TestHandleFileJump_RejectsInvalidTargets(t *testing.T) {
	server, _, st := newTestServer(t)
	parent := t.TempDir()
	workspacePath := filepath.Join(parent, "ws-jump")
	if err := os.MkdirAll(filepath.Join(workspacePath, "directory"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	outsidePath := filepath.Join(parent, "outside.md")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(workspacePath, "escape.md")); err != nil {
		t.Fatalf("symlink outside file: %v", err)
	}
	insidePath := filepath.Join(workspacePath, "inside.md")
	if err := os.WriteFile(insidePath, []byte("inside"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	if err := os.Symlink(insidePath, filepath.Join(workspacePath, "inside-link.md")); err != nil {
		t.Fatalf("symlink inside file: %v", err)
	}
	realDirectory := filepath.Join(workspacePath, "real-directory")
	if err := os.MkdirAll(realDirectory, 0o755); err != nil {
		t.Fatalf("mkdir real directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDirectory, "nested.md"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	if err := os.Symlink(realDirectory, filepath.Join(workspacePath, "linked-directory")); err != nil {
		t.Fatalf("symlink inside directory: %v", err)
	}
	if err := st.AddWorkspace(state.Workspace{ID: "ws-jump", Path: workspacePath}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	tests := []struct {
		name string
		path string
		code int
	}{
		{"missing", "/jump/ws-jump/missing.md", http.StatusNotFound},
		{"directory", "/jump/ws-jump/directory", http.StatusForbidden},
		{"traversal", "/jump/ws-jump/..%2Foutside.md", http.StatusBadRequest},
		{"outside symlink", "/jump/ws-jump/escape.md", http.StatusForbidden},
		{"inside symlink", "/jump/ws-jump/inside-link.md", http.StatusForbidden},
		{"symlink directory", "/jump/ws-jump/linked-directory%2Fnested.md", http.StatusForbidden},
		{"unknown workspace", "/jump/unknown/file.md", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			server.handleFileJump(rr, req)
			if rr.Code != tt.code {
				t.Fatalf("expected %d, got %d: %s", tt.code, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestWorkspaceFileViewRoute(t *testing.T) {
	tests := []struct {
		filePath string
		want     string
	}{
		{"README.md", "/diff/ws-1/md/README.md"},
		{"guide.MDX", "/diff/ws-1/md/guide.MDX"},
		{"architecture.mmd", "/diff/ws-1/mmd/architecture.mmd"},
		{"assets/shot.png", "/diff/ws-1/img/assets%2Fshot.png"},
		{"assets/shot.JPEG", "/diff/ws-1/img/assets%2Fshot.JPEG"},
		{"assets/shot.webp", "/diff/ws-1/img/assets%2Fshot.webp"},
		{"assets/shot.gif", "/diff/ws-1/img/assets%2Fshot.gif"},
		{"reports/check.html", "/diff/ws-1/html/reports%2Fcheck.html"},
		{"src/main.go", "/diff/ws-1?file=src%2Fmain.go"},
	}
	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			if got := workspaceFileViewRoute("ws-1", tt.filePath); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func newWorkspaceWithIgnoredFile(t *testing.T, st state.StateStore, workspaceID string) string {
	t.Helper()
	workspacePath := filepath.Join(t.TempDir(), workspaceID)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	files := map[string][]byte{
		".gitignore": []byte("secret.md\n"),
		"secret.md":  []byte("secret"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(workspacePath, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := st.AddWorkspace(state.Workspace{ID: workspaceID, Path: workspacePath}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	return workspacePath
}

func TestHandleFileJump_RejectsVCSIgnoredFile(t *testing.T) {
	server, _, st := newTestServer(t)
	newWorkspaceWithIgnoredFile(t, st, "ws-jump-ignored")

	req := httptest.NewRequest(http.MethodGet, "/jump/ws-jump-ignored/secret.md", nil)
	rr := httptest.NewRecorder()
	server.handleFileJump(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "file is ignored") {
		t.Fatalf("expected ignored-file error, got %s", rr.Body.String())
	}
	if rr.Header().Get("Location") != "" {
		t.Fatalf("ignored file must not redirect, got %q", rr.Header().Get("Location"))
	}
}

func TestHandleFileJump_VCSIgnoreCheckFailure(t *testing.T) {
	server, _, st := newTestServer(t)
	workspacePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspacePath, "report.md"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if err := st.AddWorkspace(state.Workspace{ID: "ws-jump-ignore-error", Path: workspacePath}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/jump/ws-jump-ignore-error/report.md", nil)
	rr := httptest.NewRecorder()
	server.handleFileJump(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "failed to check ignore patterns") {
		t.Fatalf("expected ignore-check failure, got %s", rr.Body.String())
	}
	if rr.Header().Get("Location") != "" {
		t.Fatalf("failed ignore check must not redirect, got %q", rr.Header().Get("Location"))
	}
}
