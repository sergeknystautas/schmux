package detect

import (
	"testing"
)

func TestParseDescriptor_Minimal(t *testing.T) {
	yaml := `
name: mytool
detect:
  - type: path_lookup
    command: mytool
`
	d, err := ParseDescriptor([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseDescriptor: %v", err)
	}
	if d.Name != "mytool" {
		t.Errorf("Name = %q, want %q", d.Name, "mytool")
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
capabilities: [interactive, oneshot]
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
	if len(d.Capabilities) != 2 {
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
