package autolearn

import (
	"strings"
	"testing"
)

func TestInstructionStore_ReadWrite(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	// Initially empty
	content, err := store.Read(LayerCrossRepoPrivate, "")
	if err != nil {
		t.Fatalf("read empty should not error: %v", err)
	}
	if content != "" {
		t.Error("expected empty content for non-existent file")
	}

	// Write global
	if err := store.Write(LayerCrossRepoPrivate, "", "# Global Rules\n- Rule 1"); err != nil {
		t.Fatalf("write global failed: %v", err)
	}
	content, _ = store.Read(LayerCrossRepoPrivate, "")
	if content != "# Global Rules\n- Rule 1" {
		t.Errorf("unexpected content: %s", content)
	}

	// Write repo-private
	if err := store.Write(LayerRepoPrivate, "schmux", "# Private\n- Secret rule"); err != nil {
		t.Fatalf("write private failed: %v", err)
	}
	content, _ = store.Read(LayerRepoPrivate, "schmux")
	if content != "# Private\n- Secret rule" {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestInstructionStore_RepoPrivateRequiresRepo(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	err := store.Write(LayerRepoPrivate, "", "content")
	if err == nil {
		t.Error("expected error when repo is empty for repo_private layer")
	}
}

func TestInstructionStore_Assemble(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)
	store.Write(LayerCrossRepoPrivate, "", "# Global\nglobal rule")
	store.Write(LayerRepoPrivate, "myrepo", "# Private\nprivate rule")

	assembled := store.Assemble("myrepo", "# Public\npublic rule")
	if !strings.Contains(assembled, "global rule") {
		t.Error("should contain global instructions")
	}
	if !strings.Contains(assembled, "private rule") {
		t.Error("should contain private instructions")
	}
	if !strings.Contains(assembled, "public rule") {
		t.Error("should contain public instructions")
	}
	// Global should come before private, private before public
	globalIdx := strings.Index(assembled, "global rule")
	privateIdx := strings.Index(assembled, "private rule")
	publicIdx := strings.Index(assembled, "public rule")
	if globalIdx >= privateIdx || privateIdx >= publicIdx {
		t.Error("assembly order should be global < private < public")
	}
}

func TestInstructionStore_AssembleEmptyLayers(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	// Only public content
	assembled := store.Assemble("myrepo", "# Public\npublic rule")
	if assembled != "# Public\npublic rule" {
		t.Errorf("expected only public content, got: %s", assembled)
	}

	// No content at all
	assembled = store.Assemble("myrepo", "")
	if assembled != "" {
		t.Errorf("expected empty string, got: %s", assembled)
	}
}

func TestInstructionStore_UnsupportedLayer(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	_, err := store.Read(LayerRepoPublic, "myrepo")
	if err == nil {
		t.Error("expected error for repo_public layer (not managed by InstructionStore)")
	}
}

func TestInstructionStore_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	store := NewInstructionStore(dir)

	err := store.Write(LayerRepoPrivate, "../escape", "malicious")
	if err == nil {
		t.Error("expected error for path traversal in repo name")
	}
}
