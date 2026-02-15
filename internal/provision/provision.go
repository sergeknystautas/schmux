// Package provision handles automatic provisioning of agent instruction files.
package provision

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/detect"
)

const (
	// Markers used to identify schmux-managed content in instruction files
	schmuxMarkerStart = "<!-- SCHMUX:BEGIN -->"
	schmuxMarkerEnd   = "<!-- SCHMUX:END -->"
)

// SignalingInstructions is the template for agent signaling instructions.
// This is appended to agent instruction files to enable direct signaling.
const SignalingInstructions = `## Schmux Status Signaling

This workspace is managed by schmux. Signal your status to help the user monitor your progress.

### How to Signal

Write your status to the file at $SCHMUX_STATUS_FILE (set in your environment). Write a single line:

` + "```" + `
echo "STATE message" > $SCHMUX_STATUS_FILE
` + "```" + `

### Available States

| State | When to Use |
|-------|-------------|
| ` + "`completed`" + ` | Task finished successfully |
| ` + "`needs_input`" + ` | Waiting for user confirmation, approval, or choice |
| ` + "`needs_testing`" + ` | Implementation ready for user to test |
| ` + "`error`" + ` | Something failed that needs user attention |
| ` + "`working`" + ` | Starting new work (include what you're doing) |

### Examples

` + "```" + `
# After finishing a task
echo "completed Implemented the login feature" > $SCHMUX_STATUS_FILE

# When you need user approval
echo "needs_input Should I delete these 5 files?" > $SCHMUX_STATUS_FILE

# When encountering an error
echo "error Build failed - missing dependency" > $SCHMUX_STATUS_FILE

# When starting new work
echo "working Refactoring auth module" > $SCHMUX_STATUS_FILE
` + "```" + `

### Best Practices

1. **Signal completion** when you finish the user's request
2. **Signal needs_input** when waiting for user decisions (don't just ask in text)
3. **Signal error** for failures that block progress
4. **Signal working** when starting a new task (describe what you're doing)
5. Keep messages concise (under 100 characters)
`

// EnsureAgentInstructions ensures the signaling instructions are present
// in the appropriate instruction file for the given target.
// Returns nil if the target doesn't have a known instruction file.
func EnsureAgentInstructions(workspacePath, targetName string) error {
	config, ok := detect.GetAgentInstructionConfigForTarget(targetName)
	if !ok {
		// Target doesn't have a known instruction file, nothing to do
		return nil
	}

	// Build the full path to the instruction file
	instructionDir := filepath.Join(workspacePath, config.InstructionDir)
	instructionPath := filepath.Join(instructionDir, config.InstructionFile)

	// Ensure the directory exists
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		return fmt.Errorf("failed to create instruction directory %s: %w", instructionDir, err)
	}

	// Build the schmux block with markers
	schmuxBlock := buildSchmuxBlock()

	// Check if file exists
	content, err := os.ReadFile(instructionPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, create it with just the schmux block
			if err := os.WriteFile(instructionPath, []byte(schmuxBlock), 0644); err != nil {
				return fmt.Errorf("failed to create instruction file %s: %w", instructionPath, err)
			}
			fmt.Printf("[provision] created %s with signaling instructions\n", instructionPath)
			return nil
		}
		return fmt.Errorf("failed to read instruction file %s: %w", instructionPath, err)
	}

	// File exists, check if schmux block is already present
	contentStr := string(content)
	if strings.Contains(contentStr, schmuxMarkerStart) {
		// Block already exists, update it
		newContent := replaceSchmuxBlock(contentStr, schmuxBlock)
		if newContent != contentStr {
			if err := os.WriteFile(instructionPath, []byte(newContent), 0644); err != nil {
				return fmt.Errorf("failed to update instruction file %s: %w", instructionPath, err)
			}
			fmt.Printf("[provision] updated signaling instructions in %s\n", instructionPath)
		}
		return nil
	}

	// Block doesn't exist, append it
	newContent := contentStr
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += "\n" + schmuxBlock

	if err := os.WriteFile(instructionPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to append to instruction file %s: %w", instructionPath, err)
	}
	fmt.Printf("[provision] appended signaling instructions to %s\n", instructionPath)
	return nil
}

// buildSchmuxBlock builds the full schmux block with markers.
func buildSchmuxBlock() string {
	return schmuxMarkerStart + "\n" + SignalingInstructions + schmuxMarkerEnd + "\n"
}

// replaceSchmuxBlock replaces an existing schmux block with the new one.
func replaceSchmuxBlock(content, newBlock string) string {
	startIdx := strings.Index(content, schmuxMarkerStart)
	endIdx := strings.Index(content, schmuxMarkerEnd)

	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		// Markers not found or malformed, just return original
		return content
	}

	// Include the end marker and any trailing newline
	endIdx += len(schmuxMarkerEnd)
	if endIdx < len(content) && content[endIdx] == '\n' {
		endIdx++
	}

	return content[:startIdx] + newBlock + content[endIdx:]
}

// RemoveAgentInstructions removes the schmux signaling block from an instruction file.
// Used for cleanup if needed.
func RemoveAgentInstructions(workspacePath, targetName string) error {
	config, ok := detect.GetAgentInstructionConfigForTarget(targetName)
	if !ok {
		return nil
	}

	instructionPath := filepath.Join(workspacePath, config.InstructionDir, config.InstructionFile)

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, schmuxMarkerStart) {
		return nil
	}

	startIdx := strings.Index(contentStr, schmuxMarkerStart)
	endIdx := strings.Index(contentStr, schmuxMarkerEnd)

	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return nil
	}

	// Include the end marker and surrounding whitespace
	endIdx += len(schmuxMarkerEnd)
	if endIdx < len(contentStr) && contentStr[endIdx] == '\n' {
		endIdx++
	}
	// Also remove preceding newline if present
	if startIdx > 0 && contentStr[startIdx-1] == '\n' {
		startIdx--
	}

	newContent := contentStr[:startIdx] + contentStr[endIdx:]

	// If file is now empty or just whitespace, remove it
	if strings.TrimSpace(newContent) == "" {
		if err := os.Remove(instructionPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	return os.WriteFile(instructionPath, []byte(newContent), 0644)
}

// SupportsSystemPromptFlag returns true if the tool supports passing
// instructions via CLI flag (e.g., --append-system-prompt for Claude).
// Tools that support this don't need file-based instruction injection.
func SupportsSystemPromptFlag(toolName string) bool {
	switch toolName {
	case "claude":
		return true
	case "codex":
		return true
	default:
		return false
	}
}

// SignalingInstructionsFilePath returns the canonical path for system prompt file injection.
func SignalingInstructionsFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".schmux", "signaling.md")
	}
	return filepath.Join(homeDir, ".schmux", "signaling.md")
}

// EnsureSignalingInstructionsFile writes signaling instructions to ~/.schmux/signaling.md.
func EnsureSignalingInstructionsFile() error {
	path := SignalingInstructionsFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create signaling directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(SignalingInstructions), 0644); err != nil {
		return fmt.Errorf("failed to write signaling instructions file: %w", err)
	}
	return nil
}

// HasSignalingInstructions checks if the instruction file for a target
// already has the schmux signaling block.
func HasSignalingInstructions(workspacePath, targetName string) bool {
	config, ok := detect.GetAgentInstructionConfigForTarget(targetName)
	if !ok {
		return false
	}

	instructionPath := filepath.Join(workspacePath, config.InstructionDir, config.InstructionFile)

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(content), schmuxMarkerStart)
}

// SupportsHooks returns true if the tool supports the Claude Code hook system
// for automatic signaling. Tools with hook support use lifecycle event hooks
// instead of system prompt injection, which is more reliable.
func SupportsHooks(baseTool string) bool {
	return baseTool == "claude"
}

// claudeHookHandler is a single hook handler in a Claude Code hooks config.
type claudeHookHandler struct {
	Type          string `json:"type"`
	Command       string `json:"command"`
	StatusMessage string `json:"statusMessage"`
}

// claudeHookMatcherGroup is a matcher group containing one or more hook handlers.
type claudeHookMatcherGroup struct {
	Matcher string              `json:"matcher,omitempty"`
	Hooks   []claudeHookHandler `json:"hooks"`
}

// signalCommand returns a shell command that writes a state to SCHMUX_STATUS_FILE.
// Guarded by env var check so it's a no-op outside schmux-managed sessions.
func signalCommand(state string) string {
	return fmt.Sprintf(`[ -n "$SCHMUX_STATUS_FILE" ] && echo "%s" > "$SCHMUX_STATUS_FILE" || true`, state)
}

// buildClaudeHooksMap returns the hooks configuration map for Claude Code signaling.
func buildClaudeHooksMap() map[string][]claudeHookMatcherGroup {
	return map[string][]claudeHookMatcherGroup{
		"SessionStart": {
			{
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       signalCommand("working"),
						StatusMessage: "schmux: signaling",
					},
				},
			},
		},
		"UserPromptSubmit": {
			{
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       signalCommand("working"),
						StatusMessage: "schmux: signaling",
					},
				},
			},
		},
		"Stop": {
			{
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       signalCommand("completed"),
						StatusMessage: "schmux: signaling",
					},
				},
			},
		},
		"Notification": {
			{
				Matcher: "permission_prompt|idle_prompt|elicitation_dialog",
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       signalCommand("needs_input"),
						StatusMessage: "schmux: signaling",
					},
				},
			},
		},
	}
}

// schmuxStatusMessagePrefix identifies hook handlers managed by schmux.
// Used to distinguish schmux hooks from user-defined hooks during merge.
const schmuxStatusMessagePrefix = "schmux:"

// ClaudeHooksJSON returns the complete .claude/settings.local.json content
// with hooks configuration for schmux signaling, as compact JSON bytes.
func ClaudeHooksJSON() ([]byte, error) {
	config := map[string]interface{}{
		"hooks": buildClaudeHooksMap(),
	}
	return json.Marshal(config)
}

// isSchmuxMatcherGroup returns true if any handler in the group has a
// statusMessage starting with the schmux prefix.
func isSchmuxMatcherGroup(group claudeHookMatcherGroup) bool {
	for _, h := range group.Hooks {
		if strings.HasPrefix(h.StatusMessage, schmuxStatusMessagePrefix) {
			return true
		}
	}
	return false
}

// mergeHooksForEvent takes existing matcher groups for a hook event and
// schmux matcher groups, removes old schmux groups from existing, and
// appends the new schmux groups.
func mergeHooksForEvent(existing, schmux []claudeHookMatcherGroup) []claudeHookMatcherGroup {
	// Keep non-schmux groups from existing
	var merged []claudeHookMatcherGroup
	for _, g := range existing {
		if !isSchmuxMatcherGroup(g) {
			merged = append(merged, g)
		}
	}
	// Append schmux groups
	merged = append(merged, schmux...)
	return merged
}

// EnsureClaudeHooks creates or updates .claude/settings.local.json in the
// workspace with Claude Code hooks for automatic schmux signaling.
// Preserves all non-hooks settings and merges with existing user hooks
// (schmux hooks are identified by statusMessage prefix and replaced in-place).
func EnsureClaudeHooks(workspacePath string) error {
	settingsDir := filepath.Join(workspacePath, ".claude")
	settingsPath := filepath.Join(settingsDir, "settings.local.json")

	// Read existing settings or start fresh
	var settings map[string]json.RawMessage
	if data, err := os.ReadFile(settingsPath); err == nil {
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			// File is malformed, start fresh
			settings = nil
		}
	}
	if settings == nil {
		settings = make(map[string]json.RawMessage)
	}

	// Parse existing hooks (if any) into typed structure for merging
	existingHooks := make(map[string][]claudeHookMatcherGroup)
	if raw, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(raw, &existingHooks); err != nil {
			// Existing hooks malformed, start fresh for hooks only
			existingHooks = make(map[string][]claudeHookMatcherGroup)
		}
	}

	// Merge: for each event, remove old schmux groups and add new ones
	schmuxHooks := buildClaudeHooksMap()
	mergedHooks := make(map[string][]claudeHookMatcherGroup)

	// Copy all existing events (with schmux groups filtered out per-event)
	for event, groups := range existingHooks {
		if schmuxGroups, hasSchmux := schmuxHooks[event]; hasSchmux {
			mergedHooks[event] = mergeHooksForEvent(groups, schmuxGroups)
		} else {
			// Event not managed by schmux, preserve as-is
			var filtered []claudeHookMatcherGroup
			for _, g := range groups {
				if !isSchmuxMatcherGroup(g) {
					filtered = append(filtered, g)
				} else {
					// Remove stale schmux hooks from events we no longer use
				}
			}
			if len(filtered) > 0 {
				mergedHooks[event] = filtered
			}
		}
	}

	// Add schmux events that didn't exist yet
	for event, groups := range schmuxHooks {
		if _, exists := mergedHooks[event]; !exists {
			mergedHooks[event] = groups
		}
	}

	// Serialize merged hooks back into settings
	hooksJSON, err := json.Marshal(mergedHooks)
	if err != nil {
		return fmt.Errorf("failed to marshal hooks config: %w", err)
	}
	settings["hooks"] = json.RawMessage(hooksJSON)

	// Write back with indentation
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}
	if err := os.WriteFile(settingsPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}
	fmt.Printf("[provision] configured Claude hooks in %s\n", settingsPath)
	return nil
}

// WrapCommandWithHooksProvisioning prepends hooks file creation to a command.
// Used for remote sessions where we can't write files via local I/O.
// The hooks file is created in the working directory before the agent starts,
// ensuring hooks are captured at Claude Code startup.
func WrapCommandWithHooksProvisioning(command string) (string, error) {
	jsonBytes, err := ClaudeHooksJSON()
	if err != nil {
		return command, fmt.Errorf("failed to build hooks JSON: %w", err)
	}
	// JSON uses double quotes only, safe to wrap in single quotes for shell
	return fmt.Sprintf("mkdir -p .claude && printf '%%s\\n' '%s' > .claude/settings.local.json && %s", string(jsonBytes), command), nil
}
