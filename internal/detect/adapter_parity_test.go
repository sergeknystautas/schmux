package detect

import (
	"os"
	"path/filepath"
	"testing"
)

// loadDescriptorAdapter removes the Go adapter for the given name,
// loads the embedded YAML descriptor, and registers it instead.
// Uses saveAndRestoreRegistries for cleanup.
func loadDescriptorAdapter(t *testing.T, name string) {
	t.Helper()
	saveAndRestoreRegistries(t)
	delete(adapters, name)
	descs, err := LoadEmbeddedDescriptors()
	if err != nil {
		t.Fatalf("LoadEmbeddedDescriptors: %v", err)
	}
	for _, d := range descs {
		if d.Name == name {
			if err := RegisterDescriptorAdapters([]*Descriptor{d}); err != nil {
				t.Fatalf("RegisterDescriptorAdapters: %v", err)
			}
			return
		}
	}
	t.Fatalf("descriptor %q not found in embedded descriptors", name)
}

func TestCodexParity(t *testing.T) {
	loadDescriptorAdapter(t, "codex")
	a := GetAdapter("codex")
	if a == nil {
		t.Fatal("codex adapter not registered")
	}

	if a.Name() != "codex" {
		t.Errorf("Name = %q", a.Name())
	}
	if caps := a.Capabilities(); len(caps) != 2 || caps[0] != "interactive" || caps[1] != "oneshot" {
		t.Errorf("Capabilities = %v", caps)
	}
	if a.ModelFlag() != "-m" {
		t.Errorf("ModelFlag = %q", a.ModelFlag())
	}

	cfg := a.InstructionConfig()
	if cfg.InstructionDir != ".codex" || cfg.InstructionFile != "AGENTS.md" {
		t.Errorf("InstructionConfig = %+v", cfg)
	}

	if a.SignalingStrategy() != SignalingCLIFlag {
		t.Errorf("SignalingStrategy = %v", a.SignalingStrategy())
	}
	if a.PersonaInjection() != PersonaInstructionFile {
		t.Errorf("PersonaInjection = %v", a.PersonaInjection())
	}
	if a.SupportsHooks() {
		t.Error("SupportsHooks should be false")
	}

	// InteractiveArgs — no model, no resume
	if args := a.InteractiveArgs(nil, false); len(args) != 0 {
		t.Errorf("InteractiveArgs(nil, false) = %v, want empty", args)
	}

	// InteractiveArgs — resume
	args := a.InteractiveArgs(nil, true)
	if len(args) != 2 || args[0] != "resume" || args[1] != "--last" {
		t.Errorf("InteractiveArgs(nil, true) = %v", args)
	}

	// InteractiveArgs — with model
	model := &Model{Runners: map[string]RunnerSpec{
		"codex": {ModelValue: "gpt-4"},
	}}
	args = a.InteractiveArgs(model, false)
	if len(args) != 2 || args[0] != "-m" || args[1] != "gpt-4" {
		t.Errorf("InteractiveArgs(model, false) = %v", args)
	}

	// OneshotArgs — no model, no schema
	oArgs, err := a.OneshotArgs(nil, "")
	if err != nil {
		t.Errorf("OneshotArgs(nil, \"\") error: %v", err)
	}
	if len(oArgs) != 2 || oArgs[0] != "exec" || oArgs[1] != "--json" {
		t.Errorf("OneshotArgs(nil, \"\") = %v, want [exec --json]", oArgs)
	}

	// OneshotArgs — with model, with schema
	schema := `{"type":"object"}`
	oArgs, err = a.OneshotArgs(model, schema)
	if err != nil {
		t.Errorf("OneshotArgs(model, schema) error: %v", err)
	}
	want := []string{"exec", "--json", "-m", "gpt-4", "--output-schema", schema}
	if len(oArgs) != len(want) {
		t.Errorf("OneshotArgs(model, schema) = %v, want %v", oArgs, want)
	} else {
		for i := range want {
			if oArgs[i] != want[i] {
				t.Errorf("OneshotArgs(model, schema)[%d] = %q, want %q", i, oArgs[i], want[i])
			}
		}
	}

	// OneshotArgs — no model, with schema (model stripped)
	oArgs, err = a.OneshotArgs(nil, schema)
	if err != nil {
		t.Errorf("OneshotArgs(nil, schema) error: %v", err)
	}
	wantNoModel := []string{"exec", "--json", "--output-schema", schema}
	if len(oArgs) != len(wantNoModel) {
		t.Errorf("OneshotArgs(nil, schema) = %v, want %v", oArgs, wantNoModel)
	} else {
		for i := range wantNoModel {
			if oArgs[i] != wantNoModel[i] {
				t.Errorf("OneshotArgs(nil, schema)[%d] = %q, want %q", i, oArgs[i], wantNoModel[i])
			}
		}
	}

	// SignalingArgs
	sigArgs := a.SignalingArgs("/tmp/s.md")
	if len(sigArgs) != 2 || sigArgs[0] != "-c" || sigArgs[1] != "model_instructions_file=/tmp/s.md" {
		t.Errorf("SignalingArgs = %v", sigArgs)
	}

	// SpawnEnv — nil
	if env := a.SpawnEnv(SpawnContext{}); env != nil {
		t.Errorf("SpawnEnv = %v", env)
	}

	// BuildRunnerEnv — nil/empty
	if env := a.BuildRunnerEnv(RunnerSpec{}); len(env) != 0 {
		t.Errorf("BuildRunnerEnv = %v", env)
	}
}

func TestGeminiParity(t *testing.T) {
	loadDescriptorAdapter(t, "gemini")
	a := GetAdapter("gemini")
	if a == nil {
		t.Fatal("gemini adapter not registered")
	}

	if a.Name() != "gemini" {
		t.Errorf("Name = %q", a.Name())
	}
	if caps := a.Capabilities(); len(caps) != 1 || caps[0] != "interactive" {
		t.Errorf("Capabilities = %v", caps)
	}
	if a.ModelFlag() != "--model" {
		t.Errorf("ModelFlag = %q", a.ModelFlag())
	}

	cfg := a.InstructionConfig()
	if cfg.InstructionDir != ".gemini" || cfg.InstructionFile != "GEMINI.md" {
		t.Errorf("InstructionConfig = %+v", cfg)
	}

	if a.SignalingStrategy() != SignalingInstructionFile {
		t.Errorf("SignalingStrategy = %v", a.SignalingStrategy())
	}
	if a.PersonaInjection() != PersonaInstructionFile {
		t.Errorf("PersonaInjection = %v", a.PersonaInjection())
	}
	if a.SupportsHooks() {
		t.Error("SupportsHooks should be false")
	}

	// InteractiveArgs — no model, no resume
	if args := a.InteractiveArgs(nil, false); len(args) != 0 {
		t.Errorf("InteractiveArgs(nil, false) = %v, want empty", args)
	}

	// InteractiveArgs — resume
	args := a.InteractiveArgs(nil, true)
	if len(args) != 2 || args[0] != "-r" || args[1] != "latest" {
		t.Errorf("InteractiveArgs(nil, true) = %v", args)
	}

	// InteractiveArgs — with model
	model := &Model{Runners: map[string]RunnerSpec{
		"gemini": {ModelValue: "gemini-2.5-pro"},
	}}
	args = a.InteractiveArgs(model, false)
	if len(args) != 2 || args[0] != "--model" || args[1] != "gemini-2.5-pro" {
		t.Errorf("InteractiveArgs(model) = %v", args)
	}

	// OneshotArgs — should error
	if _, err := a.OneshotArgs(nil, ""); err == nil {
		t.Error("OneshotArgs should error")
	}

	// SpawnEnv — nil
	if env := a.SpawnEnv(SpawnContext{}); env != nil {
		t.Errorf("SpawnEnv = %v", env)
	}

	// BuildRunnerEnv — nil/empty
	if env := a.BuildRunnerEnv(RunnerSpec{}); len(env) != 0 {
		t.Errorf("BuildRunnerEnv = %v", env)
	}
}

func TestOpencodeParity(t *testing.T) {
	loadDescriptorAdapter(t, "opencode")
	a := GetAdapter("opencode")
	if a == nil {
		t.Fatal("opencode adapter not registered")
	}

	// --- Name, Capabilities, ModelFlag ---
	if a.Name() != "opencode" {
		t.Errorf("Name = %q", a.Name())
	}
	if caps := a.Capabilities(); len(caps) != 2 || caps[0] != "interactive" || caps[1] != "oneshot" {
		t.Errorf("Capabilities = %v", caps)
	}
	if a.ModelFlag() != "--model" {
		t.Errorf("ModelFlag = %q", a.ModelFlag())
	}

	// --- InstructionConfig ---
	cfg := a.InstructionConfig()
	if cfg.InstructionDir != ".opencode" || cfg.InstructionFile != "AGENTS.md" {
		t.Errorf("InstructionConfig = %+v", cfg)
	}

	// --- SignalingStrategy ---
	if a.SignalingStrategy() != SignalingInstructionFile {
		t.Errorf("SignalingStrategy = %v", a.SignalingStrategy())
	}

	// --- PersonaInjection ---
	if a.PersonaInjection() != PersonaConfigOverlay {
		t.Errorf("PersonaInjection = %v", a.PersonaInjection())
	}

	// --- SupportsHooks ---
	if !a.SupportsHooks() {
		t.Error("SupportsHooks should be true")
	}

	// --- InteractiveArgs: nil/no resume ---
	if args := a.InteractiveArgs(nil, false); len(args) != 0 {
		t.Errorf("InteractiveArgs(nil, false) = %v, want empty", args)
	}

	// --- InteractiveArgs: resume ---
	args := a.InteractiveArgs(nil, true)
	if len(args) != 1 || args[0] != "--continue" {
		t.Errorf("InteractiveArgs(nil, true) = %v, want [--continue]", args)
	}

	// --- InteractiveArgs: with model ---
	model := &Model{Runners: map[string]RunnerSpec{
		"opencode": {ModelValue: "gpt-4"},
	}}
	args = a.InteractiveArgs(model, false)
	if len(args) != 2 || args[0] != "--model" || args[1] != "gpt-4" {
		t.Errorf("InteractiveArgs(model, false) = %v, want [--model gpt-4]", args)
	}

	// --- OneshotArgs: no model, no schema ---
	oArgs, err := a.OneshotArgs(nil, "")
	if err != nil {
		t.Errorf("OneshotArgs(nil, \"\") error: %v", err)
	}
	if len(oArgs) != 1 || oArgs[0] != "run" {
		t.Errorf("OneshotArgs(nil, \"\") = %v, want [run]", oArgs)
	}

	// --- OneshotArgs: with model, no schema ---
	oArgs, err = a.OneshotArgs(model, "")
	if err != nil {
		t.Errorf("OneshotArgs(model, \"\") error: %v", err)
	}
	if len(oArgs) != 3 || oArgs[0] != "run" || oArgs[1] != "--model" || oArgs[2] != "gpt-4" {
		t.Errorf("OneshotArgs(model, \"\") = %v, want [run --model gpt-4]", oArgs)
	}

	// --- OneshotArgs: no model, with schema ---
	schema := `{"type":"object"}`
	oArgs, err = a.OneshotArgs(nil, schema)
	if err != nil {
		t.Errorf("OneshotArgs(nil, schema) error: %v", err)
	}
	wantNoModel := []string{"run", "--format", "json"}
	if len(oArgs) != len(wantNoModel) {
		t.Errorf("OneshotArgs(nil, schema) = %v, want %v", oArgs, wantNoModel)
	} else {
		for i := range wantNoModel {
			if oArgs[i] != wantNoModel[i] {
				t.Errorf("OneshotArgs(nil, schema)[%d] = %q, want %q", i, oArgs[i], wantNoModel[i])
			}
		}
	}

	// --- OneshotArgs: with model, with schema ---
	oArgs, err = a.OneshotArgs(model, schema)
	if err != nil {
		t.Errorf("OneshotArgs(model, schema) error: %v", err)
	}
	want := []string{"run", "--model", "gpt-4", "--format", "json"}
	if len(oArgs) != len(want) {
		t.Errorf("OneshotArgs(model, schema) = %v, want %v", oArgs, want)
	} else {
		for i := range want {
			if oArgs[i] != want[i] {
				t.Errorf("OneshotArgs(model, schema)[%d] = %q, want %q", i, oArgs[i], want[i])
			}
		}
	}

	// --- SpawnEnv: nil when no persona ---
	if env := a.SpawnEnv(SpawnContext{}); env != nil {
		t.Errorf("SpawnEnv(empty) = %v, want nil", env)
	}

	// --- SpawnEnv: OPENCODE_CONFIG_CONTENT when persona set ---
	env := a.SpawnEnv(SpawnContext{PersonaPath: "/tmp/persona.md"})
	if env == nil {
		t.Fatal("SpawnEnv(persona) should not be nil")
	}
	wantEnv := `{"instructions":["/tmp/persona.md"]}`
	if env["OPENCODE_CONFIG_CONTENT"] != wantEnv {
		t.Errorf("SpawnEnv OPENCODE_CONFIG_CONTENT = %q, want %q", env["OPENCODE_CONFIG_CONTENT"], wantEnv)
	}

	// --- Skill injection: file_pattern ---
	tmpDir := t.TempDir()
	skill := SkillModule{Name: "test-skill", Content: "# Test Skill\nDo the thing."}
	if err := a.InjectSkill(tmpDir, skill); err != nil {
		t.Fatalf("InjectSkill: %v", err)
	}
	skillPath := filepath.Join(tmpDir, ".opencode", "commands", "schmux-test-skill.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("skill file not found: %v", err)
	}
	if string(data) != skill.Content {
		t.Errorf("skill content = %q, want %q", string(data), skill.Content)
	}
	if err := a.RemoveSkill(tmpDir, "test-skill"); err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Errorf("skill file should be removed")
	}

	// --- SetupCommands: writes commit.md ---
	setupDir := t.TempDir()
	if err := a.SetupCommands(setupDir); err != nil {
		t.Fatalf("SetupCommands: %v", err)
	}
	commitPath := filepath.Join(setupDir, ".opencode", "commands", "commit.md")
	if _, err := os.Stat(commitPath); os.IsNotExist(err) {
		t.Errorf("commit.md not created at %s", commitPath)
	}

	// --- BuildRunnerEnv: nil/empty ---
	if benv := a.BuildRunnerEnv(RunnerSpec{}); len(benv) != 0 {
		t.Errorf("BuildRunnerEnv = %v", benv)
	}
}

func TestClaudeParity(t *testing.T) {
	loadDescriptorAdapter(t, "claude")
	a := GetAdapter("claude")
	if a == nil {
		t.Fatal("claude adapter not registered")
	}

	// --- Name, Capabilities, ModelFlag ---
	if a.Name() != "claude" {
		t.Errorf("Name = %q", a.Name())
	}
	if caps := a.Capabilities(); len(caps) != 2 || caps[0] != "interactive" || caps[1] != "oneshot" {
		t.Errorf("Capabilities = %v", caps)
	}
	if a.ModelFlag() != "--model" {
		t.Errorf("ModelFlag = %q", a.ModelFlag())
	}

	// --- InstructionConfig ---
	cfg := a.InstructionConfig()
	if cfg.InstructionDir != ".claude" || cfg.InstructionFile != "CLAUDE.md" {
		t.Errorf("InstructionConfig = %+v", cfg)
	}

	// --- SignalingStrategy ---
	if a.SignalingStrategy() != SignalingHooks {
		t.Errorf("SignalingStrategy = %v, want SignalingHooks", a.SignalingStrategy())
	}

	// --- PersonaInjection ---
	if a.PersonaInjection() != PersonaCLIFlag {
		t.Errorf("PersonaInjection = %v, want PersonaCLIFlag", a.PersonaInjection())
	}

	// --- PersonaArgs: empty path returns nil ---
	if pargs := a.PersonaArgs(""); pargs != nil {
		t.Errorf("PersonaArgs(\"\") = %v, want nil", pargs)
	}

	// --- PersonaArgs: non-empty path ---
	pargs := a.PersonaArgs("/tmp/persona.md")
	if len(pargs) != 2 || pargs[0] != "--append-system-prompt-file" || pargs[1] != "/tmp/persona.md" {
		t.Errorf("PersonaArgs(\"/tmp/persona.md\") = %v, want [--append-system-prompt-file /tmp/persona.md]", pargs)
	}

	// --- SupportsHooks ---
	if !a.SupportsHooks() {
		t.Error("SupportsHooks should be true")
	}

	// --- InteractiveArgs: nil model, no resume ---
	if args := a.InteractiveArgs(nil, false); len(args) != 0 {
		t.Errorf("InteractiveArgs(nil, false) = %v, want empty", args)
	}

	// --- InteractiveArgs: resume ---
	args := a.InteractiveArgs(nil, true)
	if len(args) != 1 || args[0] != "--continue" {
		t.Errorf("InteractiveArgs(nil, true) = %v, want [--continue]", args)
	}

	// --- InteractiveArgs: with model ---
	model := &Model{Runners: map[string]RunnerSpec{
		"claude": {ModelValue: "claude-opus-4-20250514"},
	}}
	args = a.InteractiveArgs(model, false)
	if len(args) != 2 || args[0] != "--model" || args[1] != "claude-opus-4-20250514" {
		t.Errorf("InteractiveArgs(model, false) = %v, want [--model claude-opus-4-20250514]", args)
	}

	// --- OneshotArgs: no model, no schema ---
	oArgs, err := a.OneshotArgs(nil, "")
	if err != nil {
		t.Errorf("OneshotArgs(nil, \"\") error: %v", err)
	}
	wantOneshot := []string{"-p", "--dangerously-skip-permissions", "--output-format", "json"}
	if len(oArgs) != len(wantOneshot) {
		t.Errorf("OneshotArgs(nil, \"\") = %v, want %v", oArgs, wantOneshot)
	} else {
		for i := range wantOneshot {
			if oArgs[i] != wantOneshot[i] {
				t.Errorf("OneshotArgs(nil, \"\")[%d] = %q, want %q", i, oArgs[i], wantOneshot[i])
			}
		}
	}

	// --- OneshotArgs: with model, no schema (model MUST be ignored) ---
	oArgs, err = a.OneshotArgs(model, "")
	if err != nil {
		t.Errorf("OneshotArgs(model, \"\") error: %v", err)
	}
	// Model is never added to oneshot args
	if len(oArgs) != len(wantOneshot) {
		t.Errorf("OneshotArgs(model, \"\") = %v, want %v (model should be ignored)", oArgs, wantOneshot)
	} else {
		for i := range wantOneshot {
			if oArgs[i] != wantOneshot[i] {
				t.Errorf("OneshotArgs(model, \"\")[%d] = %q, want %q", i, oArgs[i], wantOneshot[i])
			}
		}
	}

	// --- OneshotArgs: no model, with schema ---
	schema := `{"type":"object"}`
	oArgs, err = a.OneshotArgs(nil, schema)
	if err != nil {
		t.Errorf("OneshotArgs(nil, schema) error: %v", err)
	}
	wantOneshotSchema := []string{"-p", "--dangerously-skip-permissions", "--output-format", "json", "--json-schema", schema}
	if len(oArgs) != len(wantOneshotSchema) {
		t.Errorf("OneshotArgs(nil, schema) = %v, want %v", oArgs, wantOneshotSchema)
	} else {
		for i := range wantOneshotSchema {
			if oArgs[i] != wantOneshotSchema[i] {
				t.Errorf("OneshotArgs(nil, schema)[%d] = %q, want %q", i, oArgs[i], wantOneshotSchema[i])
			}
		}
	}

	// --- OneshotArgs: with model AND schema (model still MUST be ignored) ---
	oArgs, err = a.OneshotArgs(model, schema)
	if err != nil {
		t.Errorf("OneshotArgs(model, schema) error: %v", err)
	}
	if len(oArgs) != len(wantOneshotSchema) {
		t.Errorf("OneshotArgs(model, schema) = %v, want %v (model should be ignored)", oArgs, wantOneshotSchema)
	} else {
		for i := range wantOneshotSchema {
			if oArgs[i] != wantOneshotSchema[i] {
				t.Errorf("OneshotArgs(model, schema)[%d] = %q, want %q", i, oArgs[i], wantOneshotSchema[i])
			}
		}
	}

	// --- SpawnEnv: nil ---
	if env := a.SpawnEnv(SpawnContext{}); env != nil {
		t.Errorf("SpawnEnv = %v, want nil", env)
	}

	// --- SetupCommands: no-op ---
	tmpDir := t.TempDir()
	if err := a.SetupCommands(tmpDir); err != nil {
		t.Fatalf("SetupCommands: %v", err)
	}

	// --- Skill injection: dir_pattern ---
	skill := SkillModule{Name: "code-review", Content: "# Code Review\nReview the PR."}
	if err := a.InjectSkill(tmpDir, skill); err != nil {
		t.Fatalf("InjectSkill: %v", err)
	}
	skillPath := filepath.Join(tmpDir, ".claude", "skills", "schmux-code-review", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("skill file not found: %v", err)
	}
	if string(data) != skill.Content {
		t.Errorf("skill content = %q, want %q", string(data), skill.Content)
	}
	if err := a.RemoveSkill(tmpDir, "code-review"); err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Error("skill file should be removed after RemoveSkill")
	}
	// Verify the parent directory is also removed
	skillDir := filepath.Join(tmpDir, ".claude", "skills", "schmux-code-review")
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("skill directory should be removed after RemoveSkill")
	}

	// --- BuildRunnerEnv: expands runner_env.when_endpoint when Endpoint is set ---
	if benv := a.BuildRunnerEnv(RunnerSpec{}); len(benv) != 0 {
		t.Errorf("BuildRunnerEnv(empty) = %v, want nil/empty", benv)
	}
	if benv := a.BuildRunnerEnv(RunnerSpec{ModelValue: "native"}); len(benv) != 0 {
		t.Errorf("BuildRunnerEnv(no endpoint) = %v, want nil/empty for native claude", benv)
	}
	benv := a.BuildRunnerEnv(RunnerSpec{ModelValue: "test", Endpoint: "http://example.com"})
	wantBEnv := map[string]string{
		"ANTHROPIC_BASE_URL":             "http://example.com",
		"ANTHROPIC_MODEL":                "test",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "test",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "test",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "test",
		"CLAUDE_CODE_SUBAGENT_MODEL":     "test",
	}
	for k, want := range wantBEnv {
		if got := benv[k]; got != want {
			t.Errorf("BuildRunnerEnv(endpoint)[%q] = %q, want %q", k, got, want)
		}
	}
	if len(benv) != len(wantBEnv) {
		t.Errorf("BuildRunnerEnv(endpoint) has %d entries, want %d: %v", len(benv), len(wantBEnv), benv)
	}
}

func TestGitExcludePatterns(t *testing.T) {
	patterns := AllGitExcludePatterns()
	expected := map[string]bool{
		".claude/settings.local.json":    false,
		".claude/skills/schmux-*/":       false,
		".opencode/plugins/schmux.ts":    false,
		".opencode/commands/schmux-*.md": false,
		".opencode/commands/commit.md":   false,
	}
	for _, p := range patterns {
		if _, ok := expected[p]; ok {
			expected[p] = true
		}
	}
	for pattern, found := range expected {
		if !found {
			t.Errorf("expected pattern %q not found in AllGitExcludePatterns()", pattern)
		}
	}
}
