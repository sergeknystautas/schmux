package detect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestGenericAdapter_SetupCommands(t *testing.T) {
	RegisterSetupTemplate("test-command", []byte("# Test Command\nDo the thing"))
	defer delete(setupTemplates, "test-command")

	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
setup_files:
  - target: ".testool/commands/test.md"
    source: test-command
`
	d, err := ParseDescriptor([]byte(yamlData))
	if err != nil {
		t.Fatalf("ParseDescriptor: %v", err)
	}
	a, err := NewGenericAdapter(d)
	if err != nil {
		t.Fatalf("NewGenericAdapter: %v", err)
	}
	dir := t.TempDir()
	err = a.SetupCommands(dir)
	if err != nil {
		t.Fatalf("SetupCommands: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, ".testool", "commands", "test.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "Test Command") {
		t.Errorf("content = %q", string(content))
	}
}

func TestGenericAdapter_SetupCommands_NoFiles(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	err := a.SetupCommands(t.TempDir())
	if err != nil {
		t.Fatalf("SetupCommands with no setup_files should succeed: %v", err)
	}
}

func TestGenericAdapter_SetupCommands_UnknownSource(t *testing.T) {
	yamlData := `
name: testool
detect:
  - type: path_lookup
    command: testool
setup_files:
  - target: ".testool/commands/test.md"
    source: nonexistent-template
`
	d, _ := ParseDescriptor([]byte(yamlData))
	a, _ := NewGenericAdapter(d)
	err := a.SetupCommands(t.TempDir())
	if err != nil {
		t.Fatalf("SetupCommands with unknown source should skip silently: %v", err)
	}
}
