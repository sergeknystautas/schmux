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
// The jsonSchema parameter is optional; if provided, it should be a JSON schema for structured output.
// For Claude, this is inline JSON; for Codex, a file path.
// Returns the parsed response string from the agent.
func Execute(ctx context.Context, agentName, agentCommand, prompt, jsonSchema string, env map[string]string) (string, error) {
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
	cmdParts, err := detect.BuildCommandParts(agentName, agentCommand, detect.ToolModeOneshot, jsonSchema)
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
// The jsonSchema parameter is optional; if provided, it should be a JSON schema for structured output.
func ExecuteTarget(ctx context.Context, cfg *config.Config, targetName, prompt, jsonSchema string, timeout time.Duration) (string, error) {
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
		// User-defined targets don't support JSON schema
		return ExecuteCommand(timeoutCtx, target.Command, prompt, target.Env)
	}
	return Execute(timeoutCtx, target.ToolName, target.Command, prompt, jsonSchema, target.Env)
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
// When JSON schema is used, the output is in an envelope format:
// - Claude: JSON with "structured_output" field containing the result
// - Codex: JSONL stream; we extract the final agent_message text
func parseResponse(agentName, output string) string {
	switch agentName {
	case "claude":
		return parseClaudeStructuredOutput(output)
	case "codex":
		return parseCodexJSONLOutput(output)
	default:
		return output
	}
}

// parseClaudeStructuredOutput extracts the structured_output field from Claude's JSON response.
// If the output is not JSON or lacks structured_output, returns the output as-is.
func parseClaudeStructuredOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return output
	}

	// Try to parse as JSON envelope
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		// Not JSON, return as-is
		return output
	}

	// Look for structured_output field
	if raw, ok := envelope["structured_output"]; ok && len(raw) > 0 {
		return string(raw)
	}

	// No structured_output field, return as-is
	return output
}

// parseCodexJSONLOutput extracts the final agent_message from Codex's JSONL output.
// Codex outputs multiple JSON lines; we look for the last item.completed with agent_message type.
//
// Note: Errors from json.Unmarshal are intentionally ignored to ensure resilience.
// Malformed JSONL lines should be skipped rather than causing the entire parse to fail,
// as we only need to find the valid agent_message containing the result.
func parseCodexJSONLOutput(output string) string {
	lines := strings.Split(output, "\n")
	var lastAgentMessage string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		var event map[string]json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
			// Not a JSON line, skip
			continue
		}

		// Check if this is an item.completed event
		if eventType, ok := event["type"]; ok {
			var typeName string
			_ = json.Unmarshal(eventType, &typeName) // Error intentionally ignored
			if typeName == "item.completed" {
				// Look for the item field
				if rawItem, ok := event["item"]; ok {
					var item map[string]json.RawMessage
					if err := json.Unmarshal(rawItem, &item); err == nil {
						// Check if it's an agent_message
						if itemType, ok := item["type"]; ok {
							var itemTypeName string
							_ = json.Unmarshal(itemType, &itemTypeName) // Error intentionally ignored
							if itemTypeName == "agent_message" {
								// Extract the text field
								if textRaw, ok := item["text"]; ok {
									_ = json.Unmarshal(textRaw, &lastAgentMessage) // Error intentionally ignored
								}
							}
						}
					}
				}
			}
		}
	}

	if lastAgentMessage != "" {
		return lastAgentMessage
	}
	// Fallback: return original output
	return output
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

// NormalizeJSONPayload normalizes common JSON encoding issues that can occur
// with LLM outputs, such as fancy quotes, extra whitespace, and tabs.
// Returns an empty string if the input is empty after trimming.
func NormalizeJSONPayload(payload string) string {
	fixed := strings.TrimSpace(payload)
	if fixed == "" {
		return ""
	}
	fixed = strings.ReplaceAll(fixed, "“", "\"")
	fixed = strings.ReplaceAll(fixed, "”", "\"")
	fixed = strings.ReplaceAll(fixed, "'", "'")
	fixed = strings.ReplaceAll(fixed, "\t", " ")
	for strings.Contains(fixed, "  ") {
		fixed = strings.ReplaceAll(fixed, "  ", " ")
	}
	fixed = strings.TrimSpace(fixed)
	return fixed
}
