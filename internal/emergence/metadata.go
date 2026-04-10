package emergence

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
	data    map[string]map[string]contracts.EmergenceMetadata // repo -> skill_name -> metadata
	baseDir string
}

// NewMetadataStore creates a new metadata store.
func NewMetadataStore(baseDir string) *MetadataStore {
	return &MetadataStore{
		baseDir: baseDir,
		data:    make(map[string]map[string]contracts.EmergenceMetadata),
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
			s.data[repo] = make(map[string]contracts.EmergenceMetadata)
			return nil
		}
		return fmt.Errorf("read emergence metadata: %w", err)
	}
	var m map[string]contracts.EmergenceMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse emergence metadata: %w", err)
	}
	s.data[repo] = m
	return nil
}

func (s *MetadataStore) save(repo string) error {
	dir := filepath.Dir(s.filePath(repo))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create metadata dir: %w", err)
	}
	data, err := json.MarshalIndent(s.data[repo], "", "  ")
	if err != nil {
		return fmt.Errorf("marshal emergence metadata: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".metadata-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	return os.Rename(tmpPath, s.filePath(repo))
}

// Save writes or updates metadata for a skill.
func (s *MetadataStore) Save(repo string, meta contracts.EmergenceMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}
	s.data[repo][meta.SkillName] = meta
	return s.save(repo)
}

// Get returns metadata for a specific skill.
func (s *MetadataStore) Get(repo, skillName string) (contracts.EmergenceMetadata, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return contracts.EmergenceMetadata{}, false, err
	}
	meta, ok := s.data[repo][skillName]
	return meta, ok, nil
}
