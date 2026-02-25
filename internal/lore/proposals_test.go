package lore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestProposalStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	proposal := &Proposal{
		ID:            "prop-20260213-143200",
		Repo:          "schmux",
		Status:        ProposalPending,
		SourceCount:   3,
		Sources:       []string{"ws-abc", "ws-def"},
		FileHashes:    map[string]string{"CLAUDE.md": "sha256:abc"},
		ProposedFiles: map[string]string{"CLAUDE.md": "# Updated content"},
		DiffSummary:   "Added 1 item",
		EntriesUsed:   []string{"entry-1"},
	}

	if err := store.Save(proposal); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Get("schmux", "prop-20260213-143200")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if loaded.DiffSummary != "Added 1 item" {
		t.Errorf("expected 'Added 1 item', got %q", loaded.DiffSummary)
	}
	if loaded.Status != ProposalPending {
		t.Errorf("expected pending, got %s", loaded.Status)
	}
}

func TestProposalStore_List(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	for _, id := range []string{"prop-001", "prop-002"} {
		store.Save(&Proposal{ID: id, Repo: "myrepo", Status: ProposalPending})
	}
	store.Save(&Proposal{ID: "prop-003", Repo: "otherrepo", Status: ProposalPending})

	proposals, err := store.List("myrepo")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(proposals) != 2 {
		t.Fatalf("expected 2 proposals for myrepo, got %d", len(proposals))
	}
}

func TestProposalStore_UpdateStatus(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)
	store.Save(&Proposal{ID: "prop-001", Repo: "myrepo", Status: ProposalPending})

	if err := store.UpdateStatus("myrepo", "prop-001", ProposalApplied); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	loaded, _ := store.Get("myrepo", "prop-001")
	if loaded.Status != ProposalApplied {
		t.Errorf("expected applied, got %s", loaded.Status)
	}
}

func TestProposalStore_IsStale(t *testing.T) {
	dir := t.TempDir()
	// Write a fake CLAUDE.md
	repoDir := filepath.Join(dir, "repo")
	writeTestFile(t, filepath.Join(repoDir, "CLAUDE.md"), "# Original")

	proposal := &Proposal{
		FileHashes: map[string]string{"CLAUDE.md": "sha256:wrong"},
	}

	stale, err := proposal.IsStale(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected proposal to be stale when hashes don't match")
	}
}

func TestProposalStore_CurrentFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	proposal := &Proposal{
		ID:            "prop-current-files",
		Repo:          "schmux",
		Status:        ProposalPending,
		SourceCount:   1,
		FileHashes:    map[string]string{"CLAUDE.md": "sha256:abc"},
		CurrentFiles:  map[string]string{"CLAUDE.md": "# Original content"},
		ProposedFiles: map[string]string{"CLAUDE.md": "# Updated content"},
		DiffSummary:   "Updated content",
		EntriesUsed:   []string{"entry-1"},
	}

	if err := store.Save(proposal); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Get("schmux", "prop-current-files")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if loaded.CurrentFiles["CLAUDE.md"] != "# Original content" {
		t.Errorf("expected current file content, got %q", loaded.CurrentFiles["CLAUDE.md"])
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestHashFileContent(t *testing.T) {
	t.Parallel()

	t.Run("returns sha256 prefixed hash", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.md")
		content := "# Hello World\n"
		writeTestFile(t, path, content)

		got, err := HashFileContent(path)
		if err != nil {
			t.Fatalf("HashFileContent() error: %v", err)
		}

		// Verify against independently computed hash
		h := sha256.Sum256([]byte(content))
		want := "sha256:" + hex.EncodeToString(h[:])
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("different content produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		path1 := filepath.Join(dir, "a.md")
		path2 := filepath.Join(dir, "b.md")
		writeTestFile(t, path1, "content A")
		writeTestFile(t, path2, "content B")

		hash1, _ := HashFileContent(path1)
		hash2, _ := HashFileContent(path2)
		if hash1 == hash2 {
			t.Error("different files should produce different hashes")
		}
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		_, err := HashFileContent("/nonexistent/path/file.md")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("empty file produces valid hash", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.md")
		writeTestFile(t, path, "")

		got, err := HashFileContent(path)
		if err != nil {
			t.Fatalf("HashFileContent() error: %v", err)
		}
		if len(got) == 0 {
			t.Error("expected non-empty hash for empty file")
		}
		if got[:7] != "sha256:" {
			t.Errorf("hash should start with 'sha256:', got %q", got)
		}
	})
}
