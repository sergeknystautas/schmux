package oneshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Execute runs the given agent command in one-shot (non-interactive) mode with the provided prompt.
// The agentCommand should be the detected binary path (e.g., "claude", "/home/user/.local/bin/claude").
// Returns the parsed response string from the agent.
func Execute(ctx context.Context, agentName, agentCommand, prompt string) (string, error) {
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
	cmdParts, err := buildOneShotCommand(agentName, agentCommand)
	if err != nil {
		return "", err
	}

	// Build exec command with prompt as final argument (safe from shell injection)
	execCmd := exec.CommandContext(ctx, cmdParts[0], append(cmdParts[1:], prompt)...)

	// Capture stdout and stderr
	rawOutput, err := execCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("agent %s: one-shot execution failed (command: %s): %w\noutput: %s",
			agentName, strings.Join(append(cmdParts, "<prompt>"), " "), err, string(rawOutput))
	}

	// Parse response based on agent type
	return parseResponse(agentName, string(rawOutput)), nil
}

// buildOneShotCommand builds the one-shot command parts for the given agent name and detected binary.
// Returns command parts where the first element is the binary and the rest are arguments.
func buildOneShotCommand(agentName, agentCommand string) ([]string, error) {
	// Split the agent command into base binary and existing arguments
	// This handles cases like "claude" or "/home/user/.local/bin/claude"
	parts := strings.Fields(agentCommand)
	if len(parts) == 0 {
		return nil, fmt.Errorf("agent %s: empty command", agentName)
	}

	baseCmd := parts[0]
	existingArgs := parts[1:]

	var newArgs []string
	switch agentName {
	case "claude":
		// claude -p --model haiku, preserving any existing args from detection
		newArgs = append(existingArgs, "-p", "--model", "haiku")
	case "codex":
		// codex exec --json, preserving any existing args
		newArgs = append(existingArgs, "exec", "--json")
	case "gemini":
		// gemini interactive is "gemini -i", one-shot is just "gemini"
		// Remove the -i flag if present
		var filtered []string
		for _, arg := range existingArgs {
			if arg != "-i" {
				filtered = append(filtered, arg)
			}
		}
		newArgs = filtered
	case "glm-4.7":
		// glm-4.7 takes prompt directly, no additional args needed
		newArgs = append(existingArgs, "-p")
	default:
		return nil, fmt.Errorf("unknown agent: %s (supported: claude, codex, gemini, glm-4.7)", agentName)
	}

	result := append([]string{baseCmd}, newArgs...)
	return result, nil
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
