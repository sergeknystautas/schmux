package detect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// codexHookGroup mirrors one matcher group in codex's hooks.json:
// {"matcher"?: ..., "hooks": [{type, command, timeout?, statusMessage?}]}.
// Matcher is RawMessage so user groups round-trip unmodified.
type codexHookGroup struct {
	Matcher json.RawMessage    `json:"matcher,omitempty"`
	Hooks   []codexHookHandler `json:"hooks"`
}

type codexHookHandler struct {
	Type          string `json:"type"`
	Command       string `json:"command"`
	Timeout       *int   `json:"timeout,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

// codexHooksStrategy implements HookStrategy by merging schmux's
// session-id capture hook into codex's global hooks file
// (~/.codex/hooks.json). Unlike jsonSettingsStrategy (claude, per-workspace
// settings), the target is a single harness-global file; the hook itself is
// inert outside schmux because capture-session.sh exits 0 when
// SCHMUX_EVENTS_FILE is unset.
//
// Codex gates hooks behind per-hook trust (a trusted_hash in config.toml keyed
// by "<file>:<event>:<group index>:<hook index>"), so the merge appends
// schmux's group after the user's: their groups keep their indexes and stay
// trusted. Schmux's own group is new, so the first interactive codex session
// after a merge shows codex's one-time "Hooks need review" prompt.
type codexHooksStrategy struct{}

func init() {
	RegisterHookStrategy("global-json-settings-merge", &codexHooksStrategy{})
}

func (s *codexHooksStrategy) SupportsHooks() bool                          { return true }
func (s *codexHooksStrategy) SetupHooks(ctx HookContext) error             { return codexSetupHooks(ctx) }
func (s *codexHooksStrategy) CleanupHooks(_ string) error                  { return nil }
func (s *codexHooksStrategy) WrapRemoteCommand(cmd string) (string, error) { return cmd, nil }

// codexHookScript wraps a hooks-dir script the way the capture command does:
// a missing script is a no-op, never a failing hook.
func codexHookScript(hooksDir, name string) string {
	script := filepath.Join(hooksDir, name)
	return fmt.Sprintf(`[ -f %q ] && %q || true`, script, script)
}

func codexPermissionStatusCommand() string {
	return `[ -n "$SCHMUX_EVENTS_FILE" ] && { MSG=$(jq -r '"\(.tool_name): \(.tool_input.command // .tool_input.path // .tool_input.file_path // "")"' 2>/dev/null | tr -d "\n" | head -c 200); EMSG=$(printf "%s" "$MSG" | jq -Rs .); printf "{\"ts\":\"%s\",\"type\":\"status\",\"state\":\"needs_input\",\"message\":%s}\n" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$EMSG" >> "$SCHMUX_EVENTS_FILE"; } || true`
}

func codexGroup(command, statusMessage string) codexHookGroup {
	return codexHookGroup{Hooks: []codexHookHandler{{Type: "command", Command: command, StatusMessage: statusMessage}}}
}

// codexManagedEvents returns the hook event names schmux owns.
func codexManagedEvents() []string {
	return []string{"SessionStart", "UserPromptSubmit", "PermissionRequest", "Stop", "PostToolUse", "SessionEnd"}
}

// buildCodexHooksMap is Claude's hook map for Codex: the same states from the
// same lifecycle events, the same gate scripts, one capture script adapted to
// Codex's PostToolUse payload.
func buildCodexHooksMap(hooksDir string) map[string][]codexHookGroup {
	capture := codexGroup(codexHookScript(hooksDir, "capture-session.sh"), "schmux: resume id")
	return map[string][]codexHookGroup{
		"SessionStart": {
			codexGroup(statusEventCommand("working", ""), "schmux: signaling"),
			capture,
		},
		// Capture first: it is the group installations already have, and Codex
		// trusts a handler by its index and command hash, so keeping it at its
		// index keeps resume_id capture working before the user accepts the
		// review for the new groups.
		"UserPromptSubmit": {
			capture,
			codexGroup(statusEventWithContextCommand("working", "prompt"), "schmux: signaling"),
		},
		"PermissionRequest": {
			codexGroup(codexPermissionStatusCommand(), "schmux: signaling"),
		},
		"Stop": {
			codexGroup(statusEventCommand("idle", ""), "schmux: signaling"),
			codexGroup(codexHookScript(hooksDir, "stop-status-check.sh"), "schmux: signaling"),
			codexGroup(codexHookScript(hooksDir, "stop-autolearn-check.sh"), "schmux: autolearn check"),
		},
		"PostToolUse": {
			codexGroup(codexHookScript(hooksDir, "capture-failure-codex.sh"), "schmux: autolearn capture"),
		},
		"SessionEnd": {
			codexGroup(statusEventCommand("completed", ""), "schmux: signaling"),
		},
	}
}

func isSchmuxCodexGroup(g codexHookGroup) bool {
	for _, h := range g.Hooks {
		if strings.HasPrefix(h.StatusMessage, "schmux:") {
			return true
		}
	}
	return false
}

func isCodexManagedEvent(event string, managed map[string][]codexHookGroup) bool {
	_, ok := managed[event]
	return ok
}

// codexSetupHooks merges schmux's hook map (buildCodexHooksMap) into the hooks file declared by
// the descriptor (ctx.Hooks.SettingsFile), or $CODEX_HOME/hooks.json when
// CODEX_HOME is set. Preservation is semantic: user events, groups, and
// top-level fields survive as their original bytes; only schmux's own groups
// are rewritten. Malformed input is an error, never a rewrite.
func codexSetupHooks(ctx HookContext) error {
	if ctx.Hooks == nil || ctx.Hooks.SettingsFile == "" {
		return fmt.Errorf("codex hooks: descriptor hooks.settings_file is required")
	}
	path, err := codexHooksPath(ctx.Hooks.SettingsFile)
	if err != nil {
		return err
	}

	var root map[string]json.RawMessage
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if jerr := json.Unmarshal(data, &root); jerr != nil {
			return fmt.Errorf("codex hooks: %s is malformed, leaving it untouched: %w", path, jerr)
		}
	case errors.Is(err, fs.ErrNotExist):
		root = make(map[string]json.RawMessage)
	default:
		return fmt.Errorf("codex hooks: read %s: %w", path, err)
	}

	events := make(map[string]json.RawMessage)
	if hooksRaw, ok := root["hooks"]; ok {
		if jerr := json.Unmarshal(hooksRaw, &events); jerr != nil {
			return fmt.Errorf("codex hooks: %s hooks block is malformed, leaving it untouched: %w", path, jerr)
		}
	}

	managed := buildCodexHooksMap(ctx.HooksDir)
	schmuxGroups := make(map[string][]json.RawMessage, len(managed))
	for event, groups := range managed {
		for _, group := range groups {
			encoded, err := json.Marshal(group)
			if err != nil {
				return err
			}
			schmuxGroups[event] = append(schmuxGroups[event], encoded)
		}
	}

	for event, raw := range events {
		// Groups stay as their original bytes so fields schmux does not model
		// survive; only the classification is typed.
		var rawGroups []json.RawMessage
		if jerr := json.Unmarshal(raw, &rawGroups); jerr != nil {
			return fmt.Errorf("codex hooks: %s event %s is malformed, leaving file untouched: %w", path, event, jerr)
		}
		kept := make([]json.RawMessage, 0, len(rawGroups)+len(schmuxGroups[event]))
		droppedSchmux := false
		for _, rg := range rawGroups {
			var g codexHookGroup
			if jerr := json.Unmarshal(rg, &g); jerr != nil {
				return fmt.Errorf("codex hooks: %s event %s is malformed, leaving file untouched: %w", path, event, jerr)
			}
			if isSchmuxCodexGroup(g) {
				droppedSchmux = true
				continue
			}
			kept = append(kept, rg)
		}
		isManaged := isCodexManagedEvent(event, managed)
		if !isManaged && !droppedSchmux {
			continue // untouched: keep the original bytes verbatim
		}
		if isManaged {
			kept = append(kept, schmuxGroups[event]...)
		}
		if len(kept) == 0 {
			delete(events, event)
			continue
		}
		merged, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		events[event] = json.RawMessage(merged)
	}
	for _, event := range codexManagedEvents() {
		groups := schmuxGroups[event]
		if _, exists := events[event]; !exists {
			merged, err := json.Marshal(groups)
			if err != nil {
				return err
			}
			events[event] = json.RawMessage(merged)
		}
	}

	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return err
	}
	root["hooks"] = json.RawMessage(eventsJSON)
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	return codexWriteAtomic(path, out)
}

// codexWriteAtomic writes via a temp file in the destination directory and
// renames, so a crash never leaves a half-written hooks file behind.
func codexWriteAtomic(path string, out []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("codex hooks: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".hooks.json.schmux-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("codex hooks: rename into %s: %w", path, err)
	}
	return nil
}

// codexHooksPath resolves the hooks file: CODEX_HOME wins (codex homes
// there), otherwise the descriptor's settings_file with ~ expanded.
func codexHooksPath(settingsFile string) (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "hooks.json"), nil
	}
	if !strings.HasPrefix(settingsFile, "~") && !filepath.IsAbs(settingsFile) {
		return "", fmt.Errorf("codex hooks: settings_file %q must be ~-anchored or absolute", settingsFile)
	}
	return expandHome(settingsFile)
}
