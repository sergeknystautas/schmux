package oneshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sergeknystautas/schmux/internal/detect"
)

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
