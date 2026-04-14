package detect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
	yaml := []byte(`name: [invalid`)
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRuntimeDescriptors(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadRuntimeDescriptors_NameCollision(t *testing.T) {
	dir := t.TempDir()
	yaml1 := []byte("name: dupe\ndetect:\n  - type: path_lookup\n    command: dupe\n")
	yaml2 := []byte("name: dupe\ndetect:\n  - type: path_lookup\n    command: dupe2\n")
	os.WriteFile(filepath.Join(dir, "a.yaml"), yaml1, 0644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), yaml2, 0644)
	_, err := LoadRuntimeDescriptors(dir)
	if err == nil {
		t.Fatal("expected error for name collision")
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

	// Verify builtins still detected alongside the descriptor adapter
	builtinFound := 0
	for _, tool := range tools {
		if IsBuiltinToolName(tool.Name) {
			builtinFound++
		}
	}
	if builtinFound == 0 {
		t.Error("no builtin tools detected — descriptor registration may have broken builtin detection")
	}
}
