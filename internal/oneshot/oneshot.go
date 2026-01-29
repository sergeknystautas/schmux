package oneshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

// ErrTargetNotFound is returned when a target name cannot be resolved.
var ErrTargetNotFound = errors.New("target not found")

// Execute runs the given agent command in one-shot (non-interactive) mode with the provided prompt.
// The agentCommand should be the detected binary path (e.g., "claude", "/home/user/.local/bin/claude").
// Returns the parsed response string from the agent.
func Execute(ctx context.Context, agentName, agentCommand, prompt string, env map[string]string) (string, error) {
	// Validate inputs
	if agentName == "" {
		return "", fmt.Errorf("agent name cannot be empty")
	}
	if agentCommand == "" {
		return "", fmt.Errorf("agent command cannot be empty")
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt cannot be empty")
	}

	// Build command parts safely
	cmdParts, err := detect.BuildCommandParts(agentName, agentCommand, detect.ToolModeOneshot)
	if err != nil {
		return "", err
	}

	// Build exec command with prompt as final argument (safe from shell injection)
	execCmd := exec.CommandContext(ctx, cmdParts[0], append(cmdParts[1:], prompt)...)
	if len(env) > 0 {
		execCmd.Env = mergeEnv(env)
	}

	// Capture stdout and stderr
	rawOutput, err := execCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("agent %s: one-shot execution failed (command: %s): %w\noutput: %s",
			agentName, strings.Join(append(cmdParts, "<prompt>"), " "), err, string(rawOutput))
	}

	// Parse response based on agent type
	return parseResponse(agentName, string(rawOutput)), nil
}

// ExecuteCommand runs an arbitrary promptable command in one-shot mode, appending the prompt as the final argument.
// This is used for user-defined promptable run targets.
func ExecuteCommand(ctx context.Context, command, prompt string, env map[string]string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt cannot be empty")
	}

	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("command cannot be empty")
	}

	execCmd := exec.CommandContext(ctx, parts[0], append(parts[1:], prompt)...)
	if len(env) > 0 {
		execCmd.Env = mergeEnv(env)
	}

	rawOutput, err := execCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command: one-shot execution failed (command: %s): %w\noutput: %s",
			strings.Join(append(parts, "<prompt>"), " "), err, string(rawOutput))
	}

	return string(rawOutput), nil
}

// ExecuteTarget runs a one-shot execution for a named target from config.
// It resolves models, loads secrets, and merges env vars automatically.
// This is the preferred way to execute oneshot commands for promptable targets.
// The timeout parameter controls how long to wait for the one-shot execution to complete.
func ExecuteTarget(ctx context.Context, cfg *config.Config, targetName, prompt string, timeout time.Duration) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("prompt cannot be empty")
	}

	target, err := resolveTarget(cfg, targetName)
	if err != nil {
		return "", err
	}
	if !target.Promptable {
		return "", fmt.Errorf("target %s must be promptable", targetName)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if target.Kind == targetKindUser {
		return ExecuteCommand(timeoutCtx, target.Command, prompt, target.Env)
	}
	return Execute(timeoutCtx, target.ToolName, target.Command, prompt, target.Env)
}

func mergeEnv(extra map[string]string) []string {
	base := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			base[parts[0]] = parts[1]
		}
	}
	for k, v := range extra {
		base[k] = v
	}
	result := make([]string, 0, len(base))
	for k, v := range base {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

// parseResponse parses the raw output from an agent into a clean response string.
func parseResponse(agentName, output string) string {
	switch agentName {
	case "gemini":
		return parseGeminiOneShot(output)
	case "claude":
		// Claude returns clean output, no parsing needed
		return output
	case "codex":
		// TODO: Parse JSONL response from codex exec --json
		// For now, return full output
		return output
	default:
		return output
	}
}

// parseGeminiOneShot strips the "Loaded cached credentials." line from gemini output.
func parseGeminiOneShot(output string) string {
	lines := strings.Split(output, "\n")
	// Filter out the credentials message
	var filtered []string
	for _, line := range lines {
		if line != "Loaded cached credentials." {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

// CodexJSONL represents a single JSONL line from codex --json output.
// TODO: Implement JSONL parsing for codex responses.
type CodexJSONL struct {
	// Fields will be added when we implement JSONL parsing
}

// ParseCodexJSONL parses JSONL output from codex exec --json.
// TODO: Implement this function to handle streaming JSONL responses.
func ParseCodexJSONL(output string) ([]CodexJSONL, error) {
	// Placeholder for future implementation
	var results []CodexJSONL
	decoder := json.NewDecoder(strings.NewReader(output))
	for decoder.More() {
		var line CodexJSONL
		if err := decoder.Decode(&line); err != nil {
			return nil, fmt.Errorf("failed to parse codex JSONL: %w", err)
		}
		results = append(results, line)
	}
	return results, nil
}

// resolvedTarget represents a fully resolved oneshot target with all env vars merged.
type resolvedTarget struct {
	Name       string
	Kind       string
	ToolName   string
	Command    string
	Promptable bool
	Env        map[string]string
}

const (
	targetKindDetected = "detected"
	targetKindModel    = "model"
	targetKindUser     = "user"
)

// resolveTarget resolves a target name to its full configuration including models and secrets.
func resolveTarget(cfg *config.Config, targetName string) (resolvedTarget, error) {
	if cfg == nil {
		return resolvedTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
	}

	// Check if it's a model (handles aliases like "opus", "sonnet", "haiku")
	model, ok := detect.FindModel(targetName)
	if ok {
		// Verify the base tool is detected
		detectedTools := config.DetectedToolsFromConfig(cfg)
		baseToolDetected := false
		for _, tool := range detectedTools {
			if tool.Name == model.BaseTool {
				baseToolDetected = true
				break
			}
		}
		if !baseToolDetected {
			return resolvedTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
		}
		baseTarget, found := cfg.GetDetectedRunTarget(model.BaseTool)
		if !found {
			return resolvedTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
		}
		secrets, err := config.GetEffectiveModelSecrets(model)
		if err != nil {
			return resolvedTarget{}, fmt.Errorf("failed to load secrets for model %s: %w", model.ID, err)
		}
		if err := ensureModelSecrets(model, secrets); err != nil {
			return resolvedTarget{}, err
		}
		return resolvedTarget{
			Name:       model.ID,
			Kind:       targetKindModel,
			ToolName:   model.BaseTool,
			Command:    baseTarget.Command,
			Promptable: true,
			Env:        mergeEnvMaps(model.BuildEnv(), secrets),
		}, nil
	}

	// Check regular run targets
	if target, found := cfg.GetRunTarget(targetName); found {
		kind := targetKindUser
		toolName := ""
		if target.Source == config.RunTargetSourceDetected {
			kind = targetKindDetected
			toolName = target.Name
		}
		return resolvedTarget{
			Name:       target.Name,
			Kind:       kind,
			ToolName:   toolName,
			Command:    target.Command,
			Promptable: target.Type == config.RunTargetTypePromptable,
			Env:        nil,
		}, nil
	}

	return resolvedTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
}

func mergeEnvMaps(base, overrides map[string]string) map[string]string {
	if base == nil && overrides == nil {
		return nil
	}
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

func ensureModelSecrets(model detect.Model, secrets map[string]string) error {
	return config.EnsureModelSecrets(model, secrets)
}
