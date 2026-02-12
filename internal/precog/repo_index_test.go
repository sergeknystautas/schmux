package precog

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}

func setupRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	return repo
}

func commitAll(t *testing.T, repo, authorName, authorEmail, message string) {
	t.Helper()
	runGit(t, repo, "add", ".")
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+authorName,
		"GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+authorName,
		"GIT_COMMITTER_EMAIL="+authorEmail,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, string(out))
	}
}

func TestRepoIndexerAnalyze(t *testing.T) {
	repo := setupRepo(t)

	writeFile(t, repo, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeFile(t, repo, "internal/a.go", "package internal\n\nfunc A() {}\n")
	writeFile(t, repo, "internal/b.go", "package internal\n\nfunc B() {}\n")
	writeFile(t, repo, "assets/dashboard/package.json", `{"name":"dashboard"}`)
	writeFile(t, repo, "assets/dashboard/src/app.ts", "export const app = 1;\n")
	commitAll(t, repo, "Alice", "alice@example.com", "initial")

	writeFile(t, repo, "internal/a.go", "package internal\n\nfunc A() { println(\"a\") }\n")
	writeFile(t, repo, "internal/b.go", "package internal\n\nfunc B() { println(\"b\") }\n")
	commitAll(t, repo, "Bob", "bob@example.com", "change a and b")

	writeFile(t, repo, "internal/a.go", "package internal\n\nfunc A() { println(\"a2\") }\n")
	writeFile(t, repo, "assets/dashboard/src/app.ts", "export const app = 2;\n")
	commitAll(t, repo, "Alice", "alice@example.com", "change a and app")

	indexer := NewRepoIndexer(100)
	index, err := indexer.Analyze(context.Background(), repo)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if index.RepoPath == "" {
		t.Fatalf("RepoPath should be set")
	}

	info, ok := index.Files["internal/a.go"]
	if !ok {
		t.Fatalf("expected internal/a.go in files map")
	}
	if len(info.Authors) != 2 {
		t.Fatalf("expected 2 authors for internal/a.go, got %d", len(info.Authors))
	}

	neighbors := index.Coupling["internal/a.go"]
	if len(neighbors) == 0 {
		t.Fatalf("expected coupling neighbors for internal/a.go")
	}

	foundB := false
	for _, n := range neighbors {
		if n.Path == "internal/b.go" {
			foundB = true
			if n.Strength <= 0 {
				t.Fatalf("expected positive strength for a<->b")
			}
		}
	}
	if !foundB {
		t.Fatalf("expected internal/b.go in coupling neighbors")
	}

	hasDashboardPkg := false
	for _, pkg := range index.Packages {
		if pkg.Path == "assets/dashboard/src" && pkg.ConfigFile == "assets/dashboard/package.json" {
			hasDashboardPkg = true
			break
		}
	}
	if !hasDashboardPkg {
		t.Fatalf("expected assets/dashboard/src package mapped to assets/dashboard/package.json")
	}
}
