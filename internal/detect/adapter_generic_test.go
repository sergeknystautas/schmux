package detect

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSignalingNoneIsDistinct(t *testing.T) {
	if SignalingNone == SignalingHooks {
		t.Fatal("SignalingNone must not equal SignalingHooks")
	}
	if SignalingNone == SignalingCLIFlag {
		t.Fatal("SignalingNone must not equal SignalingCLIFlag")
	}
	if SignalingNone == SignalingInstructionFile {
		t.Fatal("SignalingNone must not equal SignalingInstructionFile")
	}
}

func TestPersonaNoneIsDistinct(t *testing.T) {
	if PersonaNone == PersonaCLIFlag {
		t.Fatal("PersonaNone must not equal PersonaCLIFlag")
	}
	if PersonaNone == PersonaInstructionFile {
		t.Fatal("PersonaNone must not equal PersonaInstructionFile")
	}
	if PersonaNone == PersonaConfigOverlay {
		t.Fatal("PersonaNone must not equal PersonaConfigOverlay")
	}
}

func TestGenericAdapter_Minimal(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [interactive]
interactive:
  base_args: ["--run"]
`
	d, err := ParseDescriptor([]byte(yamlData))
	if err != nil {
		t.Fatalf("ParseDescriptor: %v", err)
	}
	a, err := NewGenericAdapter(d)
	if err != nil {
		t.Fatalf("NewGenericAdapter: %v", err)
	}
	if a.Name() != "testool" {
		t.Errorf("Name = %q", a.Name())
	}
	if a.ModelFlag() != "" {
		t.Errorf("ModelFlag = %q, want empty", a.ModelFlag())
	}
	caps := a.Capabilities()
	if len(caps) != 1 || caps[0] != "interactive" {
		t.Errorf("Capabilities = %v", caps)
	}
	if a.SignalingStrategy() != SignalingNone {
		t.Errorf("SignalingStrategy = %v, want SignalingNone", a.SignalingStrategy())
	}
	if a.PersonaInjection() != PersonaNone {
		t.Errorf("PersonaInjection = %v, want PersonaNone", a.PersonaInjection())
	}
	if a.SupportsHooks() {
		t.Error("SupportsHooks should be false for none strategy")
	}
	args := a.InteractiveArgs(nil, false)
	if len(args) != 1 || args[0] != "--run" {
		t.Errorf("InteractiveArgs = %v", args)
	}
}

func TestGenericAdapter_ResumeArgs(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
interactive:
  base_args: ["--run"]
  resume_args: ["resume", "--last"]
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	args := a.InteractiveArgs(nil, true)
	if len(args) != 2 || args[0] != "resume" || args[1] != "--last" {
		t.Errorf("InteractiveArgs(resume) = %v, want [resume --last]", args)
	}
}

func TestGenericAdapter_ModelPlaceholder(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [oneshot]
oneshot:
  base_args: ["exec", "--json", "-m", "{model}", "--output-schema"]
  schema_flag: "--schema"
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	model := &Model{Runners: map[string]RunnerSpec{"testool": {ModelValue: "gpt-5"}}}
	args, err := a.OneshotArgs(model, `{"type":"object"}`)
	if err != nil {
		t.Fatalf("OneshotArgs: %v", err)
	}
	expected := []string{"exec", "--json", "-m", "gpt-5", "--output-schema", "--schema", `{"type":"object"}`}
	if len(args) != len(expected) {
		t.Fatalf("OneshotArgs = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("OneshotArgs[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestGenericAdapter_SpawnEnv(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
spawn_env:
  FOO: bar
  BAZ: qux
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	env := a.SpawnEnv(SpawnContext{})
	if env["FOO"] != "bar" || env["BAZ"] != "qux" {
		t.Errorf("SpawnEnv = %v", env)
	}
}

func TestGenericAdapter_SkillInjection_DirPattern(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
skills:
  dir_pattern: ".testool/skills/schmux-{name}"
  file_name: "SKILL.md"
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	dir := t.TempDir()
	err := a.InjectSkill(dir, SkillModule{Name: "greeting", Content: "Hello!"})
	if err != nil {
		t.Fatalf("InjectSkill: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, ".testool", "skills", "schmux-greeting", "SKILL.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "Hello!" {
		t.Errorf("Skill content = %q", string(content))
	}
	err = a.RemoveSkill(dir, "greeting")
	if err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".testool", "skills", "schmux-greeting")); !os.IsNotExist(err) {
		t.Error("skill directory should be removed")
	}
}

func TestGenericAdapter_SkillInjection_FilePattern(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
skills:
  file_pattern: ".testool/commands/schmux-{name}.md"
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	dir := t.TempDir()
	err := a.InjectSkill(dir, SkillModule{Name: "greeting", Content: "Hello!"})
	if err != nil {
		t.Fatalf("InjectSkill: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, ".testool", "commands", "schmux-greeting.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "Hello!" {
		t.Errorf("Skill content = %q", string(content))
	}
}

func TestGenericAdapter_InstructionConfig(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
instruction:
  dir: ".testool"
  file: "INSTRUCTIONS.md"
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	cfg := a.InstructionConfig()
	if cfg.InstructionDir != ".testool" || cfg.InstructionFile != "INSTRUCTIONS.md" {
		t.Errorf("InstructionConfig = %+v", cfg)
	}
}

func TestGenericAdapter_SignalingCLIFlag(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
signaling:
  strategy: cli_flag
  flag: "-c"
  value_template: "instructions_file={path}"
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	if a.SignalingStrategy() != SignalingCLIFlag {
		t.Errorf("SignalingStrategy = %v", a.SignalingStrategy())
	}
	args := a.SignalingArgs("/tmp/signal.md")
	if len(args) != 2 || args[0] != "-c" || args[1] != "instructions_file=/tmp/signal.md" {
		t.Errorf("SignalingArgs = %v", args)
	}
}

func TestGenericAdapter_PersonaCLIFlag(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
persona:
  strategy: cli_flag
  flag: "--system-prompt"
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	if a.PersonaInjection() != PersonaCLIFlag {
		t.Errorf("PersonaInjection = %v", a.PersonaInjection())
	}
	args := a.PersonaArgs("/tmp/persona.md")
	if len(args) != 2 || args[0] != "--system-prompt" {
		t.Errorf("PersonaArgs = %v", args)
	}
	if a.PersonaArgs("") != nil {
		t.Error("PersonaArgs('') should return nil")
	}
}

func TestGenericAdapter_ModelFlagAppended(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [interactive]
model_flag: "--model"
interactive:
  base_args: ["--run"]
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)

	model := &Model{Runners: map[string]RunnerSpec{"testool": {ModelValue: "gpt-5"}}}
	args := a.InteractiveArgs(model, false)
	// No {model} placeholder in base_args, so --model gpt-5 should be appended
	expected := []string{"--run", "--model", "gpt-5"}
	if len(args) != len(expected) {
		t.Fatalf("InteractiveArgs = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("InteractiveArgs[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestGenericAdapter_DetectWithCommandArgs(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: go
command_args: ["-help"]
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	tool, found := a.Detect(context.Background())
	if !found {
		t.Fatal("expected to find 'go' in PATH")
	}
	if tool.Command != "go -help" {
		t.Errorf("Command = %q, want %q", tool.Command, "go -help")
	}
}

func TestGenericAdapter_SpawnEnv_Nil(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	env := a.SpawnEnv(SpawnContext{})
	if env != nil {
		t.Errorf("SpawnEnv = %v, want nil", env)
	}
}

func TestGenericAdapter_PerModeModelFlag_Disabled(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [interactive, oneshot]
model_flag: "--model"
interactive:
  base_args: ["--start"]
oneshot:
  model_flag: "-"
  base_args: ["-p", "--output-format", "json"]
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	model := &Model{Runners: map[string]RunnerSpec{"testool": {ModelValue: "gpt-5"}}}

	iArgs := a.InteractiveArgs(model, false)
	if len(iArgs) != 3 || iArgs[1] != "--model" {
		t.Errorf("InteractiveArgs = %v, want model appended", iArgs)
	}

	oArgs, _ := a.OneshotArgs(model, "")
	for _, arg := range oArgs {
		if arg == "--model" || arg == "gpt-5" {
			t.Errorf("OneshotArgs = %v, should not contain model", oArgs)
		}
	}
}

func TestGenericAdapter_SchemaArgs(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [oneshot]
oneshot:
  base_args: ["run"]
  schema_args: ["--format", "json"]
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)

	args, _ := a.OneshotArgs(nil, `{"type":"object"}`)
	expected := []string{"run", "--format", "json"}
	if len(args) != len(expected) {
		t.Fatalf("OneshotArgs(with schema) = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], expected[i])
		}
	}

	args, _ = a.OneshotArgs(nil, "")
	if len(args) != 1 || args[0] != "run" {
		t.Errorf("OneshotArgs(no schema) = %v, want [run]", args)
	}
}

func TestGenericAdapter_ModelPlaceholder_NoModel(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [oneshot]
oneshot:
  base_args: ["exec", "--json", "-m", "{model}"]
  schema_flag: "--output-schema"
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	args, _ := a.OneshotArgs(nil, `{"type":"object"}`)
	expected := []string{"exec", "--json", "--output-schema", `{"type":"object"}`}
	if len(args) != len(expected) {
		t.Fatalf("OneshotArgs = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestGenericAdapterAutoApproveArgs(t *testing.T) {
	yamlData := []byte(`
name: testtool
detect:
  - type: path_lookup
    command: testtool
auto_approve_args: ['--yolo', '-y']
`)
	d, err := ParseDescriptor(yamlData)
	if err != nil {
		t.Fatalf("ParseDescriptor: %v", err)
	}
	a, err := NewGenericAdapter(d)
	if err != nil {
		t.Fatalf("NewGenericAdapter: %v", err)
	}
	got := a.AutoApproveArgs()
	if len(got) != 2 || got[0] != "--yolo" || got[1] != "-y" {
		t.Errorf("AutoApproveArgs() = %v, want [--yolo -y]", got)
	}
}

func TestGenericAdapterAutoApproveArgsEmpty(t *testing.T) {
	yamlData := []byte(`
name: noflag
detect:
  - type: path_lookup
    command: noflag
`)
	d, _ := ParseDescriptor(yamlData)
	a, _ := NewGenericAdapter(d)
	if got := a.AutoApproveArgs(); len(got) != 0 {
		t.Errorf("AutoApproveArgs() = %v, want empty", got)
	}
}

func TestGenericAdapter_ResumeIDArgs(t *testing.T) {
	d := &Descriptor{Name: "claude", Interactive: &ModeDesc{
		ResumeIDArgs: []string{"--resume", "{resume_id}"},
	}}
	a, err := NewGenericAdapter(d)
	if err != nil {
		t.Fatal(err)
	}
	got := a.ResumeIDArgs(nil, "conv-abc")
	want := []string{"--resume", "conv-abc"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ResumeIDArgs = %v, want %v", got, want)
	}

	none, _ := NewGenericAdapter(&Descriptor{Name: "x", Interactive: &ModeDesc{}})
	if none.ResumeIDArgs(nil, "conv-abc") != nil {
		t.Fatal("no resume_id_args should return nil")
	}
}

// recordingHookStrategy captures the HookContext it was handed so tests can
// assert what the adapter passes down.
type recordingHookStrategy struct{ got HookContext }

func (r *recordingHookStrategy) SupportsHooks() bool                          { return true }
func (r *recordingHookStrategy) SetupHooks(ctx HookContext) error             { r.got = ctx; return nil }
func (r *recordingHookStrategy) CleanupHooks(_ string) error                  { return nil }
func (r *recordingHookStrategy) WrapRemoteCommand(cmd string) (string, error) { return cmd, nil }

// TestGenericAdapterSetupHooksPassesDescriptorHooks pins the WHERE/HOW split:
// the descriptor says where to inject, the strategy says how.
func TestGenericAdapterSetupHooksPassesDescriptorHooks(t *testing.T) {
	rec := &recordingHookStrategy{}
	RegisterHookStrategy("test-recording-hooks", rec)
	desc := &Descriptor{
		Name:  "probe-tool",
		Hooks: &HooksDesc{Strategy: "test-recording-hooks", SettingsFile: "~/.probe/hooks.json", OwnershipPrefix: "probe:"},
	}
	a, err := NewGenericAdapter(desc)
	if err != nil {
		t.Fatalf("NewGenericAdapter: %v", err)
	}
	if err := a.SetupHooks(HookContext{WorkspacePath: "/ws"}); err != nil {
		t.Fatalf("SetupHooks: %v", err)
	}
	if rec.got.Hooks != desc.Hooks {
		t.Errorf("strategy received Hooks = %+v, want the adapter's own descriptor Hooks %+v", rec.got.Hooks, desc.Hooks)
	}
	if rec.got.WorkspacePath != "/ws" {
		t.Errorf("strategy received WorkspacePath = %q, want /ws", rec.got.WorkspacePath)
	}
}

// TestGitExcludePatterns_HomeAbsoluteSettingsFileExcluded pins that a
// harness-global settings file never becomes a workspace git exclude.
func TestGitExcludePatterns_HomeAbsoluteSettingsFileExcluded(t *testing.T) {
	desc := &Descriptor{
		Name:        "probe-tool",
		Instruction: &InstructionDesc{Dir: ".probe"},
		Hooks:       &HooksDesc{SettingsFile: "~/.probe/hooks.json"},
	}
	a, err := NewGenericAdapter(desc)
	if err != nil {
		t.Fatalf("NewGenericAdapter: %v", err)
	}
	got := a.GitExcludePatterns()
	want := []string{".probe/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GitExcludePatterns() = %v, want %v (home-absolute settings file must not become a workspace exclude)", got, want)
	}
}

func TestParseDescriptorRunnerArgs(t *testing.T) {
	yaml := `
name: probetool
detect:
  - type: path_lookup
    command: probetool
runner_args:
  when_endpoint:
    - '-c'
    - 'model_provider={provider}'
`
	d, err := ParseDescriptor([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.RunnerArgs == nil {
		t.Fatal("runner_args not parsed")
	}
	want := []string{"-c", "model_provider={provider}"}
	if !reflect.DeepEqual(d.RunnerArgs.WhenEndpoint, want) {
		t.Fatalf("when_endpoint = %v, want %v", d.RunnerArgs.WhenEndpoint, want)
	}
}

func TestBuildRunnerArgsPlaceholders(t *testing.T) {
	desc, err := ParseDescriptor([]byte(`
name: codex
detect:
  - type: path_lookup
    command: codex
runner_args:
  when_endpoint:
    - '-c'
    - 'model_provider={provider}'
    - '-c'
    - 'model_providers.{provider}.name={provider}'
    - '-c'
    - 'model_providers.{provider}.base_url="{endpoint}"'
    - '-c'
    - 'model_providers.{provider}.wire_api="responses"'
    - '-c'
    - 'model_providers.{provider}.env_key={auth_env}'
    - '-c'
    - 'model_catalog_json={schmux_dir}/cache/codex-models-{provider}.json'
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a, err := NewGenericAdapter(desc)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(dir, "cache", "codex-models-zai.json")
	if err := os.WriteFile(catalog, []byte(`{"models":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	model := &Model{ID: "glm-5.3", Provider: "zai"}
	spec := RunnerSpec{
		ModelValue:      "glm-5.3",
		Endpoint:        "https://api.z.ai/api/v1",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
	}
	want := []string{
		"-c", "model_provider=zai",
		"-c", `model_providers.zai.name=zai`,
		"-c", `model_providers.zai.base_url="https://api.z.ai/api/v1"`,
		"-c", `model_providers.zai.wire_api="responses"`,
		"-c", "model_providers.zai.env_key=ANTHROPIC_AUTH_TOKEN",
		"-c", "model_catalog_json=" + catalog,
	}
	got := a.BuildRunnerArgs(model, spec, dir)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildRunnerArgs =\n%q\nwant\n%q", got, want)
	}
}

func TestBuildRunnerArgsNoEndpointNil(t *testing.T) {
	desc, err := ParseDescriptor([]byte(`
name: codex
detect:
  - type: path_lookup
    command: codex
runner_args:
  when_endpoint:
    - '-c'
    - 'model_provider={provider}'
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a, _ := NewGenericAdapter(desc)
	model := &Model{ID: "gpt-5.5", Provider: "openai"}
	if got := a.BuildRunnerArgs(model, RunnerSpec{ModelValue: "gpt-5.5"}, t.TempDir()); got != nil {
		t.Fatalf("endpoint-less spec: got %v, want nil", got)
	}
}

func TestBuildRunnerArgsPairDrop(t *testing.T) {
	desc, err := ParseDescriptor([]byte(`
name: codex
detect:
  - type: path_lookup
    command: codex
runner_args:
  when_endpoint:
    - '-c'
    - 'model_provider={provider}'
    - '-c'
    - 'model_providers.{provider}.env_key={auth_env}'
    - '-c'
    - 'model_catalog_json={schmux_dir}/cache/codex-models-{provider}.json'
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a, _ := NewGenericAdapter(desc)
	dir := t.TempDir() // no catalog file, no secrets
	model := &Model{ID: "custom-1", Provider: "custom"}
	spec := RunnerSpec{ModelValue: "custom-1", Endpoint: "https://api.example/v1"}
	// env_key pair drops (empty auth_env) and catalog pair drops (file absent):
	// only the model_provider pair survives. This is also the user-defined
	// model case: runner codex + endpoint + no secrets.
	want := []string{"-c", "model_provider=custom"}
	if got := a.BuildRunnerArgs(model, spec, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("pair-drop: got %v, want %v", got, want)
	}
}

func TestBuildRunnerArgsModelPlaceholder(t *testing.T) {
	desc, err := ParseDescriptor([]byte(`
name: probetool
detect:
  - type: path_lookup
    command: probetool
runner_args:
  when_endpoint:
    - '--route={model}'
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a, _ := NewGenericAdapter(desc)
	spec := RunnerSpec{ModelValue: "glm-5.3-airline", Endpoint: "https://api.example/v1"}
	want := []string{"--route=glm-5.3-airline"}
	if got := a.BuildRunnerArgs(&Model{ID: "x", Provider: "p"}, spec, t.TempDir()); !reflect.DeepEqual(got, want) {
		t.Fatalf("model placeholder: got %v, want %v", got, want)
	}
}
