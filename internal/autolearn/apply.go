//go:build !noautolearn

package autolearn

import (
	"fmt"
	"strings"
)

// ApplyToLayer writes merged content to a private instruction layer.
// Public layer (repo_public) is handled by the workspace-based flow.
func ApplyToLayer(store *InstructionStore, layer Layer, repo, content string) error {
	if layer == LayerRepoPublic {
		return fmt.Errorf("public layer is applied via workspace, not instruction store")
	}
	return store.Write(layer, repo, content)
}

// NormalizeLearningTitle lowercases, collapses whitespace, and strips
// trailing punctuation for fuzzy comparison.
func NormalizeLearningTitle(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	// Collapse runs of whitespace to a single space
	parts := strings.Fields(text)
	text = strings.Join(parts, " ")
	// Strip trailing punctuation
	text = strings.TrimRight(text, ".!?,;:")
	return text
}

// DeduplicateLearnings filters out learnings whose normalized title matches
// any of the exclude titles. Returns the remaining learnings and the count
// of removed duplicates.
func DeduplicateLearnings(learnings []Learning, excludeTitles []string) ([]Learning, int) {
	existing := make(map[string]bool, len(excludeTitles))
	for _, t := range excludeTitles {
		existing[NormalizeLearningTitle(t)] = true
	}
	var kept []Learning
	removed := 0
	for _, l := range learnings {
		if existing[NormalizeLearningTitle(l.Title)] {
			removed++
			continue
		}
		kept = append(kept, l)
	}
	return kept, removed
}
