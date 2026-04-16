package detect

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Descriptor represents a YAML adapter descriptor that defines how to detect,
// configure, and interact with an AI coding agent.
type Descriptor struct {
	Name         string            `yaml:"name"`
	DisplayName  string            `yaml:"display_name"`
	Detect       []DetectEntry     `yaml:"detect"`
	Capabilities []string          `yaml:"capabilities"`
	ModelFlag    string            `yaml:"model_flag"`
	PromptFlag   string            `yaml:"prompt_flag"`
	CommandArgs  []string          `yaml:"command_args"`
	Instruction  *InstructionDesc  `yaml:"instruction"`
	Interactive  *ModeDesc         `yaml:"interactive"`
	Oneshot      *ModeDesc         `yaml:"oneshot"`
	Signaling    *SignalingDesc    `yaml:"signaling"`
	Persona      *PersonaDesc      `yaml:"persona"`
	Hooks        *HooksDesc        `yaml:"hooks"`
	Skills       *SkillsDesc       `yaml:"skills"`
	SetupFiles   []SetupFileDesc   `yaml:"setup_files"`
	SpawnEnv     map[string]string `yaml:"spawn_env"`
	RunnerEnv    *RunnerEnvDesc    `yaml:"runner_env"`
}

// RunnerEnvDesc describes env vars the adapter emits when spawning a runner.
// WhenEndpoint is applied only when the resolved RunnerSpec has a non-empty
// Endpoint (third-party providers proxied through this tool). Values may use
// the placeholders {endpoint} and {model}.
type RunnerEnvDesc struct {
	WhenEndpoint map[string]string `yaml:"when_endpoint"`
}

// DetectEntry describes one method for detecting whether an agent is installed.
type DetectEntry struct {
	Type    string `yaml:"type"`
	Command string `yaml:"command"`
	Path    string `yaml:"path"`
	Verify  string `yaml:"verify"`
	Name    string `yaml:"name"`
	Package string `yaml:"package"`
}

// InstructionDesc describes where an agent reads its instruction files.
type InstructionDesc struct {
	Dir  string `yaml:"dir"`
	File string `yaml:"file"`
}

// ModeDesc describes the CLI arguments for a particular execution mode.
type ModeDesc struct {
	BaseArgs   []string `yaml:"base_args"`
	ResumeArgs []string `yaml:"resume_args"`
	SchemaFlag string   `yaml:"schema_flag"`
	SchemaArgs []string `yaml:"schema_args"`
	ModelFlag  string   `yaml:"model_flag"`
}

// SignalingDesc describes how schmux signals lifecycle events to the agent.
type SignalingDesc struct {
	Strategy      string `yaml:"strategy"`
	Flag          string `yaml:"flag"`
	ValueTemplate string `yaml:"value_template"`
}

// PersonaDesc describes how the agent's persona/system prompt is configured.
type PersonaDesc struct {
	Strategy      string `yaml:"strategy"`
	Flag          string `yaml:"flag"`
	EnvVar        string `yaml:"env_var"`
	ValueTemplate string `yaml:"value_template"`
}

// HooksDesc describes how schmux injects hooks into the agent.
type HooksDesc struct {
	Strategy        string `yaml:"strategy"`
	SettingsFile    string `yaml:"settings_file"`
	OwnershipPrefix string `yaml:"ownership_prefix"`
	PluginDir       string `yaml:"plugin_dir"`
	PluginFile      string `yaml:"plugin_file"`
}

// SkillsDesc describes where the agent reads skill files.
type SkillsDesc struct {
	DirPattern  string `yaml:"dir_pattern"`
	FileName    string `yaml:"file_name"`
	FilePattern string `yaml:"file_pattern"`
}

// SetupFileDesc describes a file that should be copied into the workspace.
type SetupFileDesc struct {
	Target string `yaml:"target"`
	Source string `yaml:"source"`
}

// Valid enum sets for validation.
var (
	validDetectTypes         = map[string]bool{"path_lookup": true, "file_exists": true, "homebrew_cask": true, "homebrew_formula": true, "npm_global": true}
	validCapabilities        = map[string]bool{"interactive": true, "oneshot": true}
	validSignalingStrategies = map[string]bool{"hooks": true, "cli_flag": true, "instruction_file": true, "none": true, "": true}
	validPersonaStrategies   = map[string]bool{"cli_flag": true, "instruction_file": true, "config_overlay": true, "none": true, "": true}
	validHooksStrategies     = map[string]bool{"json-settings-merge": true, "plugin-file": true, "none": true, "": true}
)

// ParseDescriptor parses and validates a YAML adapter descriptor.
// It uses strict mode to reject unknown fields and validates all enum values.
func ParseDescriptor(data []byte) (*Descriptor, error) {
	var d Descriptor

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(&d); err != nil {
		return nil, fmt.Errorf("parsing descriptor YAML: %w", err)
	}

	if d.Name == "" {
		return nil, fmt.Errorf("descriptor: name is required")
	}
	if len(d.Detect) == 0 {
		return nil, fmt.Errorf("descriptor: at least one detect entry is required")
	}

	for i, entry := range d.Detect {
		if !validDetectTypes[entry.Type] {
			return nil, fmt.Errorf("descriptor: detect[%d].type %q is not valid (must be one of: path_lookup, file_exists, homebrew_cask, homebrew_formula, npm_global)", i, entry.Type)
		}
	}

	for i, cap := range d.Capabilities {
		if !validCapabilities[cap] {
			return nil, fmt.Errorf("descriptor: capabilities[%d] %q is not valid (must be one of: interactive, oneshot)", i, cap)
		}
	}

	if d.Signaling != nil {
		if !validSignalingStrategies[d.Signaling.Strategy] {
			return nil, fmt.Errorf("descriptor: signaling.strategy %q is not valid (must be one of: hooks, cli_flag, instruction_file, none)", d.Signaling.Strategy)
		}
	}

	if d.Persona != nil {
		if !validPersonaStrategies[d.Persona.Strategy] {
			return nil, fmt.Errorf("descriptor: persona.strategy %q is not valid (must be one of: cli_flag, instruction_file, config_overlay, none)", d.Persona.Strategy)
		}
	}

	if d.Hooks != nil {
		if !validHooksStrategies[d.Hooks.Strategy] {
			return nil, fmt.Errorf("descriptor: hooks.strategy %q is not valid (must be one of: json-settings-merge, plugin-file, none)", d.Hooks.Strategy)
		}
	}

	return &d, nil
}
