package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleFileJump_ValidLocalFiles(t *testing.T) {
	server, _, st := newTestServer(t)
	workspacePath := filepath.Join(t.TempDir(), "ws-jump")
	if err := os.MkdirAll(filepath.Join(workspacePath, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
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
