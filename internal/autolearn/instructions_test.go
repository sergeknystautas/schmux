package autolearn

import (
	"testing"
)

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
