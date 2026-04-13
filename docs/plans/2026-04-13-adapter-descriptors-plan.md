# Plan: Adapter Descriptors (Phase 1)

**Goal**: External agents (orc, etc.) can be defined via YAML descriptors loaded from embedded `contrib/` or runtime `~/.schmux/adapters/`, parsed by a single `GenericAdapter`, without modifying any existing builtin adapter code.

**Architecture**: YAML descriptors → Go struct → `GenericAdapter` implementing `ToolAdapter`. Hook strategies are an interface with `none` implemented in Phase 1. Builtin adapters (claude, codex, gemini, opencode) remain as Go code unchanged.

**Tech Stack**: Go 1.24, `gopkg.in/yaml.v3` (already in go.mod), `//go:embed`

**Design Spec**: `docs/specs/adapter-descriptors.md`

## Changes from plan review

- **C1 (go:embed)**: Use `//go:embed contrib` (directory-level, not glob) so
  it compiles when the directory has no `.yaml` files. Drop `descriptors/`
  embed entirely — Phase 1 has no builtin YAML descriptors.
- **C2 (registerToolName)**: Use a separate `descriptorToolNames` slice
  instead of mutating `builtinToolNames`. `IsBuiltinToolName` unchanged.
  New `IsToolName` checks both. Tests save/restore all mutable state.
- **C3 (TestAllAdaptersRegistered)**: Update the existing test to tolerate
  descriptor adapters by asserting `>= 4`.
- **S1 (Step 4 too large)**: Split GenericAdapter into sub-steps.
- **S3 (unsupported mode errors)**: `OneshotArgs`/`StreamingArgs` return
  error when descriptor has no config for that mode.
- **S6 (empty descriptors/ dir)**: Defer to Phase 2.
- **S7 (commit commands)**: Use `/commit` instead of `git commit`.

## Changes from schmux-002 rebase alignment

The `review` branch in schmux-002 refactors tool/model ID lookups into
`internal/types/` and adds `schmuxdir` path helpers. This lands on main
before our work. Adjustments:

- **Step 7 (registration)**: `descriptorToolNames` and `IsToolName` go in
  `internal/types/tools.go` (where `BuiltinToolNames` now lives), not in
  `internal/detect/tools.go`. `detect.IsToolName` becomes a thin wrapper
  like `detect.IsBuiltinToolName` already is. The `registerToolName` and
  `registerInstructionConfig` helpers stay in `detect/` since they touch
  the `detect`-owned `adapters` map and `agentInstructionConfigs` map.
- **Step 8 (daemon wiring)**: Use `schmuxdir.Get()` for the config dir
  path. Add `schmuxdir.AdaptersDir()` helper matching the existing pattern
  (`ConfigPath()`, `StatePath()`, etc.).

---

## Step 1: Add `SignalingNone` and `PersonaNone` enum values

**File**: `internal/detect/adapter.go`

The current enum zero-values are `SignalingHooks` (iota=0) and `PersonaCLIFlag` (iota=0). `GenericAdapter` needs explicit "none" values for omitted descriptor fields. Add them as the last values so existing iota assignments are unchanged.

### 1a. Write test

**File**: `internal/detect/adapter_generic_test.go`

```go
package detect

import "testing"

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
```

### 1b. Run test to verify it fails

```bash
go test ./internal/detect/ -run "TestSignalingNone|TestPersonaNone" -count=1
```

### 1c. Write implementation

**File**: `internal/detect/adapter.go`

Add after existing enum values:

```go
const (
	SignalingHooks           SignalingStrategy = iota // 0
	SignalingCLIFlag                                  // 1
	SignalingInstructionFile                          // 2
	SignalingNone                                     // 3 — no signaling
)

const (
	PersonaCLIFlag         PersonaInjection = iota // 0
	PersonaInstructionFile                         // 1
	PersonaConfigOverlay                           // 2
	PersonaNone                                    // 3 — no persona injection
)
```

### 1d. Run test to verify it passes

```bash
go test ./internal/detect/ -run "TestSignalingNone|TestPersonaNone" -count=1
```

### 1e. Commit

Use `/commit`.

---

## Step 2: Define the descriptor Go struct

**File**: `internal/detect/descriptor.go` (new)

### 2a. Write test

**File**: `internal/detect/descriptor_test.go` (new)

```go
package detect

import (
	"testing"
)

func TestParseDescriptor_Minimal(t *testing.T) {
	yaml := `
name: orc
detect:
  - type: path_lookup
    command: orc
`
	d, err := ParseDescriptor([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseDescriptor: %v", err)
	}
	if d.Name != "orc" {
		t.Errorf("Name = %q, want %q", d.Name, "orc")
	}
	if d.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty", d.DisplayName)
	}
	if len(d.Detect) != 1 {
		t.Fatalf("Detect len = %d, want 1", len(d.Detect))
	}
	if d.Detect[0].Type != "path_lookup" {
		t.Errorf("Detect[0].Type = %q", d.Detect[0].Type)
	}
	// Defaults
	if len(d.Capabilities) != 0 {
		t.Errorf("Capabilities should be empty by default, got %v", d.Capabilities)
	}
}

func TestParseDescriptor_Full(t *testing.T) {
	yaml := `
name: claude
display_name: Claude Code
detect:
  - type: path_lookup
    command: claude
  - type: file_exists
    path: "~/.local/bin/claude"
    verify: "-v"
  - type: homebrew_cask
    name: claude-code
  - type: npm_global
    package: "@anthropic-ai/claude-code"
capabilities: [interactive, oneshot, streaming]
model_flag: "--model"
command_args: []
instruction:
  dir: ".claude"
  file: "CLAUDE.md"
interactive:
  base_args: []
  resume_args: ["--continue"]
oneshot:
  base_args: ["-p", "--output-format", "json"]
  schema_flag: "--json-schema"
streaming:
  base_args: ["-p", "--output-format", "stream-json"]
  schema_flag: "--json-schema"
signaling:
  strategy: hooks
persona:
  strategy: cli_flag
  flag: "--append-system-prompt-file"
hooks:
  strategy: json-settings-merge
  settings_file: ".claude/settings.local.json"
  ownership_prefix: "schmux:"
skills:
  dir_pattern: ".claude/skills/schmux-{name}"
  file_name: "SKILL.md"
spawn_env:
  CLAUDE_CODE_EFFORT_LEVEL: "max"
`
	d, err := ParseDescriptor([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseDescriptor: %v", err)
	}
	if d.Name != "claude" {
		t.Errorf("Name = %q", d.Name)
	}
	if d.DisplayName != "Claude Code" {
		t.Errorf("DisplayName = %q", d.DisplayName)
	}
	if len(d.Capabilities) != 3 {
		t.Errorf("Capabilities = %v", d.Capabilities)
	}
	if d.ModelFlag != "--model" {
		t.Errorf("ModelFlag = %q", d.ModelFlag)
	}
	if d.Signaling.Strategy != "hooks" {
		t.Errorf("Signaling.Strategy = %q", d.Signaling.Strategy)
	}
	if d.Hooks.Strategy != "json-settings-merge" {
		t.Errorf("Hooks.Strategy = %q", d.Hooks.Strategy)
	}
	if d.SpawnEnv["CLAUDE_CODE_EFFORT_LEVEL"] != "max" {
		t.Errorf("SpawnEnv = %v", d.SpawnEnv)
	}
}

func TestParseDescriptor_MissingName(t *testing.T) {
	yaml := `
detect:
  - type: path_lookup
    command: test
`
	_, err := ParseDescriptor([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseDescriptor_MissingDetect(t *testing.T) {
	yaml := `name: test`
	_, err := ParseDescriptor([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing detect")
	}
}

func TestParseDescriptor_UnknownField(t *testing.T) {
	yaml := `
name: test
detect:
  - type: path_lookup
    command: test
bogus_field: true
`
	_, err := ParseDescriptor([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown field (strict mode)")
	}
}

func TestParseDescriptor_InvalidStrategy(t *testing.T) {
	yaml := `
name: test
detect:
  - type: path_lookup
    command: test
signaling:
  strategy: telekinesis
`
	_, err := ParseDescriptor([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid signaling strategy")
	}
}
```

### 2b. Run test to verify it fails

```bash
go test ./internal/detect/ -run "TestParseDescriptor" -count=1
```

### 2c. Write implementation

**File**: `internal/detect/descriptor.go` (new)

Define the `Descriptor` struct with YAML tags mapping to the schema from the spec. Implement `ParseDescriptor(data []byte) (*Descriptor, error)` which:

1. Unmarshals YAML with `yaml.v3` using `KnownFields(true)` for strict mode
2. Validates required fields (`name`, `detect` non-empty)
3. Validates enum values (`signaling.strategy`, `persona.strategy`, `hooks.strategy`, detect entry `type`, capabilities)

Key structs:

```go
type Descriptor struct {
    Name         string             `yaml:"name"`
    DisplayName  string             `yaml:"display_name"`
    Detect       []DetectEntry      `yaml:"detect"`
    Capabilities []string           `yaml:"capabilities"`
    ModelFlag    string             `yaml:"model_flag"`
    CommandArgs  []string           `yaml:"command_args"`
    Instruction  *InstructionDesc   `yaml:"instruction"`
    Interactive  *ModeDesc          `yaml:"interactive"`
    Oneshot      *ModeDesc          `yaml:"oneshot"`
    Streaming    *ModeDesc          `yaml:"streaming"`
    Signaling    *SignalingDesc     `yaml:"signaling"`
    Persona      *PersonaDesc       `yaml:"persona"`
    Hooks        *HooksDesc         `yaml:"hooks"`
    Skills       *SkillsDesc        `yaml:"skills"`
    SetupFiles   []SetupFileDesc    `yaml:"setup_files"`
    SpawnEnv     map[string]string  `yaml:"spawn_env"`
}

type DetectEntry struct {
    Type    string `yaml:"type"`     // path_lookup | file_exists | homebrew_cask | homebrew_formula | npm_global
    Command string `yaml:"command"`  // for path_lookup
    Path    string `yaml:"path"`     // for file_exists
    Verify  string `yaml:"verify"`   // for file_exists
    Name    string `yaml:"name"`     // for homebrew_cask/formula
    Package string `yaml:"package"`  // for npm_global
}

type InstructionDesc struct {
    Dir  string `yaml:"dir"`
    File string `yaml:"file"`
}

type ModeDesc struct {
    BaseArgs   []string `yaml:"base_args"`
    ResumeArgs []string `yaml:"resume_args"`
    SchemaFlag string   `yaml:"schema_flag"`
}

type SignalingDesc struct {
    Strategy      string `yaml:"strategy"`       // hooks | cli_flag | instruction_file | none
    Flag          string `yaml:"flag"`
    ValueTemplate string `yaml:"value_template"`
}

type PersonaDesc struct {
    Strategy      string `yaml:"strategy"`       // cli_flag | instruction_file | config_overlay | none
    Flag          string `yaml:"flag"`
    EnvVar        string `yaml:"env_var"`
    ValueTemplate string `yaml:"value_template"`
}

type HooksDesc struct {
    Strategy        string `yaml:"strategy"`        // json-settings-merge | plugin-file | none
    SettingsFile    string `yaml:"settings_file"`
    OwnershipPrefix string `yaml:"ownership_prefix"`
    PluginDir       string `yaml:"plugin_dir"`      // for plugin-file
    PluginFile      string `yaml:"plugin_file"`     // for plugin-file
}

type SkillsDesc struct {
    DirPattern  string `yaml:"dir_pattern"`
    FileName    string `yaml:"file_name"`
    FilePattern string `yaml:"file_pattern"`
}

type SetupFileDesc struct {
    Target string `yaml:"target"`
    Source string `yaml:"source"`
}
```

Validation function checks:

- `name` non-empty
- `detect` non-empty
- `signaling.strategy` in `{hooks, cli_flag, instruction_file, none, ""}`
- `persona.strategy` in `{cli_flag, instruction_file, config_overlay, none, ""}`
- `hooks.strategy` in `{json-settings-merge, plugin-file, none, ""}`
- `capabilities` entries in `{interactive, oneshot, streaming}`
- `detect[].type` in `{path_lookup, file_exists, homebrew_cask, homebrew_formula, npm_global}`

### 2d. Run test to verify it passes

```bash
go test ./internal/detect/ -run "TestParseDescriptor" -count=1
```

### 2e. Commit

Use `/commit`.

---

## Step 3: Hook strategy interface

**File**: `internal/detect/hook_strategy.go` (new)

### 3a. Write test

**File**: `internal/detect/hook_strategy_test.go` (new)

```go
package detect

import "testing"

func TestGetHookStrategy_None(t *testing.T) {
	s, err := GetHookStrategy("none")
	if err != nil {
		t.Fatalf("GetHookStrategy(none): %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil strategy")
	}
	if s.SupportsHooks() {
		t.Error("none strategy should not support hooks")
	}
}

func TestGetHookStrategy_Unknown(t *testing.T) {
	_, err := GetHookStrategy("telekinesis")
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestGetHookStrategy_Empty(t *testing.T) {
	s, err := GetHookStrategy("")
	if err != nil {
		t.Fatalf("GetHookStrategy empty: %v", err)
	}
	if s.SupportsHooks() {
		t.Error("empty strategy should default to none")
	}
}

func TestNoneStrategy_SetupHooks(t *testing.T) {
	s, _ := GetHookStrategy("none")
	err := s.SetupHooks(HookContext{WorkspacePath: "/tmp/test"})
	if err != nil {
		t.Errorf("SetupHooks: %v", err)
	}
}

func TestNoneStrategy_CleanupHooks(t *testing.T) {
	s, _ := GetHookStrategy("none")
	err := s.CleanupHooks("/tmp/test")
	if err != nil {
		t.Errorf("CleanupHooks: %v", err)
	}
}

func TestNoneStrategy_WrapRemoteCommand(t *testing.T) {
	s, _ := GetHookStrategy("none")
	cmd, err := s.WrapRemoteCommand("echo hello")
	if err != nil {
		t.Errorf("WrapRemoteCommand: %v", err)
	}
	if cmd != "echo hello" {
		t.Errorf("WrapRemoteCommand = %q, want passthrough", cmd)
	}
}
```

### 3b. Run test to verify it fails

```bash
go test ./internal/detect/ -run "TestGetHookStrategy|TestNoneStrategy" -count=1
```

### 3c. Write implementation

**File**: `internal/detect/hook_strategy.go` (new)

```go
package detect

import "fmt"

// HookStrategy defines how schmux injects lifecycle hooks into an agent's
// configuration. The strategy handles format-specific setup, cleanup, and
// remote command wrapping.
type HookStrategy interface {
	SupportsHooks() bool
	SetupHooks(ctx HookContext) error
	CleanupHooks(workspacePath string) error
	WrapRemoteCommand(command string) (string, error)
}

var hookStrategies = map[string]HookStrategy{
	"none": &noneHookStrategy{},
	"":     &noneHookStrategy{},
}

// GetHookStrategy returns the hook strategy registered under the given name.
func GetHookStrategy(name string) (HookStrategy, error) {
	s, ok := hookStrategies[name]
	if !ok {
		return nil, fmt.Errorf("unknown hook strategy: %q", name)
	}
	return s, nil
}

// RegisterHookStrategy registers a hook strategy by name. Called from init()
// by strategy implementations (e.g., json-settings-merge, plugin-file).
func RegisterHookStrategy(name string, s HookStrategy) {
	hookStrategies[name] = s
}

// noneHookStrategy is the default — no hook integration.
type noneHookStrategy struct{}

func (s *noneHookStrategy) SupportsHooks() bool                              { return false }
func (s *noneHookStrategy) SetupHooks(_ HookContext) error                   { return nil }
func (s *noneHookStrategy) CleanupHooks(_ string) error                      { return nil }
func (s *noneHookStrategy) WrapRemoteCommand(cmd string) (string, error)     { return cmd, nil }
```

### 3d. Run test to verify it passes

```bash
go test ./internal/detect/ -run "TestGetHookStrategy|TestNoneStrategy" -count=1
```

### 3e. Commit

Use `/commit`.

---

## Step 4: GenericAdapter implementation

**File**: `internal/detect/adapter_generic.go` (new)

### 4a. Write test

Add to `internal/detect/adapter_generic_test.go`:

```go
func TestGenericAdapter_Minimal(t *testing.T) {
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [interactive]
interactive:
  base_args: ["--run"]
`
	d, err := ParseDescriptor([]byte(yaml))
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
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
interactive:
  base_args: ["--run"]
  resume_args: ["resume", "--last"]
`
	d, _ := ParseDescriptor([]byte(yaml))
	a, _ := NewGenericAdapter(d)

	args := a.InteractiveArgs(nil, true)
	if len(args) != 2 || args[0] != "resume" || args[1] != "--last" {
		t.Errorf("InteractiveArgs(resume) = %v, want [resume --last]", args)
	}
}

func TestGenericAdapter_ModelPlaceholder(t *testing.T) {
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
capabilities: [oneshot]
oneshot:
  base_args: ["exec", "--json", "-m", "{model}", "--output-schema"]
  schema_flag: "--schema"
`
	d, _ := ParseDescriptor([]byte(yaml))
	a, _ := NewGenericAdapter(d)

	model := &Model{Runners: map[string]RunnerSpec{"testool": {ModelValue: "gpt-5"}}}
	args, err := a.OneshotArgs(model, `{"type":"object"}`)
	if err != nil {
		t.Fatalf("OneshotArgs: %v", err)
	}
	// Should expand {model} to gpt-5 and append schema
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
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
spawn_env:
  FOO: bar
  BAZ: qux
`
	d, _ := ParseDescriptor([]byte(yaml))
	a, _ := NewGenericAdapter(d)

	env := a.SpawnEnv(SpawnContext{})
	if env["FOO"] != "bar" || env["BAZ"] != "qux" {
		t.Errorf("SpawnEnv = %v", env)
	}
}

func TestGenericAdapter_SkillInjection_DirPattern(t *testing.T) {
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
skills:
  dir_pattern: ".testool/skills/schmux-{name}"
  file_name: "SKILL.md"
`
	d, _ := ParseDescriptor([]byte(yaml))
	a, _ := NewGenericAdapter(d)

	dir := t.TempDir()
	err := a.InjectSkill(dir, SkillModule{Name: "greeting", Content: "Hello!"})
	if err != nil {
		t.Fatalf("InjectSkill: %v", err)
	}

	// Check file was written
	content, err := os.ReadFile(filepath.Join(dir, ".testool", "skills", "schmux-greeting", "SKILL.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "Hello!" {
		t.Errorf("Skill content = %q", string(content))
	}

	// Remove
	err = a.RemoveSkill(dir, "greeting")
	if err != nil {
		t.Fatalf("RemoveSkill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".testool", "skills", "schmux-greeting")); !os.IsNotExist(err) {
		t.Error("skill directory should be removed")
	}
}

func TestGenericAdapter_SkillInjection_FilePattern(t *testing.T) {
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
skills:
  file_pattern: ".testool/commands/schmux-{name}.md"
`
	d, _ := ParseDescriptor([]byte(yaml))
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
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
instruction:
  dir: ".testool"
  file: "INSTRUCTIONS.md"
`
	d, _ := ParseDescriptor([]byte(yaml))
	a, _ := NewGenericAdapter(d)

	cfg := a.InstructionConfig()
	if cfg.InstructionDir != ".testool" || cfg.InstructionFile != "INSTRUCTIONS.md" {
		t.Errorf("InstructionConfig = %+v", cfg)
	}
}

func TestGenericAdapter_SignalingCLIFlag(t *testing.T) {
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
signaling:
  strategy: cli_flag
  flag: "-c"
  value_template: "instructions_file={path}"
`
	d, _ := ParseDescriptor([]byte(yaml))
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
	yaml := `
name: testool
detect:
  - type: path_lookup
    command: testool
persona:
  strategy: cli_flag
  flag: "--system-prompt"
`
	d, _ := ParseDescriptor([]byte(yaml))
	a, _ := NewGenericAdapter(d)

	if a.PersonaInjection() != PersonaCLIFlag {
		t.Errorf("PersonaInjection = %v", a.PersonaInjection())
	}
	args := a.PersonaArgs("/tmp/persona.md")
	if len(args) != 2 || args[0] != "--system-prompt" {
		t.Errorf("PersonaArgs = %v", args)
	}
	// Empty path returns nil
	if a.PersonaArgs("") != nil {
		t.Error("PersonaArgs('') should return nil")
	}
}
```

Note: add `"os"`, `"path/filepath"` to imports.

### 4b. Run test to verify it fails

```bash
go test ./internal/detect/ -run "TestGenericAdapter" -count=1
```

### 4c. Write implementation

**File**: `internal/detect/adapter_generic.go` (new)

```go
package detect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenericAdapter implements ToolAdapter from a parsed Descriptor.
type GenericAdapter struct {
	desc         *Descriptor
	hookStrategy HookStrategy
}

// NewGenericAdapter creates a GenericAdapter from a parsed descriptor.
func NewGenericAdapter(d *Descriptor) (*GenericAdapter, error) {
	strategyName := "none"
	if d.Hooks != nil && d.Hooks.Strategy != "" {
		strategyName = d.Hooks.Strategy
	}
	hs, err := GetHookStrategy(strategyName)
	if err != nil {
		return nil, fmt.Errorf("descriptor %q: %w", d.Name, err)
	}
	return &GenericAdapter{desc: d, hookStrategy: hs}, nil
}
```

Then implement all 20 `ToolAdapter` methods. Key logic for each:

- **`Name()`**: return `d.Name`
- **`Detect(ctx)`**: iterate `d.Detect` entries, call existing helpers (`commandExists`, `fileExists`, `tryCommand`, `homebrewCaskInstalled`, `homebrewFormulaInstalled`, `npmGlobalInstalled`) based on entry type
- **`InteractiveArgs(model, resume)`**: if resume and `d.Interactive.ResumeArgs` set, return those; else return `d.Interactive.BaseArgs` with `{model}` expansion
- **`OneshotArgs(model, schema)`**: return `d.Oneshot.BaseArgs` with `{model}` expansion, append schema flag+value if non-empty
- **`StreamingArgs(model, schema)`**: same pattern as oneshot
- **`InstructionConfig()`**: return from `d.Instruction`, or empty
- **`SignalingStrategy()`**: map string to enum (`"hooks"` → `SignalingHooks`, etc.), default `SignalingNone`
- **`SignalingArgs(path)`**: if `d.Signaling.Flag` set, return `[flag, expanded_template]`; template replaces `{path}` with actual path
- **`SupportsHooks()`**: delegate to `hookStrategy.SupportsHooks()`
- **`SetupHooks(ctx)`**: delegate to `hookStrategy.SetupHooks(ctx)`
- **`CleanupHooks(path)`**: delegate to `hookStrategy.CleanupHooks(path)`
- **`WrapRemoteCommand(cmd)`**: delegate to `hookStrategy.WrapRemoteCommand(cmd)`
- **`PersonaInjection()`**: map string to enum, default `PersonaNone`
- **`PersonaArgs(path)`**: if empty path return nil; return `[flag, path]`
- **`SpawnEnv(ctx)`**: return `d.SpawnEnv` (static map). For `config_overlay` persona, if `ctx.PersonaPath` non-empty and `d.Persona.EnvVar` set, add it
- **`SetupCommands(path)`**: iterate `d.SetupFiles`, write embedded template content to target path
- **`InjectSkill(path, skill)`**: if `d.Skills.DirPattern` set, mkdir + write file; if `d.Skills.FilePattern` set, write flat file
- **`RemoveSkill(path, name)`**: if `d.Skills.DirPattern`, `os.RemoveAll` dir; if `d.Skills.FilePattern`, `os.Remove` file
- **`BuildRunnerEnv(spec)`**: return empty map (model routing stays in models layer)
- **`ModelFlag()`**: return `d.ModelFlag`
- **`Capabilities()`**: return `d.Capabilities`, default `["interactive"]` if empty

Helper function `expandModelPlaceholder(args []string, model *Model, adapterName string) []string` replaces `{model}` tokens in args with the resolved model value.

### 4d. Run test to verify it passes

```bash
go test ./internal/detect/ -run "TestGenericAdapter" -count=1
```

### 4e. Commit

Use `/commit`.

---

## Step 5: Set up embed directory

### 5a. Create directory and files

```bash
mkdir -p internal/detect/contrib
touch internal/detect/contrib/.gitkeep
```

Note: `descriptors/` is deferred to Phase 2 when builtin adapters are
converted to YAML. Empty directories can't be committed to git anyway.

### 5b. Add .gitignore entry

**File**: `.gitignore`

Append:

```
# External adapter descriptors (populated by CI, not committed)
internal/detect/contrib/*.yaml
!internal/detect/contrib/.gitkeep
```

### 5c. Commit

Use `/commit`.

---

## Step 6: Descriptor loader

**File**: `internal/detect/loader.go` (new)

### 6a. Write test

**File**: `internal/detect/loader_test.go` (new)

```go
package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeDescriptors(t *testing.T) {
	dir := t.TempDir()

	// Write a valid descriptor
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

	// Write a non-yaml file (should be ignored)
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
```

### 6b. Run test to verify it fails

```bash
go test ./internal/detect/ -run "TestLoadRuntimeDescriptors" -count=1
```

### 6c. Write implementation

**File**: `internal/detect/loader.go` (new)

```go
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
// directory contains no .yaml files. Phase 1 has no builtin descriptors;
// contrib/ is populated by internal CI before build.
//go:embed contrib
var embeddedContrib embed.FS

// LoadEmbeddedDescriptors loads descriptors from the embedded contrib/
// directory. Returns them with name collision detection.
func LoadEmbeddedDescriptors() ([]*Descriptor, error) {
	var all []*Descriptor
	seen := map[string]string{} // name → source

	entries, err := fs.ReadDir(embeddedContrib, "contrib")
	if err != nil {
		return nil, nil // empty dir is fine
	}
	for _, e := range entries {
		if e.IsDir() || !isYAMLFile(e.Name()) {
			continue
		}
		data, err := fs.ReadFile(embeddedContrib, "contrib/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded contrib/%s: %w", e.Name(), err)
		}
		d, err := ParseDescriptor(data)
		if err != nil {
			return nil, fmt.Errorf("parse embedded contrib/%s: %w", e.Name(), err)
		}
		if prev, ok := seen[d.Name]; ok {
			return nil, fmt.Errorf("duplicate descriptor name %q in %s and contrib/%s", d.Name, prev, e.Name())
		}
		seen[d.Name] = "contrib/" + e.Name()
		all = append(all, d)
	}
	return all, nil
}

// LoadRuntimeDescriptors loads descriptors from a directory on disk
// (typically ~/.schmux/adapters/). Returns empty slice if dir doesn't exist.
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
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		d, err := ParseDescriptor(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		if prev, ok := seen[d.Name]; ok {
			return nil, fmt.Errorf("duplicate descriptor name %q in %s and %s", d.Name, prev, e.Name())
		}
		seen[d.Name] = e.Name()
		all = append(all, d)
	}
	return all, nil
}

func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}
```

### 6d. Run test to verify it passes

```bash
go test ./internal/detect/ -run "TestLoadRuntimeDescriptors" -count=1
```

### 6e. Commit

Use `/commit`.

---

## Step 7: Registration and wiring

**Files**: `internal/types/tools.go` (modify), `internal/detect/tools.go` (modify),
`internal/detect/loader.go` (extend)

After the schmux-002 refactor, `BuiltinToolNames` and `IsBuiltinToolName`
live in `internal/types/tools.go`. `detect.IsBuiltinToolName` is a thin
wrapper. We follow the same pattern: `DescriptorToolNames` and `IsToolName`
go in `types/`, with a thin wrapper in `detect/`.

### 7a. Write test

Add to `internal/detect/loader_test.go`:

```go
// saveAndRestoreRegistries saves all mutable package-level registries and
// restores them when the test completes. Call at the start of any test
// that registers adapters or tool names.
func saveAndRestoreRegistries(t *testing.T) {
	t.Helper()
	origAdapters := make(map[string]ToolAdapter)
	for k, v := range adapters {
		origAdapters[k] = v
	}
	origDescriptorNames := append([]string(nil), types.DescriptorToolNames...)
	origInstructionConfigs := make(map[string]AgentInstructionConfig)
	for k, v := range agentInstructionConfigs {
		origInstructionConfigs[k] = v
	}
	t.Cleanup(func() {
		adapters = origAdapters
		types.DescriptorToolNames = origDescriptorNames
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

	// Should be in tool name list via IsToolName
	if !IsToolName("testool") {
		t.Error("testool should be a known tool name")
	}
	// But NOT in IsBuiltinToolName (that stays for the original four)
	if IsBuiltinToolName("testool") {
		t.Error("testool should NOT be a builtin tool name")
	}
}

func TestRegisterDescriptorAdapters_CollisionWithBuiltin(t *testing.T) {
	descs := []*Descriptor{
		{
			Name:   "claude", // collides with builtin
			Detect: []DetectEntry{{Type: "path_lookup", Command: "claude"}},
		},
	}

	err := RegisterDescriptorAdapters(descs)
	if err == nil {
		t.Fatal("expected error for collision with builtin adapter")
	}
}
```

Note: add `"github.com/sergeknystautas/schmux/internal/types"` to imports.

### 7b. Run test to verify it fails

```bash
go test ./internal/detect/ -run "TestRegisterDescriptorAdapters" -count=1
```

### 7c. Write implementation

**File**: `internal/types/tools.go` — add `DescriptorToolNames` and `IsToolName`
alongside existing `BuiltinToolNames` and `IsBuiltinToolName`:

```go
// DescriptorToolNames tracks tool names registered from YAML descriptors.
// Separate from BuiltinToolNames to preserve the semantic distinction.
var DescriptorToolNames []string

// IsToolName returns true if the name is any registered tool (builtin or descriptor).
func IsToolName(name string) bool {
	if IsBuiltinToolName(name) {
		return true
	}
	for _, n := range DescriptorToolNames {
		if n == name {
			return true
		}
	}
	return false
}
```

**File**: `internal/detect/tools.go` — add thin wrappers and registration
helpers (instruction configs stay in `detect/` since the map is `detect`-owned):

```go
// IsToolName returns true if the name is any registered tool (builtin or descriptor).
func IsToolName(name string) bool {
	return types.IsToolName(name)
}

func registerToolName(name string) {
	if !types.IsToolName(name) {
		types.DescriptorToolNames = append(types.DescriptorToolNames, name)
	}
}

func registerInstructionConfig(name string, cfg AgentInstructionConfig) {
	agentInstructionConfigs[name] = cfg
}
```

**File**: `internal/detect/loader.go` — add `RegisterDescriptorAdapters`:

```go
// RegisterDescriptorAdapters creates GenericAdapters from descriptors and
// registers them into the adapter registry. Returns error if any name
// collides with an existing (builtin) adapter.
func RegisterDescriptorAdapters(descs []*Descriptor) error {
	for _, d := range descs {
		if existing := GetAdapter(d.Name); existing != nil {
			return fmt.Errorf("descriptor %q collides with existing adapter", d.Name)
		}
		a, err := NewGenericAdapter(d)
		if err != nil {
			return err
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
```

**File**: `internal/detect/adapter_test.go` — update `TestAllAdaptersRegistered`
to tolerate descriptor adapters:

Change `len(adapters) != 4` to `len(adapters) < 4` (at least the four builtins).

Also audit callers of `IsBuiltinToolName` in `internal/config/run_targets.go`
and `internal/session/manager.go` — these should switch to `IsToolName` so
that descriptor-defined agents are treated the same as builtins for collision
detection.

### 7d. Run test to verify it passes

```bash
go test ./internal/detect/ -run "TestRegisterDescriptorAdapters" -count=1
```

### 7e. Commit

Use `/commit`.

---

## Step 8: Wire into daemon startup

**File**: `internal/daemon/daemon.go`

### 8a. Write test

This is an integration-level verification. Add to `internal/detect/loader_test.go`:

```go
func TestLoadAndRegisterAll(t *testing.T) {
	saveAndRestoreRegistries(t)

	// Create a temp runtime dir with a descriptor
	dir := t.TempDir()
	yaml := []byte(`
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
	os.WriteFile(filepath.Join(dir, "testool.yaml"), yaml, 0644)

	// Load embedded (will be empty or contain only .gitkeep)
	embedded, err := LoadEmbeddedDescriptors()
	if err != nil {
		t.Fatalf("LoadEmbeddedDescriptors: %v", err)
	}

	// Load runtime
	runtime, err := LoadRuntimeDescriptors(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeDescriptors: %v", err)
	}

	// Merge: embedded first, then runtime (skip collisions with embedded)
	all := embedded
	embeddedNames := map[string]bool{}
	for _, d := range embedded {
		embeddedNames[d.Name] = true
	}
	for _, d := range runtime {
		if embeddedNames[d.Name] {
			continue // embedded wins
		}
		all = append(all, d)
	}

	err = RegisterDescriptorAdapters(all)
	if err != nil {
		t.Fatalf("RegisterDescriptorAdapters: %v", err)
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
```

### 8b. Run test to verify it fails (or passes if code is already in place)

```bash
go test ./internal/detect/ -run "TestLoadAndRegisterAll" -count=1
```

### 8c. Write implementation

**File**: `internal/detect/loader.go` — add convenience function:

```go
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

	// Merge: embedded wins on name collision
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
```

**File**: `internal/schmuxdir/schmuxdir.go` — add `AdaptersDir()` helper,
matching the existing pattern (`ConfigPath()`, `StatePath()`, etc.):

```go
func AdaptersDir() string { return filepath.Join(Get(), "adapters") }
```

**File**: `internal/daemon/daemon.go` — call during startup.

Find the daemon's `Start` or initialization function. Add near the beginning,
after config loading (uses `schmuxdir.AdaptersDir()`):

```go
// Load external adapter descriptors
if err := detect.LoadAndRegisterDescriptors(schmuxdir.AdaptersDir()); err != nil {
	// Log warning but don't fail — external adapters are optional
	fmt.Fprintf(os.Stderr, "[daemon] warning: loading adapter descriptors: %v\n", err)
}
```

### 8d. Run test to verify it passes

```bash
go test ./internal/detect/ -run "TestLoadAndRegisterAll" -count=1
```

### 8e. Commit

Use `/commit`.

---

## Step 9: End-to-end verification

### 9a. Write a test descriptor for orc

**File**: `internal/detect/contrib/orc.yaml` (for local testing only, .gitignored)

```yaml
name: orc
display_name: Orc

detect:
  - type: path_lookup
    command: orc
  - type: file_exists
    path: /opt/facebook/bin/orc

capabilities: [interactive]

interactive:
  base_args: ['--new', '-y']

spawn_env:
  TMUX: '1'
```

### 9b. Build and verify detection

```bash
go build ./cmd/schmux && ./schmux status
```

Verify orc appears in the detection summary if `/opt/facebook/bin/orc` exists.

### 9c. Verify via API

```bash
curl -s http://localhost:7337/api/detection-summary | jq '.tools[] | select(.name=="orc")'
```

Should show orc with `Source: "file_exists /opt/facebook/bin/orc"` or `Source: "PATH"`.

### 9d. Full test suite

```bash
./test.sh --quick
```

Verify no regressions in existing adapter behavior.

### 9e. Clean up and commit

Move the orc descriptor out of contrib (it's .gitignored and for Meta CI only):

```bash
rm internal/detect/contrib/orc.yaml
```

During development, use `~/.schmux/adapters/orc.yaml` instead.

---

## Task Dependencies

| Group | Steps         | Can Parallelize         | Notes                                                   |
| ----- | ------------- | ----------------------- | ------------------------------------------------------- |
| 1     | Steps 1, 2, 3 | Yes (independent)       | Enum values, descriptor struct, hook strategy interface |
| 2     | Step 4        | No (depends on 1, 2, 3) | GenericAdapter uses all three                           |
| 3     | Steps 5, 6    | Yes (independent)       | Embed dirs, loader are independent                      |
| 4     | Step 7        | No (depends on 4, 6)    | Registration wires adapter + loader                     |
| 5     | Step 8        | No (depends on 7)       | Daemon startup wiring                                   |
| 6     | Step 9        | No (depends on 8)       | End-to-end verification                                 |

## Files Touched

| File                                      | Action                                                                       |
| ----------------------------------------- | ---------------------------------------------------------------------------- |
| `internal/detect/adapter.go`              | Modify (add enum values)                                                     |
| `internal/detect/adapter_test.go`         | Modify (relax adapter count assertion)                                       |
| `internal/detect/descriptor.go`           | Create                                                                       |
| `internal/detect/descriptor_test.go`      | Create                                                                       |
| `internal/detect/hook_strategy.go`        | Create                                                                       |
| `internal/detect/hook_strategy_test.go`   | Create                                                                       |
| `internal/detect/adapter_generic.go`      | Create                                                                       |
| `internal/detect/adapter_generic_test.go` | Create                                                                       |
| `internal/detect/loader.go`               | Create                                                                       |
| `internal/detect/loader_test.go`          | Create                                                                       |
| `internal/detect/tools.go`                | Modify (add IsToolName wrapper, registerToolName, registerInstructionConfig) |
| `internal/detect/contrib/.gitkeep`        | Create                                                                       |
| `internal/types/tools.go`                 | Modify (add DescriptorToolNames, IsToolName)                                 |
| `internal/config/run_targets.go`          | Modify (IsBuiltinToolName → IsToolName, already imports types)               |
| `internal/session/manager.go`             | Modify (IsBuiltinToolName → IsToolName where appropriate)                    |
| `internal/schmuxdir/schmuxdir.go`         | Modify (add AdaptersDir helper)                                              |
| `internal/daemon/daemon.go`               | Modify (add startup call using schmuxdir.AdaptersDir)                        |
| `.gitignore`                              | Modify (add contrib pattern)                                                 |
