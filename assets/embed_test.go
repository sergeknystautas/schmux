package dashboardassets

import (
	"io/fs"
	"testing"
)

func TestFS_ReturnsNonNilWhenAssetsBuilt(t *testing.T) {
	result := FS()
	if result == nil {
		t.Skip("dashboard assets not built (dist/ contains only placeholder)")
	}

	// Verify index.html is readable
	content, err := fs.ReadFile(result, "index.html")
	if err != nil {
		t.Fatalf("failed to read index.html from embedded FS: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("index.html is empty")
	}
}

func TestFS_ContainsAssetsSubdirectory(t *testing.T) {
	result := FS()
	if result == nil {
		t.Skip("dashboard assets not built (dist/ contains only placeholder)")
	}

	// The Vite build outputs hashed files into assets/
	entries, err := fs.ReadDir(result, "assets")
	if err != nil {
		t.Fatalf("failed to read assets/ directory from embedded FS: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("assets/ directory is empty")
	}
}

func TestFS_EmbedDirectoryExists(t *testing.T) {
	// The raw embedded FS should always have the dashboard/dist directory,
	// even on fresh clones (due to .gitkeep).
	entries, err := fs.ReadDir(dashboardFS, "dashboard/dist")
	if err != nil {
		t.Fatalf("expected dashboard/dist directory in embedded FS: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("dashboard/dist is empty in embedded FS")
	}
}
