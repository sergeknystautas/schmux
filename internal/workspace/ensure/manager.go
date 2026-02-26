// Package ensure handles ensuring workspaces have the necessary schmux configuration.
package ensure

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/state"
)

// pkgLogger is the package-level logger, set via SetLogger.
var pkgLogger *log.Logger

// SetLogger configures the package-level logger for the ensure package.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

const (
	// Markers used to identify schmux-managed content in instruction files
	schmuxMarkerStart = "<!-- SCHMUX:BEGIN -->"
	schmuxMarkerEnd   = "<!-- SCHMUX:END -->"
)

// Ensurer ensures workspaces have the necessary schmux configuration.
// It holds a state reference to make decisions about what configuration
// a workspace needs based on active sessions.
type Ensurer struct {
	state    state.StateStore
	hooksDir string // absolute path to centralized hooks directory (~/.schmux/hooks/)
}

// New creates a new Ensurer with access to state for session lookups.
func New(st state.StateStore) *Ensurer {
	return &Ensurer{state: st}
}

// SetHooksDir sets the centralized hooks directory path.
func (e *Ensurer) SetHooksDir(dir string) {
	e.hooksDir = dir
}

// ForSpawn ensures a workspace has all necessary schmux configuration
// when a new session is being spawned. The currentTarget is the agent
// being spawned (needed because the session doesn't exist in state yet).
func (e *Ensurer) ForSpawn(workspaceID, currentTarget string) error {
	w, found := e.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	hasClaude := e.workspaceHasClaude(workspaceID) ||
		SupportsHooks(detect.GetBaseToolName(currentTarget))
	return e.ensureWorkspace(w.Path, hasClaude)
}

// ForWorkspace ensures a workspace has all necessary schmux configuration.
// Used for daemon startup and overlay refresh when there's no spawn context.
func (e *Ensurer) ForWorkspace(workspaceID string) error {
	w, found := e.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	return e.ensureWorkspace(w.Path, e.workspaceHasClaude(workspaceID))
}

// workspaceHasClaude returns true if any session in this workspace uses Claude.
func (e *Ensurer) workspaceHasClaude(workspaceID string) bool {
	for _, s := range e.state.GetSessions() {
		if s.WorkspaceID == workspaceID {
			if SupportsHooks(detect.GetBaseToolName(s.Target)) {
				return true
			}
		}
	}
	return false
}

// ensureWorkspace writes all schmux-managed configuration for a workspace.
func (e *Ensurer) ensureWorkspace(workspacePath string, hasClaude bool) error {
	if hasClaude {
		if err := ClaudeHooks(workspacePath, e.hooksDir); err != nil {
			fmt.Printf("[ensure] warning: failed to ensure Claude hooks: %v\n", err)
		}
	}
	if err := GitExclude(workspacePath); err != nil {
		fmt.Printf("[ensure] warning: failed to ensure git exclude: %v\n", err)
	}
	return nil
}

// SignalingInstructions is the template for agent signaling instructions.
// This is appended to agent instruction files to enable direct signaling.
const SignalingInstructions = `## Schmux Status Signaling

This workspace is managed by schmux. Signal your status to help the user monitor your progress.

### How to Signal

Append structured events to $SCHMUX_EVENTS_FILE (per-session append-only JSONL):

` + "```" + `
echo '{"ts":"<ISO8601>","type":"status","state":"STATE","message":"what you did"}' >> "$SCHMUX_EVENTS_FILE"
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
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"completed","message":"Implemented the login feature"}' >> "$SCHMUX_EVENTS_FILE"

# When you need user approval
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"needs_input","message":"Should I delete these 5 files?"}' >> "$SCHMUX_EVENTS_FILE"

# When encountering an error
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"error","message":"Build failed - missing dependency"}' >> "$SCHMUX_EVENTS_FILE"

# When starting new work
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"Refactoring auth module"}' >> "$SCHMUX_EVENTS_FILE"
` + "```" + `

### Best Practices

1. **Signal completion** when you finish the user's request
2. **Signal needs_input** when waiting for user decisions (don't just ask in text)
3. **Signal error** for failures that block progress
4. **Signal working** when starting a new task (describe what you're doing)
5. Keep messages concise (under 100 characters)

## Friction Capture

When you hit a wall — wrong command, missing file, failed build, wrong assumption —
append a reflection event to ` + "`$SCHMUX_EVENTS_FILE`" + `:

` + "```" + `
echo '{"ts":"<ISO8601>","type":"reflection","text":"When <trigger>, do <correction> instead"}' >> "$SCHMUX_EVENTS_FILE"
` + "```" + `

Only write when something tripped you up. Don't write what you built or learned —
write what would have saved you time if you'd known it before starting.
`

// AgentInstructions ensures the signaling instructions are present
// in the appropriate instruction file for the given target.
// Returns nil if the target doesn't have a known instruction file.
func AgentInstructions(workspacePath, targetName string) error {
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
			if pkgLogger != nil {
				pkgLogger.Info("created instruction file with signaling instructions", "path", instructionPath)
			}
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
			if pkgLogger != nil {
				pkgLogger.Info("updated signaling instructions", "path", instructionPath)
			}
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
	if pkgLogger != nil {
		pkgLogger.Info("appended signaling instructions", "path", instructionPath)
	}
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

// SignalingInstructionsFile writes signaling instructions to ~/.schmux/signaling.md.
func SignalingInstructionsFile() error {
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
// statusEventCommand returns a shell command that appends a status event to the event file.
func statusEventCommand(state, messageExpr string) string {
	return fmt.Sprintf(
		`[ -n "$SCHMUX_EVENTS_FILE" ] && printf "{\"ts\":\"%%s\",\"type\":\"status\",\"state\":\"%s\",\"message\":\"%%s\"}\n" "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" "%s" >> "$SCHMUX_EVENTS_FILE" || true`,
		state, messageExpr,
	)
}

// statusEventWithContextCommand returns a shell command that extracts a JSON field
// and appends a status event with that field as both message and intent.
// Uses jq -Rs for safe JSON escaping (handles quotes, special chars).
// Detects [from FM] prefix to add "source":"floor-manager".
// Note: No single quotes allowed — the output is embedded in JSON that gets wrapped in single quotes by the shell.
func statusEventWithContextCommand(state, jqField string) string {
	return fmt.Sprintf(
		`[ -n "$SCHMUX_EVENTS_FILE" ] && { MSG=$(jq -r ".%s // empty" 2>/dev/null | tr -d "\n" || true); SRC=""; case "$MSG" in "[from FM] "*) SRC=",\"source\":\"floor-manager\"" ;; esac; EMSG=$(printf "%%s" "$MSG" | jq -Rs .); printf "{\"ts\":\"%%s\",\"type\":\"status\",\"state\":\"%s\",\"message\":%%s,\"intent\":%%s%%s}\n" "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" "$EMSG" "$EMSG" "$SRC" >> "$SCHMUX_EVENTS_FILE"; } || true`,
		jqField, state,
	)
}

// buildClaudeHooksMap returns the hooks configuration map for Claude Code signaling.
// hooksDir is the absolute path to the centralized hooks directory (~/.schmux/hooks/).
// When hooksDir is empty, falls back to $CLAUDE_PROJECT_DIR/.schmux/hooks/ paths.
func buildClaudeHooksMap(hooksDir string) map[string][]claudeHookMatcherGroup {
	// Build stop hook commands using centralized or per-workspace paths
	var stopStatusCmd, stopLoreCmd, captureFailureCmd string
	if hooksDir != "" {
		stopStatusPath := filepath.Join(hooksDir, "stop-status-check.sh")
		stopLorePath := filepath.Join(hooksDir, "stop-lore-check.sh")
		captureFailurePath := filepath.Join(hooksDir, "capture-failure.sh")
		stopStatusCmd = fmt.Sprintf(`[ -f "%s" ] && "%s" || true`, stopStatusPath, stopStatusPath)
		stopLoreCmd = fmt.Sprintf(`[ -f "%s" ] && "%s" || true`, stopLorePath, stopLorePath)
		captureFailureCmd = fmt.Sprintf(`[ -f "%s" ] && "%s" || true`, captureFailurePath, captureFailurePath)
	} else {
		stopStatusCmd = `[ -f "$CLAUDE_PROJECT_DIR/.schmux/hooks/stop-status-check.sh" ] && "$CLAUDE_PROJECT_DIR"/.schmux/hooks/stop-status-check.sh || true`
		stopLoreCmd = `[ -f "$CLAUDE_PROJECT_DIR/.schmux/hooks/stop-lore-check.sh" ] && "$CLAUDE_PROJECT_DIR"/.schmux/hooks/stop-lore-check.sh || true`
		captureFailureCmd = `[ -f "$CLAUDE_PROJECT_DIR/.schmux/hooks/capture-failure.sh" ] && "$CLAUDE_PROJECT_DIR"/.schmux/hooks/capture-failure.sh || true`
	}

	hooks := map[string][]claudeHookMatcherGroup{
		"SessionStart": {
			{
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       statusEventCommand("working", ""),
						StatusMessage: "schmux: signaling",
					},
				},
			},
		},
		"SessionEnd": {
			{
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       statusEventCommand("completed", ""),
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
						Command:       statusEventWithContextCommand("working", "prompt"),
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
						Command:       statusEventCommand("idle", ""),
						StatusMessage: "schmux: signaling",
					},
				},
			},
		},
		"Notification": {
			{
				Matcher: "permission_prompt|elicitation_dialog",
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       statusEventWithContextCommand("needs_input", "message"),
						StatusMessage: "schmux: signaling",
					},
				},
			},
		},
		"PostToolUseFailure": {
			{
				Hooks: []claudeHookHandler{
					{
						Type:          "command",
						Command:       captureFailureCmd,
						StatusMessage: "schmux: lore capture",
					},
				},
			},
		},
	}

	// Add stop hooks if available
	if stopStatusCmd != "" {
		hooks["Stop"] = append(hooks["Stop"], claudeHookMatcherGroup{
			Hooks: []claudeHookHandler{
				{
					Type:          "command",
					Command:       stopStatusCmd,
					StatusMessage: "schmux: signaling",
				},
			},
		})
	}
	if stopLoreCmd != "" {
		hooks["Stop"] = append(hooks["Stop"], claudeHookMatcherGroup{
			Hooks: []claudeHookHandler{
				{
					Type:          "command",
					Command:       stopLoreCmd,
					StatusMessage: "schmux: lore check",
				},
			},
		})
	}

	return hooks
}

// schmuxStatusMessagePrefix identifies hook handlers managed by schmux.
// Used to distinguish schmux hooks from user-defined hooks during merge.
const schmuxStatusMessagePrefix = "schmux:"

// ClaudeHooksJSON returns the complete .claude/settings.local.json content
// with hooks configuration for schmux signaling, as compact JSON bytes.
func ClaudeHooksJSON(hooksDir string) ([]byte, error) {
	config := map[string]interface{}{
		"hooks": buildClaudeHooksMap(hooksDir),
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

// ClaudeHooks creates or updates .claude/settings.local.json in the
// workspace with Claude Code hooks for automatic schmux signaling.
// Preserves all non-hooks settings and merges with existing user hooks
// (schmux hooks are identified by statusMessage prefix and replaced in-place).
func ClaudeHooks(workspacePath, hooksDir string) error {
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
	schmuxHooks := buildClaudeHooksMap(hooksDir)
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
	if pkgLogger != nil {
		pkgLogger.Info("configured Claude hooks", "path", settingsPath)
	}
	return nil
}

// WrapCommandWithHooks prepends hooks file creation to a command.
// Used for remote sessions where we can't write files via local I/O.
// The hooks file is created in the working directory before the agent starts,
// ensuring hooks are captured at Claude Code startup.
func WrapCommandWithHooks(command string) (string, error) {
	jsonBytes, err := ClaudeHooksJSON("")
	if err != nil {
		return command, fmt.Errorf("failed to build hooks JSON: %w", err)
	}
	// JSON uses double quotes only, safe to wrap in single quotes for shell
	return fmt.Sprintf("mkdir -p .claude && printf '%%s\\n' '%s' > .claude/settings.local.json && %s", string(jsonBytes), command), nil
}

//go:embed hooks/capture-failure.sh
var captureFailureScript []byte

//go:embed hooks/stop-status-check.sh
var stopStatusCheckScript []byte

//go:embed hooks/stop-lore-check.sh
var stopLoreCheckScript []byte

// Markers for the schmux-managed block in .git/info/exclude (gitignore comment syntax).
const (
	excludeMarkerStart = "# SCHMUX:BEGIN - managed by schmux, do not edit"
	excludeMarkerEnd   = "# SCHMUX:END"
)

// excludePatterns are the gitignore patterns managed by schmux.
// These cover daemon-written files that should not appear in git status.
var excludePatterns = []string{
	".schmux/hooks/",
	".schmux/events/",
}

// buildExcludeBlock builds the full schmux exclude block with markers.
func buildExcludeBlock() string {
	var b strings.Builder
	b.WriteString(excludeMarkerStart)
	b.WriteByte('\n')
	for _, p := range excludePatterns {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	b.WriteString(excludeMarkerEnd)
	b.WriteByte('\n')
	return b.String()
}

// GitExclude ensures that daemon-managed .schmux/ paths are excluded from
// git status by writing patterns to .git/info/exclude (or the shared git
// dir's info/exclude for worktrees).
func GitExclude(workspacePath string) error {
	// Resolve the shared git directory (handles both full clones and worktrees).
	cmd := exec.Command("git", "-C", workspacePath, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse --git-common-dir failed: %w", err)
	}
	gitCommonDir := strings.TrimSpace(string(out))

	// git returns a relative path for full clones (e.g. ".git"),
	// and an absolute path for worktrees.
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(workspacePath, gitCommonDir)
	}

	excludePath := filepath.Join(gitCommonDir, "info", "exclude")
	return ensureExcludeEntries(excludePath)
}

// ensureExcludeEntries ensures the schmux exclude block is present and
// up-to-date in the given exclude file. It creates the file and parent
// directories if they don't exist, preserves existing user entries, and
// is idempotent (running twice produces an identical file).
func ensureExcludeEntries(excludePath string) error {
	block := buildExcludeBlock()

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return fmt.Errorf("failed to create info directory: %w", err)
	}

	// Read existing file (empty if it doesn't exist yet).
	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read exclude file: %w", err)
	}
	existing := string(content)

	var newContent string
	if strings.Contains(existing, excludeMarkerStart) {
		// Replace existing schmux block.
		newContent = replaceExcludeBlock(existing, block)
	} else {
		// Append block, ensuring a blank line separator.
		newContent = existing
		if len(newContent) > 0 && !strings.HasSuffix(newContent, "\n") {
			newContent += "\n"
		}
		if len(newContent) > 0 {
			newContent += "\n"
		}
		newContent += block
	}

	// Skip write if content is unchanged.
	if newContent == existing {
		return nil
	}

	return os.WriteFile(excludePath, []byte(newContent), 0644)
}

// replaceExcludeBlock replaces the schmux block (between markers, inclusive)
// in the given content with the new block.
func replaceExcludeBlock(content, newBlock string) string {
	startIdx := strings.Index(content, excludeMarkerStart)
	endIdx := strings.Index(content, excludeMarkerEnd)

	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return content
	}

	// Include the end marker and trailing newline.
	endIdx += len(excludeMarkerEnd)
	if endIdx < len(content) && content[endIdx] == '\n' {
		endIdx++
	}

	return content[:startIdx] + newBlock + content[endIdx:]
}

// EnsureGlobalHookScripts writes hook scripts to ~/.schmux/hooks/.
// Called once at daemon startup. Returns the hooks directory path.
func EnsureGlobalHookScripts(homeDir string) (string, error) {
	hooksDir := filepath.Join(homeDir, ".schmux", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return "", err
	}
	scripts := map[string][]byte{
		"capture-failure.sh":   captureFailureScript,
		"stop-status-check.sh": stopStatusCheckScript,
		"stop-lore-check.sh":   stopLoreCheckScript,
	}
	for name, content := range scripts {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, content, 0755); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}
	return hooksDir, nil
}
