package detect

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Use directory-level embed (not glob) so it compiles even when the
// directories contain no .yaml files.
//
//go:embed all:descriptors
var embeddedDescriptors embed.FS

//go:embed all:contrib
var embeddedContrib embed.FS

// LoadEmbeddedDescriptors loads descriptors from the embedded descriptors/
// (builtin) and contrib/ (external, populated by CI) directories.
// Returns them merged with name collision detection.
func LoadEmbeddedDescriptors() ([]*Descriptor, error) {
	var all []*Descriptor
	seen := map[string]string{}

	for _, src := range []struct {
		fs    embed.FS
		dir   string
		label string
	}{
		{embeddedDescriptors, "descriptors", "descriptors"},
		{embeddedContrib, "contrib", "contrib"},
	} {
		entries, err := fs.ReadDir(src.fs, src.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !isYAMLFile(e.Name()) {
				continue
			}
			data, err := fs.ReadFile(src.fs, src.dir+"/"+e.Name())
			if err != nil {
				return nil, fmt.Errorf("read embedded %s/%s: %w", src.label, e.Name(), err)
			}
			d, err := ParseDescriptor(data)
			if err != nil {
				return nil, fmt.Errorf("parse embedded %s/%s: %w", src.label, e.Name(), err)
			}
			if prev, ok := seen[d.Name]; ok {
				return nil, fmt.Errorf("duplicate descriptor name %q in %s and %s/%s", d.Name, prev, src.label, e.Name())
			}
			seen[d.Name] = src.label + "/" + e.Name()
			all = append(all, d)
		}
	}
	return all, nil
}

// LoadRuntimeDescriptors loads descriptors from a directory on disk
// (typically ~/.schmux/adapters/). Returns empty slice if dir doesn't exist.
// Individual files that fail to read or parse are skipped with a warning so
// that one bad YAML file does not prevent all other adapters from loading.
func LoadRuntimeDescriptors(dir string) ([]*Descriptor, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var all []*Descriptor
	seen := map[string]string{}

	for _, e := range entries {
		if e.IsDir() || !isYAMLFile(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			if pkgLogger != nil {
				pkgLogger.Warn("skipping runtime descriptor", "file", e.Name(), "err", err)
			}
			continue
		}
		d, err := ParseDescriptorLenient(data)
		if err != nil {
			if pkgLogger != nil {
				pkgLogger.Warn("skipping runtime descriptor", "file", e.Name(), "err", err)
			}
			continue
		}
		if prev, ok := seen[d.Name]; ok {
			if pkgLogger != nil {
				pkgLogger.Warn("skipping duplicate runtime descriptor", "name", d.Name, "file", e.Name(), "first", prev)
			}
			continue
		}
		seen[d.Name] = e.Name()
		all = append(all, d)
	}
	return all, nil
}

// RegisterDescriptorAdapters creates GenericAdapters from descriptors and
// registers them into the adapter registry. Silently skips any descriptor
// whose name already exists (e.g., already registered by init or a builtin).
// Individual descriptors that fail to instantiate (e.g., due to missing hook
// strategies during init-time registration) are skipped without halting
// registration of the remaining descriptors.
func RegisterDescriptorAdapters(descs []*Descriptor) error {
	for _, d := range descs {
		if existing := GetAdapter(d.Name); existing != nil {
			continue
		}
		a, err := NewGenericAdapter(d)
		if err != nil {
			if pkgLogger != nil {
				pkgLogger.Warn("skipping descriptor", "name", d.Name, "err", err)
			}
			continue
		}
		registerAdapter(a)
		registerToolName(d.Name)
		if d.Instruction != nil {
			registerInstructionConfig(d.Name, AgentInstructionConfig{
				InstructionDir:  d.Instruction.Dir,
				InstructionFile: d.Instruction.File,
			})
		}
	}
	return nil
}

// LoadAndRegisterDescriptors loads descriptors from embedded and runtime
// sources, merges them (embedded wins on collision), and registers as adapters.
func LoadAndRegisterDescriptors(runtimeDir string) error {
	embedded, err := LoadEmbeddedDescriptors()
	if err != nil {
		return fmt.Errorf("load embedded descriptors: %w", err)
	}
	runtime, err := LoadRuntimeDescriptors(runtimeDir)
	if err != nil {
		return fmt.Errorf("load runtime descriptors: %w", err)
	}
	embeddedNames := map[string]bool{}
	for _, d := range embedded {
		embeddedNames[d.Name] = true
	}
	all := embedded
	for _, d := range runtime {
		if embeddedNames[d.Name] {
			continue
		}
		all = append(all, d)
	}
	return RegisterDescriptorAdapters(all)
}

func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

func init() {
	descs, err := LoadEmbeddedDescriptors()
	if err != nil {
		return
	}
	_ = RegisterDescriptorAdapters(descs)
}
