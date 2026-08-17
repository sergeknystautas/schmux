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

// codexCaptureEvents are the hook events the capture registers on. Only
// UserPromptSubmit: codex does not fire SessionStart until the first prompt is
// submitted, so registering there would add a second hook to trust while
// capturing nothing earlier.
var codexCaptureEvents = []string{"UserPromptSubmit"}

func codexCaptureCommand(hooksDir string) string {
	script := filepath.Join(hooksDir, "capture-session.sh")
	return fmt.Sprintf(`[ -f %q ] && %q || true`, script, script)
}

func isSchmuxCodexGroup(g codexHookGroup) bool {
	for _, h := range g.Hooks {
		if strings.HasPrefix(h.StatusMessage, "schmux:") {
			return true
		}
	}
	return false
}

func isCodexManagedEvent(event string) bool {
	for _, m := range codexCaptureEvents {
		if event == m {
			return true
		}
	}
	return false
}

// codexSetupHooks merges the capture hook into the hooks file declared by
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

	schmuxGroup, err := json.Marshal(codexHookGroup{Hooks: []codexHookHandler{{
		Type:          "command",
		Command:       codexCaptureCommand(ctx.HooksDir),
		StatusMessage: "schmux: resume id",
	}}})
	if err != nil {
		return err
	}

	for event, raw := range events {
		// Groups stay as their original bytes so fields schmux does not model
		// survive; only the classification is typed.
		var rawGroups []json.RawMessage
		if jerr := json.Unmarshal(raw, &rawGroups); jerr != nil {
			return fmt.Errorf("codex hooks: %s event %s is malformed, leaving file untouched: %w", path, event, jerr)
		}
		kept := make([]json.RawMessage, 0, len(rawGroups)+1)
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
		managed := isCodexManagedEvent(event)
		if !managed && !droppedSchmux {
			continue // untouched: keep the original bytes verbatim
		}
		if managed {
			kept = append(kept, schmuxGroup)
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
	for _, event := range codexCaptureEvents {
		if _, exists := events[event]; !exists {
			merged, err := json.Marshal([]json.RawMessage{schmuxGroup})
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
