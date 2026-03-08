package lore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstructionStore manages private instruction files:
//   - cross_repo_private: <baseDir>/cross-repo-private.md
//   - repo_private: <baseDir>/repos/<repo>/private.md
type InstructionStore struct {
	baseDir string
}

// NewInstructionStore creates a new InstructionStore rooted at the given directory.
// Typically baseDir is ~/.schmux/instructions/.
func NewInstructionStore(baseDir string) *InstructionStore {
	return &InstructionStore{baseDir: baseDir}
}

func (s *InstructionStore) pathFor(layer Layer, repo string) (string, error) {
	switch layer {
	case LayerCrossRepoPrivate:
		return filepath.Join(s.baseDir, "cross-repo-private.md"), nil
	case LayerRepoPrivate:
		if repo == "" {
			return "", fmt.Errorf("repo_private layer requires a repo name")
		}
		if strings.Contains(repo, "..") || strings.ContainsAny(repo, "/\\") {
			return "", fmt.Errorf("invalid repo name: %s", repo)
		}
		return filepath.Join(s.baseDir, "repos", repo, "private.md"), nil
	default:
		return "", fmt.Errorf("unsupported layer for InstructionStore: %s", layer)
	}
}

// Read returns the content of the instruction file for the given layer and repo.
// Returns empty string (not error) if the file does not exist.
func (s *InstructionStore) Read(layer Layer, repo string) (string, error) {
	path, err := s.pathFor(layer, repo)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// Write writes content to the instruction file for the given layer and repo.
// Creates parent directories as needed.
func (s *InstructionStore) Write(layer Layer, repo string, content string) error {
	path, err := s.pathFor(layer, repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// Assemble concatenates all instruction layers for a given repo, in order:
// cross_repo_private, repo_private, then the public content (passed in).
// Empty layers are skipped. Each layer is separated by a blank line.
func (s *InstructionStore) Assemble(repo string, publicContent string) string {
	var sections []string

	if crossRepo, _ := s.Read(LayerCrossRepoPrivate, ""); crossRepo != "" {
		sections = append(sections, crossRepo)
	}
	if private, _ := s.Read(LayerRepoPrivate, repo); private != "" {
		sections = append(sections, private)
	}
	if publicContent != "" {
		sections = append(sections, publicContent)
	}

	return strings.Join(sections, "\n\n")
}
