package style

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Manager struct {
	dir string
}

func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

func (m *Manager) List() ([]*Style, error) {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create styles directory: %w", err)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read styles directory: %w", err)
	}
	var styles []*Style
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}
		s, err := ParseStyle(data)
		if err != nil {
			continue
		}
		styles = append(styles, s)
	}
	sort.Slice(styles, func(i, j int) bool {
		return styles[i].Name < styles[j].Name
	})
	return styles, nil
}

func (m *Manager) Get(id string) (*Style, error) {
	path := filepath.Join(m.dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("style not found: %s", id)
		}
		return nil, fmt.Errorf("failed to read style %s: %w", id, err)
	}
	return ParseStyle(data)
}

func (m *Manager) Create(s *Style) error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create styles directory: %w", err)
	}
	path := filepath.Join(m.dir, s.ID+".yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("style already exists: %s", s.ID)
	}
	data, err := MarshalStyle(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (m *Manager) Update(s *Style) error {
	path := filepath.Join(m.dir, s.ID+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("style not found: %s", s.ID)
	}
	data, err := MarshalStyle(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (m *Manager) Delete(id string) error {
	path := filepath.Join(m.dir, id+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("style not found: %s", id)
	}
	return os.Remove(path)
}

func (m *Manager) EnsureBuiltins() error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create styles directory: %w", err)
	}
	entries, err := builtinsFS.ReadDir("builtins")
	if err != nil {
		return fmt.Errorf("failed to read embedded builtins: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		destPath := filepath.Join(m.dir, entry.Name())
		if _, err := os.Stat(destPath); err == nil {
			continue
		}
		data, err := builtinsFS.ReadFile("builtins/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read embedded builtin %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write builtin %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (m *Manager) ResetBuiltIn(id string) error {
	filename := id + ".yaml"
	data, err := builtinsFS.ReadFile("builtins/" + filename)
	if err != nil {
		return fmt.Errorf("not a built-in style: %s", id)
	}
	destPath := filepath.Join(m.dir, filename)
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write builtin %s: %w", filename, err)
	}
	return nil
}
