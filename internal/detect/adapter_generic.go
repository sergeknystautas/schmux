package detect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// setupTemplates maps template names to their content. Templates are registered
// at init time and referenced by SetupFileDesc.Source in YAML descriptors.
var setupTemplates = map[string][]byte{}

// RegisterSetupTemplate registers a setup file template by name. Called from
// init functions so templates are available when GenericAdapter.SetupCommands runs.
func RegisterSetupTemplate(name string, content []byte) {
	setupTemplates[name] = content
}

// GenericAdapter implements ToolAdapter by reading all behavior from a parsed
// Descriptor. This lets new tools be added via YAML files without Go code.
type GenericAdapter struct {
	desc         *Descriptor
	hookStrategy HookStrategy
}

// NewGenericAdapter creates a GenericAdapter from a parsed Descriptor.
func NewGenericAdapter(d *Descriptor) (*GenericAdapter, error) {
	strategyName := ""
	if d.Hooks != nil {
		strategyName = d.Hooks.Strategy
	}
	hs, err := GetHookStrategy(strategyName)
	if err != nil {
		return nil, fmt.Errorf("generic adapter %q: %w", d.Name, err)
	}
	return &GenericAdapter{desc: d, hookStrategy: hs}, nil
}

// Name returns the canonical tool name.
func (a *GenericAdapter) Name() string {
	return a.desc.Name
}

// Detect attempts to find the tool on the system using the descriptor's detect entries.
func (a *GenericAdapter) Detect(ctx context.Context) (Tool, bool) {
	for _, entry := range a.desc.Detect {
		switch entry.Type {
		case "path_lookup":
			if commandExists(entry.Command) {
				cmd := entry.Command
				if len(a.desc.CommandArgs) > 0 {
					cmd = cmd + " " + strings.Join(a.desc.CommandArgs, " ")
				}
				return Tool{Name: a.desc.Name, Command: cmd, Source: "PATH", Agentic: true}, true
			}
		case "file_exists":
			expanded, err := expandHome(entry.Path)
			if err != nil {
				continue
			}
			if fileExists(expanded) {
				if entry.Verify != "" {
					if !tryCommand(ctx, expanded, entry.Verify) {
						continue
					}
				}
				cmd := expanded
				if len(a.desc.CommandArgs) > 0 {
					cmd = cmd + " " + strings.Join(a.desc.CommandArgs, " ")
				}
				return Tool{Name: a.desc.Name, Command: cmd, Source: "file " + entry.Path, Agentic: true}, true
			}
		case "homebrew_cask":
			if homebrewCaskInstalled(ctx, entry.Name) {
				cmd := entry.Name
				if len(a.desc.CommandArgs) > 0 {
					cmd = cmd + " " + strings.Join(a.desc.CommandArgs, " ")
				}
				return Tool{Name: a.desc.Name, Command: cmd, Source: "Homebrew cask " + entry.Name, Agentic: true}, true
			}
		case "homebrew_formula":
			if homebrewFormulaInstalled(ctx, entry.Name) {
				cmd := entry.Name
				if len(a.desc.CommandArgs) > 0 {
					cmd = cmd + " " + strings.Join(a.desc.CommandArgs, " ")
				}
				return Tool{Name: a.desc.Name, Command: cmd, Source: "Homebrew formula " + entry.Name, Agentic: true}, true
			}
		case "npm_global":
			if npmGlobalInstalled(ctx, entry.Package) {
				cmd := a.desc.Name
				if len(a.desc.CommandArgs) > 0 {
					cmd = cmd + " " + strings.Join(a.desc.CommandArgs, " ")
				}
				return Tool{Name: a.desc.Name, Command: cmd, Source: "npm global " + entry.Package, Agentic: true}, true
			}
		}
	}
	return Tool{}, false
}

// resolveModelFlag returns the effective model flag for a given mode. If the
// mode has its own ModelFlag, that overrides the top-level value. A value of
// "-" explicitly disables model injection.
func (a *GenericAdapter) resolveModelFlag(mode *ModeDesc) string {
	if mode != nil && mode.ModelFlag != "" {
		if mode.ModelFlag == "-" {
			return ""
		}
		return mode.ModelFlag
	}
	return a.desc.ModelFlag
}

// InteractiveArgs returns CLI args for interactive mode.
// When resume is true and resume_args are configured, those are returned instead.
func (a *GenericAdapter) InteractiveArgs(model *Model, resume bool) []string {
	if a.desc.Interactive == nil {
		return nil
	}
	if resume && a.desc.Interactive.ResumeArgs != nil {
		return a.desc.Interactive.ResumeArgs
	}
	mf := a.resolveModelFlag(a.desc.Interactive)
	return expandModelPlaceholder(a.desc.Interactive.BaseArgs, model, a.desc.Name, mf)
}

// OneshotArgs returns CLI args for oneshot mode.
func (a *GenericAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	if a.desc.Oneshot == nil {
		return nil, fmt.Errorf("%s: oneshot not supported", a.desc.Name)
	}
	mf := a.resolveModelFlag(a.desc.Oneshot)
	args := expandModelPlaceholder(a.desc.Oneshot.BaseArgs, model, a.desc.Name, mf)
	if jsonSchema != "" {
		if a.desc.Oneshot.SchemaFlag != "" {
			args = append(args, a.desc.Oneshot.SchemaFlag, jsonSchema)
		}
		if len(a.desc.Oneshot.SchemaArgs) > 0 {
			args = append(args, a.desc.Oneshot.SchemaArgs...)
		}
	}
	return args, nil
}

// InstructionConfig returns the instruction file location for this tool.
func (a *GenericAdapter) InstructionConfig() AgentInstructionConfig {
	if a.desc.Instruction != nil {
		return AgentInstructionConfig{
			InstructionDir:  a.desc.Instruction.Dir,
			InstructionFile: a.desc.Instruction.File,
		}
	}
	return AgentInstructionConfig{}
}

// SignalingStrategy returns how this tool receives schmux signaling.
func (a *GenericAdapter) SignalingStrategy() SignalingStrategy {
	if a.desc.Signaling == nil {
		return SignalingNone
	}
	switch a.desc.Signaling.Strategy {
	case "hooks":
		return SignalingHooks
	case "cli_flag":
		return SignalingCLIFlag
	case "instruction_file":
		return SignalingInstructionFile
	default:
		return SignalingNone
	}
}

// SignalingArgs returns CLI args for injecting the signaling instructions file.
func (a *GenericAdapter) SignalingArgs(filePath string) []string {
	if a.desc.Signaling == nil || a.desc.Signaling.Flag == "" {
		return nil
	}
	tmpl := a.desc.Signaling.ValueTemplate
	if tmpl == "" {
		tmpl = "{path}"
	}
	value := strings.ReplaceAll(tmpl, "{path}", filePath)
	return []string{a.desc.Signaling.Flag, value}
}

// SupportsHooks delegates to the hook strategy.
func (a *GenericAdapter) SupportsHooks() bool {
	return a.hookStrategy.SupportsHooks()
}

// SetupHooks delegates to the hook strategy.
func (a *GenericAdapter) SetupHooks(ctx HookContext) error {
	return a.hookStrategy.SetupHooks(ctx)
}

// CleanupHooks delegates to the hook strategy.
func (a *GenericAdapter) CleanupHooks(workspacePath string) error {
	return a.hookStrategy.CleanupHooks(workspacePath)
}

// WrapRemoteCommand delegates to the hook strategy.
func (a *GenericAdapter) WrapRemoteCommand(command string) (string, error) {
	return a.hookStrategy.WrapRemoteCommand(command)
}

// PersonaInjection returns how this tool receives persona overrides.
func (a *GenericAdapter) PersonaInjection() PersonaInjection {
	if a.desc.Persona == nil {
		return PersonaNone
	}
	switch a.desc.Persona.Strategy {
	case "cli_flag":
		return PersonaCLIFlag
	case "instruction_file":
		return PersonaInstructionFile
	case "config_overlay":
		return PersonaConfigOverlay
	default:
		return PersonaNone
	}
}

// PersonaArgs returns CLI args for injecting a persona file.
func (a *GenericAdapter) PersonaArgs(filePath string) []string {
	if filePath == "" {
		return nil
	}
	if a.desc.Persona == nil || a.desc.Persona.Flag == "" {
		return nil
	}
	return []string{a.desc.Persona.Flag, filePath}
}

// SpawnEnv returns additional environment variables for spawning a session.
func (a *GenericAdapter) SpawnEnv(ctx SpawnContext) map[string]string {
	hasPersonaOverlay := a.desc.Persona != nil && a.desc.Persona.Strategy == "config_overlay" &&
		ctx.PersonaPath != "" && a.desc.Persona.EnvVar != ""
	if len(a.desc.SpawnEnv) == 0 && !hasPersonaOverlay {
		return nil
	}
	env := make(map[string]string)
	for k, v := range a.desc.SpawnEnv {
		env[k] = v
	}
	if hasPersonaOverlay {
		tmpl := a.desc.Persona.ValueTemplate
		if tmpl == "" {
			tmpl = "{path}"
		}
		env[a.desc.Persona.EnvVar] = strings.ReplaceAll(tmpl, "{path}", ctx.PersonaPath)
	}
	return env
}

// SetupCommands writes template files into the workspace. Each SetupFileDesc
// references a registered template by name; if the template is found, its
// content is written to the target path relative to workspacePath.
func (a *GenericAdapter) SetupCommands(workspacePath string) error {
	for _, sf := range a.desc.SetupFiles {
		content, ok := setupTemplates[sf.Source]
		if !ok {
			continue
		}
		target := filepath.Join(workspacePath, sf.Target)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0644); err != nil {
			return err
		}
	}
	return nil
}

// InjectSkill writes a skill into the agent's native skill location.
func (a *GenericAdapter) InjectSkill(workspacePath string, skill SkillModule) error {
	if a.desc.Skills == nil {
		return fmt.Errorf("%s: skills not configured", a.desc.Name)
	}
	if a.desc.Skills.DirPattern != "" {
		dirName := strings.ReplaceAll(a.desc.Skills.DirPattern, "{name}", skill.Name)
		dirPath := filepath.Join(workspacePath, dirName)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			return fmt.Errorf("creating skill directory: %w", err)
		}
		fileName := a.desc.Skills.FileName
		if fileName == "" {
			fileName = "README.md"
		}
		return os.WriteFile(filepath.Join(dirPath, fileName), []byte(skill.Content), 0o644)
	}
	if a.desc.Skills.FilePattern != "" {
		filePath := strings.ReplaceAll(a.desc.Skills.FilePattern, "{name}", skill.Name)
		fullPath := filepath.Join(workspacePath, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("creating skill directory: %w", err)
		}
		return os.WriteFile(fullPath, []byte(skill.Content), 0o644)
	}
	return fmt.Errorf("%s: no skill pattern configured", a.desc.Name)
}

// RemoveSkill removes a previously injected skill.
func (a *GenericAdapter) RemoveSkill(workspacePath string, skillName string) error {
	if a.desc.Skills == nil {
		return fmt.Errorf("%s: skills not configured", a.desc.Name)
	}
	if a.desc.Skills.DirPattern != "" {
		dirName := strings.ReplaceAll(a.desc.Skills.DirPattern, "{name}", skillName)
		return os.RemoveAll(filepath.Join(workspacePath, dirName))
	}
	if a.desc.Skills.FilePattern != "" {
		filePath := strings.ReplaceAll(a.desc.Skills.FilePattern, "{name}", skillName)
		if err := os.Remove(filepath.Join(workspacePath, filePath)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return fmt.Errorf("%s: no skill pattern configured", a.desc.Name)
}

// BuildRunnerEnv expands the descriptor's runner_env.when_endpoint block
// against the given RunnerSpec. The block is applied only when spec.Endpoint
// is non-empty — i.e., the model is routed through this tool to a third-party
// provider. Values may use {endpoint} and {model} placeholders.
func (a *GenericAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	if a.desc.RunnerEnv == nil || len(a.desc.RunnerEnv.WhenEndpoint) == 0 {
		return nil
	}
	if spec.Endpoint == "" {
		return nil
	}
	out := make(map[string]string, len(a.desc.RunnerEnv.WhenEndpoint))
	for k, v := range a.desc.RunnerEnv.WhenEndpoint {
		v = strings.ReplaceAll(v, "{endpoint}", spec.Endpoint)
		v = strings.ReplaceAll(v, "{model}", spec.ModelValue)
		out[k] = v
	}
	return out
}

// ModelFlag returns the CLI flag for model selection.
func (a *GenericAdapter) ModelFlag() string {
	return a.desc.ModelFlag
}

// PromptFlag returns the CLI flag for prompt injection.
func (a *GenericAdapter) PromptFlag() string {
	return a.desc.PromptFlag
}

// PromptDelivery returns how this tool receives its initial user prompt.
func (a *GenericAdapter) PromptDelivery() PromptDelivery {
	if a.desc.PromptStrategy == "send_keys" {
		return PromptSendKeys
	}
	return PromptPositional
}

// Capabilities returns the tool modes this adapter supports.
// Defaults to ["interactive"] if none specified.
func (a *GenericAdapter) Capabilities() []string {
	if len(a.desc.Capabilities) == 0 {
		return []string{"interactive"}
	}
	return a.desc.Capabilities
}

// GitExcludePatterns returns gitignore patterns for files this adapter
// injects into workspaces, derived from the descriptor's hooks, skills,
// and setup_files fields.
func (a *GenericAdapter) GitExcludePatterns() []string {
	var patterns []string
	// Exclude the agent's config directory (contains instruction file,
	// hooks, skills — all managed by schmux in worktree workspaces)
	if a.desc.Instruction != nil && a.desc.Instruction.Dir != "" {
		patterns = append(patterns, a.desc.Instruction.Dir+"/")
	}
	if a.desc.Hooks != nil {
		if a.desc.Hooks.SettingsFile != "" {
			patterns = append(patterns, a.desc.Hooks.SettingsFile)
		}
		if a.desc.Hooks.PluginDir != "" && a.desc.Hooks.PluginFile != "" {
			patterns = append(patterns, a.desc.Hooks.PluginDir+"/"+a.desc.Hooks.PluginFile)
		}
	}
	if a.desc.Skills != nil {
		if a.desc.Skills.DirPattern != "" {
			// .claude/skills/schmux-{name} → .claude/skills/schmux-*/
			pattern := strings.ReplaceAll(a.desc.Skills.DirPattern, "{name}", "*")
			patterns = append(patterns, pattern+"/")
		}
		if a.desc.Skills.FilePattern != "" {
			// .opencode/commands/schmux-{name}.md → .opencode/commands/schmux-*.md
			patterns = append(patterns, strings.ReplaceAll(a.desc.Skills.FilePattern, "{name}", "*"))
		}
	}
	for _, sf := range a.desc.SetupFiles {
		patterns = append(patterns, sf.Target)
	}
	return patterns
}

// expandModelPlaceholder copies args and replaces {model} tokens with the
// model's runner value for the given adapter name. If no {model} placeholder
// is found and modelFlag is set, appends [modelFlag, modelValue] at the end.
// When model is nil or has no value, {model} tokens and their preceding flag
// arg are stripped from the output.
func expandModelPlaceholder(args []string, model *Model, adapterName, modelFlag string) []string {
	modelValue := ""
	if model != nil {
		if spec, ok := model.Runners[adapterName]; ok {
			modelValue = spec.ModelValue
		}
	}
	if modelValue == "" {
		// Strip {model} tokens and their preceding flag
		out := make([]string, 0, len(args))
		for i := 0; i < len(args); i++ {
			if strings.Contains(args[i], "{model}") {
				if len(out) > 0 {
					out = out[:len(out)-1] // remove preceding flag
				}
				continue
			}
			out = append(out, args[i])
		}
		return out
	}
	hasPlaceholder := false
	out := make([]string, len(args))
	for i, arg := range args {
		if strings.Contains(arg, "{model}") {
			hasPlaceholder = true
		}
		out[i] = strings.ReplaceAll(arg, "{model}", modelValue)
	}
	if !hasPlaceholder && modelFlag != "" {
		out = append(out, modelFlag, modelValue)
	}
	return out
}
