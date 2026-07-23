package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/fence"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
)

// fenceAnalyzeNickname is the session nickname for spawned fence-analysis agents.
const fenceAnalyzeNickname = "fence-analyze"

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

// handleFenceAnalyze spawns a fenced analysis agent that reads the target
// session's monitor.log to diagnose what the fenced session could not accomplish
// and, where the fence caused it, recommends .schmux/config.json changes. The
// prompt is built server-side from schmuxdir-derived absolute paths (correct
// under --config-dir / SCHMUX_HOME by construction); the browser sends only a
// session id. The prompt names no presets — the capabilities doc, which the
// agent is told to read first, owns the closed vocabulary and selection rules,
// so undefined tokens are never handed to the agent.
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

	// The capabilities doc is generated here, by and for the analysis agent —
	// fenced spawns have nothing to do with it. It goes in the workspace fence
	// dir (covered by the analyzer's read grant) and is rewritten on every
	// analyze, so it always matches the running binary.
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

	monitorLogPath := filepath.Join(schmuxdir.FenceLaunchDir(sess.WorkspaceID, sess.ID), "monitor.log")
	newSess, err := h.session.Spawn(r.Context(), session.SpawnOptions{
		WorkspaceID:  ws.ID,
		TargetName:   target,
		Prompt:       buildFenceAnalyzePrompt(capDocPath, monitorLogPath),
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

// buildFenceAnalyzePrompt builds the fence-analysis agent's prompt from the
// absolute paths of the freshly generated capabilities doc and the target
// session's monitor log. Its objective is diagnosis, not denial-cataloging: the
// agent must trace what the fenced session could not accomplish and decide
// whether the fence caused it, because sandbox denials are implementation
// details and most block nothing the session needed. It deliberately names no
// presets: the capabilities doc (read first) owns the closed vocabulary and
// selection rules, so the agent never receives undefined tokens to confabulate
// meanings for.
func buildFenceAnalyzePrompt(capDocPath, monitorLogPath string) string {
	return "A schmux session ran inside the fence sandbox. Diagnose what that agent was unable to " +
		"accomplish and determine whether the fence caused it. A sandbox denial is an implementation " +
		"detail, not a failure: most denials block nothing the session needed, and a denial that stopped " +
		"no operation is not a finding. Do not catalog denials.\n\n" +
		"1. Read " + capDocPath + " first. It explains the sandbox, how to read the log, the two-knob " +
		"vocabulary of changes you may recommend, and the selection and judgment rules for which denials " +
		"are honest to act on. Follow those rules.\n" +
		"2. Read " + monitorLogPath + " — the fence's per-operation log for that session (allowed and " +
		"denied lines).\n" +
		"3. Read this repo's .schmux/config.json to see what is already allowed.\n\n" +
		"For each thing the agent could not do, walk the causal chain in this order:\n" +
		"  1. The operation the agent intended.\n" +
		"  2. The command or tool invocation that failed.\n" +
		"  3. The user-visible error or nonzero result it produced.\n" +
		"  4. The specific fence denial that caused it — quote the monitor.log line.\n" +
		"  5. The supported config change that resolves it, or, if neither knob can express the fix, the " +
		"concrete fence capability that is missing.\n\n" +
		"Reason at the level of commands and their blocked outcomes, not syscall-level sandbox internals. " +
		"Put needs the two knobs cannot express in a Gaps section, reported rather than worked around.\n\n" +
		"Deliver the full analysis directly in this session as your reply. Do not create any file, HTML " +
		"report, or other artifact — but be as thorough here as you would be in a written report: work " +
		"every failure through the chain above and finish with the specific, actionable config changes " +
		"(or the concrete missing capability). A vague or hedged conclusion is a failed analysis. Do not " +
		"apply changes unless explicitly asked."
}
