package detect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func TestLoadRuntimeDescriptors(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte(`
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [interactive]
`)
	if err := os.WriteFile(filepath.Join(dir, "testool.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}
	descs, err := LoadRuntimeDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeDescriptors: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("got %d descriptors, want 1", len(descs))
	}
	if descs[0].Name != "testool" {
		t.Errorf("Name = %q", descs[0].Name)
	}
}

func TestLoadRuntimeDescriptors_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	descs, err := LoadRuntimeDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeDescriptors: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("got %d descriptors, want 0", len(descs))
	}
}

func TestLoadRuntimeDescriptors_MissingDir(t *testing.T) {
	descs, err := LoadRuntimeDescriptors("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadRuntimeDescriptors: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("got %d descriptors, want 0", len(descs))
	}
}

func TestLoadRuntimeDescriptors_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlData := []byte(`name: [invalid`)
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), yamlData, 0644); err != nil {
		t.Fatal(err)
	}
	descs, err := LoadRuntimeDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeDescriptors should skip bad files, got error: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("got %d descriptors, want 0 (bad file should be skipped)", len(descs))
	}
}

func TestLoadRuntimeDescriptors_NameCollision(t *testing.T) {
	dir := t.TempDir()
	yaml1 := []byte("name: dupe\ndetect:\n  - type: path_lookup\n    command: dupe\n")
	yaml2 := []byte("name: dupe\ndetect:\n  - type: path_lookup\n    command: dupe2\n")
	os.WriteFile(filepath.Join(dir, "a.yaml"), yaml1, 0644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), yaml2, 0644)
	descs, err := LoadRuntimeDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeDescriptors should skip duplicates, got error: %v", err)
	}
	if len(descs) != 1 {
		t.Errorf("got %d descriptors, want 1 (duplicate should be skipped)", len(descs))
	}
}

func TestLoadRuntimeDescriptors_UnknownFields(t *testing.T) {
	dir := t.TempDir()
	yamlData := []byte("name: lenient\ndetect:\n  - type: path_lookup\n    command: go\nfuture_field: some_value\n")
	if err := os.WriteFile(filepath.Join(dir, "lenient.yaml"), yamlData, 0644); err != nil {
		t.Fatal(err)
	}
	descs, err := LoadRuntimeDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeDescriptors: %v", err)
	}
	if len(descs) != 1 || descs[0].Name != "lenient" {
		t.Errorf("got %d descriptors, want 1 named 'lenient'", len(descs))
	}
}

func TestLoadEmbeddedDescriptors(t *testing.T) {
	descs, err := LoadEmbeddedDescriptors()
	if err != nil {
		t.Fatalf("LoadEmbeddedDescriptors: %v", err)
	}
	// descriptors/ contains claude.yaml, gemini.yaml, codex.yaml, and opencode.yaml; contrib/ has only .gitkeep
	if len(descs) < 4 {
		t.Errorf("got %d descriptors, want at least 4", len(descs))
	}
	wantNames := map[string]bool{"claude": false, "gemini": false, "codex": false, "opencode": false}
	for _, d := range descs {
		if _, ok := wantNames[d.Name]; ok {
			wantNames[d.Name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Errorf("expected %s descriptor in embedded descriptors", name)
		}
	}
}

// TestLoadDescriptorsFromFS_DuplicateInSameDir guards the invariant that two
// descriptor files in the same directory cannot share a name.
func TestLoadDescriptorsFromFS_DuplicateInSameDir(t *testing.T) {
	yaml := []byte("name: dupe\ndetect:\n  - type: path_lookup\n    command: dupe\ncapabilities: [interactive]\n")
	fsys := fstest.MapFS{
		"contrib/a.yaml": &fstest.MapFile{Data: yaml},
		"contrib/b.yaml": &fstest.MapFile{Data: yaml},
	}
	if _, err := loadDescriptorsFromFS(fsys, "contrib"); err == nil {
		t.Fatal("expected duplicate-name error within same dir, got nil")
	}
}

// TestLoadDescriptorsFromFS_MissingDir treats a missing directory as empty,
// not an error. Mirrors the OSS state where contrib/ has no .yaml files.
func TestLoadDescriptorsFromFS_MissingDir(t *testing.T) {
	descs, err := loadDescriptorsFromFS(fstest.MapFS{}, "contrib")
	if err != nil {
		t.Fatalf("loadDescriptorsFromFS missing dir: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("got %d descriptors, want 0", len(descs))
	}
}

// TestLoadEmbeddedDescriptorsFrom_ContribOverridesDescriptors reproduces the
// Meta-build scenario: contrib/claude.yaml and descriptors/claude.yaml coexist
// after build-schmux.sh copies the Meta override into contrib/. Contrib must
// win on name collision and additional contrib descriptors must be added.
// Before the fix this returned a duplicate-name error and init() silently
// swallowed it, leaving the adapter registry empty (no tools detected).
func TestLoadEmbeddedDescriptorsFrom_ContribOverridesDescriptors(t *testing.T) {
	ossClaude := []byte("name: claude\ndisplay_name: Claude (OSS)\ndetect:\n  - type: path_lookup\n    command: claude\ncapabilities: [interactive]\n")
	metaClaude := []byte("name: claude\ndisplay_name: Claude (Meta)\ndetect:\n  - type: path_lookup\n    command: claude\ncapabilities: [interactive]\nprompt_strategy: send_keys\n")
	metaOrc := []byte("name: orc\ndisplay_name: Orc\ndetect:\n  - type: path_lookup\n    command: orc\ncapabilities: [interactive]\n")

	descriptorsFS := fstest.MapFS{"descriptors/claude.yaml": &fstest.MapFile{Data: ossClaude}}
	contribFS := fstest.MapFS{
		"contrib/claude.yaml": &fstest.MapFile{Data: metaClaude},
		"contrib/orc.yaml":    &fstest.MapFile{Data: metaOrc},
	}

	descs, err := loadEmbeddedDescriptorsFrom(descriptorsFS, "descriptors", contribFS, "contrib")
	if err != nil {
		t.Fatalf("loadEmbeddedDescriptorsFrom: %v", err)
	}
	if len(descs) != 2 {
		t.Fatalf("got %d descriptors, want 2 (claude override + orc add)", len(descs))
	}

	byName := map[string]*Descriptor{}
	for _, d := range descs {
		byName[d.Name] = d
	}
	claude := byName["claude"]
	if claude == nil {
		t.Fatal("claude descriptor missing")
	}
	if claude.DisplayName != "Claude (Meta)" {
		t.Errorf("claude DisplayName = %q, want Meta override", claude.DisplayName)
	}
	if claude.PromptStrategy != "send_keys" {
		t.Errorf("claude PromptStrategy = %q, want send_keys (from contrib)", claude.PromptStrategy)
	}
	if byName["orc"] == nil {
		t.Error("orc descriptor missing — contrib should add new adapters too")
	}
}

// saveAndRestoreRegistries saves all mutable package-level registries and
// restores them when the test completes.
func saveAndRestoreRegistries(t *testing.T) {
	t.Helper()
	origAdapters := make(map[string]ToolAdapter)
	for k, v := range adapters {
		origAdapters[k] = v
	}
	origDescriptorNames := append([]string(nil), descriptorToolNames...)
	origInstructionConfigs := make(map[string]AgentInstructionConfig)
	for k, v := range agentInstructionConfigs {
		origInstructionConfigs[k] = v
	}
	t.Cleanup(func() {
		adapters = origAdapters
		descriptorToolNames = origDescriptorNames
		agentInstructionConfigs = origInstructionConfigs
	})
}

func TestRegisterDescriptorAdapters(t *testing.T) {
	saveAndRestoreRegistries(t)
	descs := []*Descriptor{
		{
			Name:         "testool",
			Detect:       []DetectEntry{{Type: "path_lookup", Command: "testool"}},
			Capabilities: []string{"interactive"},
		},
	}
	err := RegisterDescriptorAdapters(descs)
	if err != nil {
		t.Fatalf("RegisterDescriptorAdapters: %v", err)
	}
	a := GetAdapter("testool")
	if a == nil {
		t.Fatal("adapter not registered")
	}
	if a.Name() != "testool" {
		t.Errorf("Name = %q", a.Name())
	}
	if !IsToolName("testool") {
		t.Error("testool should be a known tool name")
	}
	if IsBuiltinToolName("testool") {
		t.Error("testool should NOT be a builtin tool name")
	}
}

func TestRegisterDescriptorAdapters_SkipsExisting(t *testing.T) {
	saveAndRestoreRegistries(t)
	descs := []*Descriptor{
		{
			Name:   "claude",
			Detect: []DetectEntry{{Type: "path_lookup", Command: "claude"}},
		},
	}
	err := RegisterDescriptorAdapters(descs)
	if err != nil {
		t.Fatalf("RegisterDescriptorAdapters should skip existing, got: %v", err)
	}
	// Verify the original adapter is still registered (not overwritten)
	a := GetAdapter("claude")
	if a == nil {
		t.Fatal("claude adapter should still be registered")
	}
}

func TestLoadAndRegisterAll(t *testing.T) {
	saveAndRestoreRegistries(t)
	dir := t.TempDir()
	yamlData := []byte(`
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [interactive]
interactive:
  base_args: ["--start"]
spawn_env:
  MY_VAR: "hello"
`)
	os.WriteFile(filepath.Join(dir, "testool.yaml"), yamlData, 0644)
	err := LoadAndRegisterDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadAndRegisterDescriptors: %v", err)
	}
	a := GetAdapter("testool")
	if a == nil {
		t.Fatal("testool not registered")
	}
	env := a.SpawnEnv(SpawnContext{})
	if env["MY_VAR"] != "hello" {
		t.Errorf("SpawnEnv = %v", env)
	}
}

// TestDescriptorAdapterDetection is an integration test that verifies the full
// pipeline: YAML descriptor → loader → GenericAdapter → DetectAvailableToolsContext.
// Uses "go" as the test tool since it is guaranteed to exist on any machine
// running Go tests.
func TestDescriptorAdapterDetection(t *testing.T) {
	saveAndRestoreRegistries(t)

	dir := t.TempDir()
	yamlData := []byte(`
name: gocheck
detect:
  - type: path_lookup
    command: go
capabilities: [interactive]
interactive:
  base_args: ["--help"]
`)
	os.WriteFile(filepath.Join(dir, "gocheck.yaml"), yamlData, 0644)

	err := LoadAndRegisterDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadAndRegisterDescriptors: %v", err)
	}

	// Verify it registered
	a := GetAdapter("gocheck")
	if a == nil {
		t.Fatal("gocheck adapter not registered")
	}

	// Run full detection — should find "gocheck" via PATH lookup for "go"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tools, err := DetectAvailableToolsContext(ctx, false)
	if err != nil {
		t.Fatalf("DetectAvailableToolsContext: %v", err)
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "gocheck" {
			found = true
			if tool.Command != "go" {
				t.Errorf("gocheck command = %q, want %q", tool.Command, "go")
			}
			if tool.Source != "PATH" {
				t.Errorf("gocheck source = %q, want %q", tool.Source, "PATH")
			}
			break
		}
	}
	if !found {
		t.Error("gocheck not found in DetectAvailableToolsContext results")
	}
}
