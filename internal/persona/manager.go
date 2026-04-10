package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manager handles CRUD operations on persona YAML files.
type Manager struct {
	dir string
}

// NewManager creates a Manager that stores personas in the given directory.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// List returns all personas sorted by name.
func (m *Manager) List() ([]*Persona, error) {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create personas directory: %w", err)
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read personas directory: %w", err)
	}

	var personas []*Persona
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}
		p, err := ParsePersona(data)
		if err != nil {
			continue
		}
		personas = append(personas, p)
	}

	sort.Slice(personas, func(i, j int) bool {
		return personas[i].Name < personas[j].Name
	})
	return personas, nil
}

// Get returns a persona by ID.
func (m *Manager) Get(id string) (*Persona, error) {
	path := filepath.Join(m.dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("persona not found: %s", id)
		}
		return nil, fmt.Errorf("failed to read persona %s: %w", id, err)
	}
	return ParsePersona(data)
}

// Create writes a new persona file. Fails if the ID already exists.
func (m *Manager) Create(p *Persona) error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create personas directory: %w", err)
	}

	path := filepath.Join(m.dir, p.ID+".yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("persona already exists: %s", p.ID)
	}

	data, err := MarshalPersona(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Update overwrites an existing persona file. Fails if the ID doesn't exist.
func (m *Manager) Update(p *Persona) error {
	path := filepath.Join(m.dir, p.ID+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("persona not found: %s", p.ID)
	}

	data, err := MarshalPersona(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Delete removes a persona file.
func (m *Manager) Delete(id string) error {
	path := filepath.Join(m.dir, id+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("persona not found: %s", id)
	}
	return os.Remove(path)
}

// EnsureBuiltins writes built-in personas to disk if they don't already exist.
// Does not overwrite user-modified personas.
func (m *Manager) EnsureBuiltins() error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create personas directory: %w", err)
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

		// Skip if file already exists (don't overwrite user edits)
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

// ResetBuiltIn deletes a built-in persona file and re-copies the original from
// the embedded filesystem. Returns an error if the ID is not a built-in persona.
func (m *Manager) ResetBuiltIn(id string) error {
	filename := id + ".yaml"
	data, err := builtinsFS.ReadFile("builtins/" + filename)
	if err != nil {
		return fmt.Errorf("not a built-in persona: %s", id)
	}

	destPath := filepath.Join(m.dir, filename)
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write builtin %s: %w", filename, err)
	}
	return nil
}
