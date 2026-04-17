package detect

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
)

// Use directory-level embed (not glob) so it compiles even when the
// directories contain no .yaml files.
//
//go:embed all:descriptors
var embeddedDescriptors embed.FS

//go:embed all:contrib
var embeddedContrib embed.FS

// LoadEmbeddedDescriptors loads descriptors from the embedded descriptors/
// (built-in OSS defaults) and contrib/ (downstream build-time additions and
// overrides) directories. Contrib descriptors override descriptors with the
// same name, mirroring the runtime→embedded override pattern in
// LoadAndRegisterDescriptors. This lets downstream builds (e.g. Meta's
// internal schmux) customize adapter behavior without forking the OSS source.
func LoadEmbeddedDescriptors() ([]*Descriptor, error) {
	return loadEmbeddedDescriptorsFrom(embeddedDescriptors, "descriptors", embeddedContrib, "contrib")
}

// loadEmbeddedDescriptorsFrom is the testable core of LoadEmbeddedDescriptors:
// loads two descriptor directories and merges them with override taking
// precedence on name collision.
func loadEmbeddedDescriptorsFrom(baseFS fs.FS, baseDir string, overrideFS fs.FS, overrideDir string) ([]*Descriptor, error) {
	base, err := loadDescriptorsFromFS(baseFS, baseDir)
	if err != nil {
		return nil, err
	}
	override, err := loadDescriptorsFromFS(overrideFS, overrideDir)
	if err != nil {
		return nil, err
	}

	overrideNames := map[string]bool{}
	for _, d := range override {
		overrideNames[d.Name] = true
	}
	all := append([]*Descriptor(nil), override...)
	for _, d := range base {
		if overrideNames[d.Name] {
			continue
		}
		all = append(all, d)
	}
	return all, nil
}

// loadDescriptorsFromFS loads all .yaml descriptors from a single directory in
// a filesystem. Returns an error if two files within the directory declare the
// same descriptor name. A missing directory is treated as empty (not an error)
// so the OSS build with an empty contrib/ still works.
func loadDescriptorsFromFS(fsys fs.FS, dir string) ([]*Descriptor, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, nil
	}
	var all []*Descriptor
	seen := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !isYAMLFile(e.Name()) {
			continue
		}
		data, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded %s/%s: %w", dir, e.Name(), err)
		}
		d, err := ParseDescriptor(data)
		if err != nil {
			return nil, fmt.Errorf("parse embedded %s/%s: %w", dir, e.Name(), err)
		}
		if prev, ok := seen[d.Name]; ok {
			return nil, fmt.Errorf("duplicate descriptor name %q in %s/%s and %s/%s", d.Name, dir, prev, dir, e.Name())
		}
		seen[d.Name] = e.Name()
		all = append(all, d)
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
// sources, merges them (runtime wins on collision), and registers as adapters.
// User descriptors in ~/.schmux/adapters/ override built-in ones with the
// same name, allowing per-installation customization (e.g. prompt_strategy).
func LoadAndRegisterDescriptors(runtimeDir string) error {
	embedded, err := LoadEmbeddedDescriptors()
	if err != nil {
		return fmt.Errorf("load embedded descriptors: %w", err)
	}
	runtime, err := LoadRuntimeDescriptors(runtimeDir)
	if err != nil {
		return fmt.Errorf("load runtime descriptors: %w", err)
	}
	runtimeNames := map[string]bool{}
	for _, d := range runtime {
		runtimeNames[d.Name] = true
	}
	all := runtime
	for _, d := range embedded {
		if runtimeNames[d.Name] {
			continue // user override takes precedence
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
		// pkgLogger is not yet set at init time, so use a one-off stderr
		// logger. Without this, a malformed or colliding embedded descriptor
		// leaves the adapter registry empty and the binary appears to support
		// no tools — with no signal as to why.
		log.New(os.Stderr).Warn("failed to load embedded adapter descriptors", "err", err)
		return
	}
	_ = RegisterDescriptorAdapters(descs)
}
