package spawn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// MetadataStore manages emergence metadata per skill per repository.
type MetadataStore struct {
	mu      sync.Mutex
	data    map[string]map[string]contracts.SpawnMetadata // repo -> skill_name -> metadata
	baseDir string
}

// NewMetadataStore creates a new metadata store.
func NewMetadataStore(baseDir string) *MetadataStore {
	return &MetadataStore{
		baseDir: baseDir,
		data:    make(map[string]map[string]contracts.SpawnMetadata),
	}
}

func (s *MetadataStore) filePath(repo string) string {
	return filepath.Join(s.baseDir, repo, "metadata.json")
}

func (s *MetadataStore) load(repo string) error {
	if _, ok := s.data[repo]; ok {
		return nil
	}
	data, err := os.ReadFile(s.filePath(repo))
	if err != nil {
		if os.IsNotExist(err) {
			s.data[repo] = make(map[string]contracts.SpawnMetadata)
			return nil
		}
		return fmt.Errorf("read emergence metadata: %w", err)
	}
	var m map[string]contracts.SpawnMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse emergence metadata: %w", err)
	}
	s.data[repo] = m
	return nil
}

// Get returns metadata for a specific skill.
func (s *MetadataStore) Get(repo, skillName string) (contracts.SpawnMetadata, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return contracts.SpawnMetadata{}, false, err
	}
	meta, ok := s.data[repo][skillName]
	return meta, ok, nil
}
