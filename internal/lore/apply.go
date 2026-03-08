package lore

import "fmt"

// ApplyToLayer writes merged content to a private instruction layer.
// Public layer (repo_public) is handled by the workspace-based flow in handlers_lore.go.
func ApplyToLayer(store *InstructionStore, layer Layer, repo, content string) error {
	if layer == LayerRepoPublic {
		return fmt.Errorf("public layer is applied via workspace, not instruction store")
	}
	return store.Write(layer, repo, content)
}
