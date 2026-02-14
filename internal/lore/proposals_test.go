package lore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProposalStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir)

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
	store := NewProposalStore(dir)

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
	store := NewProposalStore(dir)
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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
