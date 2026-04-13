package detect

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

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

// schmuxStatusMessagePrefix identifies hook handlers managed by schmux.
// Used to distinguish schmux hooks from user-defined hooks during merge.
const schmuxStatusMessagePrefix = "schmux:"

// statusEventCommand returns a shell command that appends a status event to the event file.
// Guarded by env var check so it's a no-op outside schmux-managed sessions.
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
// Note: No single quotes allowed -- the output is embedded in JSON that gets wrapped in single quotes by the shell.
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
	var stopStatusCmd, stopAutolearnCmd, captureFailureCmd string
	if hooksDir != "" {
		stopStatusPath := filepath.Join(hooksDir, "stop-status-check.sh")
		stopAutolearnPath := filepath.Join(hooksDir, "stop-autolearn-check.sh")
		captureFailurePath := filepath.Join(hooksDir, "capture-failure.sh")
		stopStatusCmd = fmt.Sprintf(`[ -f "%s" ] && "%s" || true`, stopStatusPath, stopStatusPath)
		stopAutolearnCmd = fmt.Sprintf(`[ -f "%s" ] && "%s" || true`, stopAutolearnPath, stopAutolearnPath)
		captureFailureCmd = fmt.Sprintf(`[ -f "%s" ] && "%s" || true`, captureFailurePath, captureFailurePath)
	} else {
		stopStatusCmd = `[ -f "$CLAUDE_PROJECT_DIR/.schmux/hooks/stop-status-check.sh" ] && "$CLAUDE_PROJECT_DIR"/.schmux/hooks/stop-status-check.sh || true`
		stopAutolearnCmd = `[ -f "$CLAUDE_PROJECT_DIR/.schmux/hooks/stop-autolearn-check.sh" ] && "$CLAUDE_PROJECT_DIR"/.schmux/hooks/stop-autolearn-check.sh || true`
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
						StatusMessage: "schmux: autolearn capture",
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
	if stopAutolearnCmd != "" {
		hooks["Stop"] = append(hooks["Stop"], claudeHookMatcherGroup{
			Hooks: []claudeHookHandler{
				{
					Type:          "command",
					Command:       stopAutolearnCmd,
					StatusMessage: "schmux: autolearn check",
				},
			},
		})
	}

	return hooks
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

// ClaudeHooksJSON returns the complete .claude/settings.local.json content
// with hooks configuration for schmux signaling, as compact JSON bytes.
func ClaudeHooksJSON(hooksDir string) ([]byte, error) {
	config := map[string]interface{}{
		"hooks": buildClaudeHooksMap(hooksDir),
	}
	return json.Marshal(config)
}

// claudeSetupHooks creates or updates .claude/settings.local.json in the
// workspace with Claude Code hooks for automatic schmux signaling.
// Preserves all non-hooks settings and merges with existing user hooks
// (schmux hooks are identified by statusMessage prefix and replaced in-place).
func claudeSetupHooks(workspacePath, hooksDir string) error {
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
		pkgLogger.Debug("configured Claude hooks", "path", settingsPath)
	}
	return nil
}

// claudeCleanupHooks removes schmux-managed hooks from .claude/settings.local.json.
// Preserves user-defined hooks and other settings. Deletes the file if only
// schmux hooks remain and there are no other settings.
func claudeCleanupHooks(workspacePath string) error {
	settingsPath := filepath.Join(workspacePath, ".claude", "settings.local.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to clean up
		}
		return fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil // malformed, leave it alone
	}

	hooksRaw, hasHooks := settings["hooks"]
	if !hasHooks {
		return nil // no hooks to clean up
	}

	var hooks map[string][]claudeHookMatcherGroup
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		return nil // malformed hooks, leave them alone
	}

	// Filter out schmux matcher groups from each event
	cleanedHooks := make(map[string][]claudeHookMatcherGroup)
	for event, groups := range hooks {
		var kept []claudeHookMatcherGroup
		for _, g := range groups {
			if !isSchmuxMatcherGroup(g) {
				kept = append(kept, g)
			}
		}
		if len(kept) > 0 {
			cleanedHooks[event] = kept
		}
	}

	if len(cleanedHooks) == 0 {
		// No hooks remain after removing schmux hooks
		delete(settings, "hooks")
	} else {
		hooksJSON, err := json.Marshal(cleanedHooks)
		if err != nil {
			return fmt.Errorf("failed to marshal cleaned hooks: %w", err)
		}
		settings["hooks"] = json.RawMessage(hooksJSON)
	}

	// If settings is now empty, delete the file
	if len(settings) == 0 {
		if err := os.Remove(settingsPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove empty settings file: %w", err)
		}
		return nil
	}

	// Write back with indentation
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0644)
}

// claudeWrapRemoteCommand prepends hooks file creation to a command.
// Used for remote sessions where we can't write files via local I/O.
// The hooks file is created in the working directory before the agent starts,
// ensuring hooks are captured at Claude Code startup.
func claudeWrapRemoteCommand(command string) (string, error) {
	jsonBytes, err := ClaudeHooksJSON("")
	if err != nil {
		return command, fmt.Errorf("failed to build hooks JSON: %w", err)
	}
	// JSON uses double quotes only, safe to wrap in single quotes for shell
	return fmt.Sprintf("mkdir -p .claude && printf '%%s\\n' '%s' > .claude/settings.local.json && %s", string(jsonBytes), command), nil
}

//go:embed hooks/capture-failure.sh
var claudeCaptureFailureScript []byte

//go:embed hooks/stop-status-check.sh
var claudeStopStatusCheckScript []byte

//go:embed hooks/stop-autolearn-check.sh
var claudeStopAutolearnCheckScript []byte

// EnsureGlobalHookScripts writes hook scripts to ~/.schmux/hooks/.
// Called once at daemon startup. Returns the hooks directory path.
func EnsureGlobalHookScripts(homeDir string) (string, error) {
	hooksDir := filepath.Join(schmuxdir.Get(), "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return "", err
	}
	scripts := map[string][]byte{
		"capture-failure.sh":      claudeCaptureFailureScript,
		"stop-status-check.sh":    claudeStopStatusCheckScript,
		"stop-autolearn-check.sh": claudeStopAutolearnCheckScript,
	}
	for name, content := range scripts {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, content, 0755); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}
	return hooksDir, nil
}
