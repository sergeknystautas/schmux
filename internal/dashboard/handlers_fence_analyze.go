package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/fence"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	// fenceAnalyzeNickname is the session nickname for spawned fence-analysis agents.
	fenceAnalyzeNickname = "fence-analyze"

	fenceAnalysisTerminalFilename = "terminal-capture.txt"
	fenceAnalysisCaptureTimeout   = 5 * time.Second
)

type fenceAnalysisInputs struct {
	capabilitiesPath string
	eventsPath       string
	commandPath      string
	terminalPath     string
	settingsPath     string
	monitorLogPath   string
	repoConfigPath   string
}

// fenceCommandOrError resolves the fence binary for a fenced spawn, applying the
// same availability and mode backstop as the spawn endpoint. Shared with
// handleSpawnPost so the fence guards are not duplicated. Returns ok=false with
// an error message and HTTP status when fencing cannot be honored.
func (h *SpawnHandlers) fenceCommandOrError() (fenceCommand, errMsg string, status int) {
	if h.dependencyReport != nil {
		if st, ok := h.dependencyReport().Status("fence"); ok && st.Detected {
			return st.Command, "", 0
		}
	}
	return "", "fence not available — install fence to use fenced sessions", http.StatusBadRequest
}

// fenceModeOrError enforces the configured fence mode for a fenced spawn. Shared
// guard so handleFenceAnalyze and handleSpawnPost apply it identically.
func (h *SpawnHandlers) fenceModeOrError() (errMsg string, status int) {
	if h.config.GetFenceMode() == config.FenceModeDisabled {
		return "fenced sessions are disabled", http.StatusBadRequest
	}
	return "", 0
}

// handleFenceAnalyze spawns a fenced analysis agent with the target session's
// intent, terminal output, launch command, effective policy, events, and monitor
// log. The browser sends only a session id; the backend snapshots and names the
// evidence so the analysis does not mistake monitor.log for a complete account
// of what the session attempted or what failed.
func (h *SpawnHandlers) handleFenceAnalyze(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	// Validate the target session exists and is fenced (404, like the fence-log
	// handler). The launch dir is reconstructed from the session record, so this
	// check also gates path construction.
	sess, ok := h.state.GetSession(sessionID)
	if !ok || !sess.Fence {
		writeJSONError(w, "unknown fenced session", http.StatusNotFound)
		return
	}

	// fence_analyze must be enabled with a configured target (400).
	if !h.config.GetFenceAnalyzeEnabled() {
		writeJSONError(w, "fence analysis is disabled", http.StatusBadRequest)
		return
	}
	target := h.config.GetFenceAnalyzeTarget()
	if target == "" {
		writeJSONError(w, "no analysis target configured", http.StatusBadRequest)
		return
	}

	// Resolve the workspace the analyzer runs in — the same workspace as the
	// target session, so it sees the same repo, branch, and fence dir.
	ws, ok := h.state.GetWorkspace(sess.WorkspaceID)
	if !ok {
		writeJSONError(w, "workspace for fenced session not found", http.StatusNotFound)
		return
	}

	// Fence availability/mode backstop, reused from the spawn path.
	fenceCommand, errMsg, status := h.fenceCommandOrError()
	if errMsg != "" {
		writeJSONError(w, errMsg, status)
		return
	}
	if modeMsg, modeStatus := h.fenceModeOrError(); modeMsg != "" {
		writeJSONError(w, modeMsg, modeStatus)
		return
	}

	// Generate the capability vocabulary from the running binary, then snapshot
	// the source session's full terminal scrollback. Both live under the
	// workspace fence dir, which the analyzer can read but cannot write.
	wsFenceDir := schmuxdir.FenceWorkspaceDir(sess.WorkspaceID)
	capDocPath := filepath.Join(wsFenceDir, "fence-capabilities.md")
	if err := os.MkdirAll(wsFenceDir, 0o700); err != nil {
		writeJSONError(w, "failed to prepare fence analysis doc: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(capDocPath, []byte(fence.RenderCapabilities()), 0o600); err != nil {
		writeJSONError(w, "failed to write fence analysis doc: "+err.Error(), http.StatusInternalServerError)
		return
	}

	targetLaunchDir := schmuxdir.FenceLaunchDir(sess.WorkspaceID, sess.ID)
	if err := os.MkdirAll(targetLaunchDir, 0o700); err != nil {
		writeJSONError(w, "failed to prepare fence analysis inputs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	terminalPath := filepath.Join(targetLaunchDir, fenceAnalysisTerminalFilename)
	if err := h.captureFenceAnalysisTerminal(r.Context(), sess.ID, terminalPath); err != nil {
		writeJSONError(w, "failed to write fence analysis terminal capture: "+err.Error(), http.StatusInternalServerError)
		return
	}

	inputs := fenceAnalysisInputs{
		capabilitiesPath: capDocPath,
		eventsPath:       filepath.Join(state.SchmuxDataDir(ws.Path), "events", sess.ID+".jsonl"),
		commandPath:      filepath.Join(targetLaunchDir, "cmd.sh"),
		terminalPath:     terminalPath,
		settingsPath:     filepath.Join(targetLaunchDir, "settings.json"),
		monitorLogPath:   filepath.Join(targetLaunchDir, "monitor.log"),
		repoConfigPath:   filepath.Join(ws.Path, ".schmux", "config.json"),
	}
	newSess, err := h.session.Spawn(r.Context(), session.SpawnOptions{
		WorkspaceID:  ws.ID,
		TargetName:   target,
		Prompt:       buildFenceAnalyzePrompt(sess.ID, inputs),
		Nickname:     fenceAnalyzeNickname,
		Fence:        true,
		FenceCommand: fenceCommand,
	})
	if err != nil {
		writeJSONError(w, "failed to spawn fence analysis agent: "+err.Error(), http.StatusInternalServerError)
		return
	}

	go h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(SessionResult{
		SessionID:   newSess.ID,
		WorkspaceID: newSess.WorkspaceID,
		Target:      target,
		Nickname:    newSess.Nickname,
	}); err != nil {
		h.logger.Error("failed to encode fence-analyze response", "err", err)
	}
}

func (h *SpawnHandlers) captureFenceAnalysisTerminal(ctx context.Context, sessionID, path string) error {
	captureCtx, cancel := context.WithTimeout(ctx, fenceAnalysisCaptureTimeout)
	output, err := h.session.GetPlainOutput(captureCtx, sessionID)
	cancel()
	if err != nil {
		h.logger.Warn("fence analysis terminal capture unavailable", "session", sessionID, "err", err)
		output = fmt.Sprintf("Terminal capture unavailable: %v\n", err)
	} else if output == "" {
		output = "Terminal capture was empty.\n"
	}
	return os.WriteFile(path, []byte(output), 0o600)
}

// buildFenceAnalyzePrompt identifies the evidence and the decision the analyzer
// must make. The capabilities document owns the current preset vocabulary and
// the product-extension rules; this prompt owns source ordering and the
// in-session delivery contract.
func buildFenceAnalyzePrompt(targetSessionID string, in fenceAnalysisInputs) string {
	return fmt.Sprintf(`Investigate fenced schmux session %s. Determine what it was trying to accomplish, what failed, and whether fence caused or contributed to each failure. Do not start from sandbox denials: a denial that blocked no required operation is noise, not a finding.

Read the evidence in this order:
1. %s — the capabilities and current repo-config vocabulary of the running schmux binary.
2. %s — session events. The initial working event contains the spawn instruction; later events may contain intent, blockers, errors, and reflections.
3. %s — the exact launch command.
4. %s — a plain-text snapshot of the source session's full terminal scrollback. Use it to recover follow-up instructions, attempted commands, tool output, and errors.
5. %s — the effective fence settings for that launch.
6. %s — this repo's current fence config. A missing file means no repo-specific fence config.
7. %s — fence's operation log. This is corroborating evidence, not a complete transcript. Fence-caused failures can be absent from it.

Inspect the workspace itself when source, build configuration, or current git state is needed to understand an attempted operation.

For every blocked goal, establish the causal chain: intended operation; command or tool; observed error or bad result; evidence that fence did or did not cause it; and the next action. Quote the relevant terminal error and, when one exists, the relevant monitor line. State uncertainty when the evidence cannot establish causation.

Try the current repo knobs first. If a documented preset or allowed domain fits, give the exact .schmux/config.json change and the command that should be rerun to verify it. If the current knobs cannot express the required access, do not stop at "unsupported": specify the least-privilege change schmux's fence implementation needs, its security cost, and a regression test that proves the failed operation works while unrelated access remains denied. If fence was not the cause, identify the actual failure and a concrete next diagnostic or fix.

When finished, answer in this terminal as the normal final response. Do not create an HTML file or any other report artifact. Include every supported finding and recommendation; do not abbreviate away evidence or product changes. Do not apply changes unless explicitly asked.`,
		targetSessionID,
		in.capabilitiesPath,
		in.eventsPath,
		in.commandPath,
		in.terminalPath,
		in.settingsPath,
		in.repoConfigPath,
		in.monitorLogPath,
	)
}
