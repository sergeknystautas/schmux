package oneshot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/schema"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// modelManager is the package-level model manager, set via SetModelManager.
var modelManager *models.Manager

// SetModelManager sets the package-level model manager for target resolution.
func SetModelManager(mm *models.Manager) {
	modelManager = mm
}

// ErrTargetNotFound is returned when a target name cannot be resolved.
var ErrTargetNotFound = errors.New("target not found")

// CommandInfo describes the resolved command and environment for a oneshot target,
// without executing it. Useful for generating reproducible run scripts.
type CommandInfo struct {
	Args []string          // Full command arguments (e.g. ["claude", "--print", ...])
	Env  map[string]string // Extra environment variables (secrets, model config)
}

// ResolveTargetCommand resolves a target name to its full command and env
// without executing it. The streaming parameter controls whether to resolve
// for streaming mode (oneshot-streaming) or regular oneshot mode.
func ResolveTargetCommand(cfg *config.Config, targetName, schemaLabel string, streaming bool) (*CommandInfo, error) {
	target, err := resolveTarget(cfg, targetName)
	if err != nil {
		return nil, err
	}
	if !target.Promptable {
		return nil, fmt.Errorf("target %s must be promptable", targetName)
	}

	if target.Kind == targetKindUser {
		// User-defined targets: command is the raw command string
		parts := strings.Fields(target.Command)
		return &CommandInfo{Args: parts, Env: target.Env}, nil
	}

	// Resolve schema
	schemaArg := ""
	if schemaLabel != "" {
		schemaPath, err := resolveSchema(schemaLabel)
		if err != nil {
			return nil, err
		}
		if target.ToolName == "claude" {
			content, err := os.ReadFile(schemaPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read schema file %s: %w", schemaPath, err)
			}
			schemaArg = string(content)
		} else {
			schemaArg = schemaPath
		}
	}

	mode := detect.ToolModeOneshot
	if streaming {
		mode = detect.ToolModeOneshotStreaming
	}

	cmdParts, err := detect.BuildCommandParts(target.ToolName, target.Command, mode, schemaArg, target.Model)
	if err != nil {
		return nil, err
	}

	return &CommandInfo{Args: cmdParts, Env: target.Env}, nil
}

// Schema labels - re-exported from schema package for backwards compatibility.
const (
	SchemaConflictResolve = schema.LabelConflictResolve
	SchemaNudgeNik        = schema.LabelNudgeNik
	SchemaBranchSuggest   = schema.LabelBranchSuggest
)

// Execute runs the given agent command in one-shot (non-interactive) mode with the provided prompt.
// The agentCommand should be the detected binary path (e.g., "claude", "/home/user/.local/bin/claude").
// The schemaLabel parameter is required; must be a registered schema label for structured output.
// The model parameter is optional; if provided, it will be used to inject model-specific flags.
// Returns the parsed response string from the agent.
func Execute(ctx context.Context, agentName, agentCommand, prompt, schemaLabel string, env map[string]string, dir string, model *detect.Model) (string, error) {
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

	// Resolve schema label to a file path, then read inline for Claude
	schemaArg := ""
	if schemaLabel != "" {
		schemaPath, err := resolveSchema(schemaLabel)
		if err != nil {
			return "", err
		}
		if agentName == "claude" {
			content, err := os.ReadFile(schemaPath)
			if err != nil {
				return "", fmt.Errorf("failed to read schema file %s: %w", schemaPath, err)
			}
			schemaArg = string(content)
		} else {
			schemaArg = schemaPath
		}
	}

	// Build command parts safely
	cmdParts, err := detect.BuildCommandParts(agentName, agentCommand, detect.ToolModeOneshot, schemaArg, model)
	if err != nil {
		return "", err
	}

	// Build exec command - prompt passed via stdin
	execCmd := exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
	// For Codex, add "-" placeholder to indicate stdin; Claude/Gemini read from stdin automatically
	if agentName == "codex" {
		execCmd.Args = append(execCmd.Args, "-")
	}
	execCmd.Stdin = strings.NewReader(prompt)
	if len(env) > 0 {
		execCmd.Env = mergeEnv(env)
	}
	if dir != "" {
		execCmd.Dir = dir
	}

	// Capture stdout and stderr
	rawOutput, err := execCmd.CombinedOutput()
	if err != nil {
		// The process may have been killed by timeout after producing valid output.
		// Attempt to parse the output before giving up.
		if parsed := parseResponse(agentName, string(rawOutput)); parsed != string(rawOutput) {
			// parseResponse extracted structured data — the output was valid despite exit error
			return parsed, nil
		}
		return "", fmt.Errorf("agent %s: one-shot execution failed (command: %s): %w\noutput: %s",
			agentName, strings.Join(append(cmdParts, "<prompt>"), " "), err, string(rawOutput))
	}

	// Parse response based on agent type
	return parseResponse(agentName, string(rawOutput)), nil
}

// ExecuteCommand runs an arbitrary promptable command in one-shot mode, appending the prompt as the final argument.
// This is used for user-defined promptable run targets.
func ExecuteCommand(ctx context.Context, command, prompt string, env map[string]string, dir string) (string, error) {
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

	execCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	execCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	execCmd.Stdin = strings.NewReader(prompt)
	if len(env) > 0 {
		execCmd.Env = mergeEnv(env)
	}
	if dir != "" {
		execCmd.Dir = dir
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
// The schemaLabel parameter is optional; if empty, no JSON schema constraint is applied
// (the CLI will still use JSON output format, but without constrained decoding).
func ExecuteTarget(ctx context.Context, cfg *config.Config, targetName, prompt, schemaLabel string, timeout time.Duration, dir string) (string, error) {
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
		return ExecuteCommand(timeoutCtx, target.Command, prompt, target.Env, dir)
	}
	return Execute(timeoutCtx, target.ToolName, target.Command, prompt, schemaLabel, target.Env, dir, target.Model)
}

// StreamEvent represents a single JSON event from claude's stream-json output.
type StreamEvent struct {
	Type    string          `json:"type"`            // "system", "assistant", "user", "result"
	Subtype string          `json:"subtype"`         // for system events: "init", "hook_started", etc.
	Error   json.RawMessage `json:"error,omitempty"` // present when the event carries an error (API failures, etc.)
	Raw     json.RawMessage `json:"-"`               // the full raw JSON line
}

// ResultEvent is the final event in a stream-json output, containing structured output.
type ResultEvent struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	DurationMs       int64           `json:"duration_ms"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// ExecuteTargetStreaming runs a one-shot execution with stream-json output,
// calling onEvent for each JSON event as it arrives. Returns the structured_output
// from the final result event.
func ExecuteTargetStreaming(ctx context.Context, cfg *config.Config, targetName, prompt, schemaLabel string, timeout time.Duration, dir string, onEvent func(StreamEvent)) (string, error) {
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
	if target.Kind == targetKindUser {
		return "", fmt.Errorf("streaming mode is not supported for user-defined targets")
	}

	// Resolve schema
	schemaArg := ""
	if schemaLabel != "" {
		schemaPath, err := resolveSchema(schemaLabel)
		if err != nil {
			return "", err
		}
		if target.ToolName == "claude" {
			content, err := os.ReadFile(schemaPath)
			if err != nil {
				return "", fmt.Errorf("failed to read schema file %s: %w", schemaPath, err)
			}
			schemaArg = string(content)
		} else {
			schemaArg = schemaPath
		}
	}

	// Build command with streaming mode
	cmdParts, err := detect.BuildCommandParts(target.ToolName, target.Command, detect.ToolModeOneshotStreaming, schemaArg, target.Model)
	if err != nil {
		return "", err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execCmd := exec.CommandContext(timeoutCtx, cmdParts[0], cmdParts[1:]...)
	execCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	execCmd.Stdin = strings.NewReader(prompt)
	if len(target.Env) > 0 {
		execCmd.Env = mergeEnv(target.Env)
	}
	if dir != "" {
		execCmd.Dir = dir
	}

	// Set up stdout pipe for streaming
	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	execCmd.Stderr = &stderrBuf

	if err := execCmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	// Kill the entire process group on context cancellation so grandchild
	// processes don't keep the stdout pipe open (which would block scanner.Scan).
	go func() {
		<-timeoutCtx.Done()
		if execCmd.Process != nil {
			_ = syscall.Kill(-execCmd.Process.Pid, syscall.SIGKILL)
		}
	}()

	// Read stdout line by line
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1MB per line

	var resultEvent *ResultEvent
	var sawErrors bool
	var allText strings.Builder // collect all assistant text for fallback
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse type and subtype
		var ev StreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip non-JSON lines
		}
		// Store raw bytes (make a copy since scanner reuses buffer)
		raw := make([]byte, len(line))
		copy(raw, line)
		ev.Raw = json.RawMessage(raw)

		if onEvent != nil {
			onEvent(ev)
		}

		// Collect assistant text blocks for fallback output extraction.
		// Without constrained decoding, LLMs may use tools and produce multiple
		// text blocks across turns; the result event only captures the last one.
		if ev.Type == "assistant" {
			if text := extractAssistantText(raw); text != "" {
				allText.WriteString(text)
			}
		}

		// Track error events
		if ev.Type == "error" || strings.HasSuffix(ev.Type, "_error") || len(ev.Error) > 0 {
			sawErrors = true
		}

		// Track result event
		if ev.Type == "result" {
			var re ResultEvent
			if err := json.Unmarshal(line, &re); err == nil {
				resultEvent = &re
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timed out after %s — the LLM took too long to respond", timeout)
		}
		return "", fmt.Errorf("error reading stdout: %w", err)
	}

	// Wait for command to finish
	waitErr := execCmd.Wait()

	if resultEvent != nil {
		if resultEvent.IsError {
			return "", fmt.Errorf("agent returned error: %s", resultEvent.Result)
		}
		if len(resultEvent.StructuredOutput) > 0 {
			return string(resultEvent.StructuredOutput), nil
		}
		// Without constrained decoding, the result event only captures the last
		// text block. Use the full collected text so callers can find JSON that
		// appeared in earlier turns.
		if collected := allText.String(); collected != "" {
			return collected, nil
		}
		return resultEvent.Result, nil
	}

	// Check for timeout after wait
	if timeoutCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s — the LLM took too long to respond", timeout)
	}

	if waitErr != nil {
		stderr := stderrBuf.String()
		if stderr != "" {
			return "", fmt.Errorf("agent failed: %w\nstderr: %s", waitErr, stderr)
		}
		return "", fmt.Errorf("agent failed: %w", waitErr)
	}

	if sawErrors {
		return "", fmt.Errorf("LLM API returned errors (see event log above)")
	}

	return "", fmt.Errorf("no result event found in stream output")
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

// extractAssistantText extracts text content from an assistant stream event.
// Assistant events have structure: {"type":"assistant","message":{"content":[{"type":"text","text":"..."},...]}}
func extractAssistantText(raw []byte) string {
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Type != "assistant" {
		return ""
	}
	var sb strings.Builder
	for _, c := range ev.Message.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
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

// parseClaudeStructuredOutput extracts the response text from Claude's JSON envelope.
// It prefers structured_output (present when --json-schema is used), falling back to
// the result field (plain text responses). The result field is JSON-unquoted so that
// escaped newlines become real newlines.
func parseClaudeStructuredOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return output
	}

	// Some CLI tools print a banner line to stdout before the JSON result,
	// and CombinedOutput may append stderr after it. Find the JSON object
	// bounds using the first '{' and last '}' to ignore surrounding noise.
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end < 0 || end <= start {
		return output
	}
	jsonStr := trimmed[start : end+1]

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &envelope); err != nil {
		return output
	}

	if raw, ok := envelope["structured_output"]; ok && len(raw) > 0 && string(raw) != "null" {
		return string(raw)
	}

	// Fall back to the result field (plain text response without a schema).
	// JSON-unquote it so that escaped newlines (\n) become real newlines.
	if raw, ok := envelope["result"]; ok && len(raw) > 0 {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			return text
		}
	}

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

func resolveSchema(label string) (string, error) {
	// Validate that the schema exists by attempting to get it
	if _, err := schema.Get(label); err != nil {
		return "", err
	}

	path := filepath.Join(schmuxdir.Get(), "schemas", label+".json")

	if _, err := os.Stat(path); err != nil {
		// File missing — write it (shouldn't happen if daemon started correctly)
		if err := WriteAllSchemas(); err != nil {
			return "", err
		}
	}

	return path, nil
}

// WriteAllSchemas writes all registered schemas to the schema directory,
// unconditionally overwriting any existing files. This should be called
// on daemon startup to ensure schemas are always up to date.
func WriteAllSchemas() error {
	dir := filepath.Join(schmuxdir.Get(), "schemas")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create schema directory: %w", err)
	}

	for _, label := range schema.Labels() {
		schemaJSON, err := schema.Get(label)
		if err != nil {
			return fmt.Errorf("failed to get schema %s: %w", label, err)
		}
		path := filepath.Join(dir, label+".json")
		if err := os.WriteFile(path, []byte(schemaJSON), 0644); err != nil {
			return fmt.Errorf("failed to write schema file %s: %w", label, err)
		}
	}
	return nil
}

// resolvedTarget represents a fully resolved oneshot target with all env vars merged.
type resolvedTarget struct {
	Name       string
	Kind       string
	ToolName   string
	Command    string
	Promptable bool
	Env        map[string]string
	Model      *detect.Model
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

	// Check if it's a model via the model manager
	if modelManager != nil && modelManager.IsModelID(targetName) {
		resolved, err := modelManager.ResolveModel(targetName)
		if err != nil {
			return resolvedTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
		}
		return resolvedTarget{
			Name:       resolved.Model.ID,
			Kind:       targetKindModel,
			ToolName:   resolved.ToolName,
			Command:    resolved.Command,
			Promptable: true,
			Env:        resolved.Env,
			Model:      &resolved.Model,
		}, nil
	}

	// Check regular run targets (command targets only)
	if target, found := cfg.GetRunTarget(targetName); found {
		return resolvedTarget{
			Name:       target.Name,
			Kind:       targetKindUser,
			Command:    target.Command,
			Promptable: false,
			Env:        nil,
			Model:      nil,
		}, nil
	}

	return resolvedTarget{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetName)
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
