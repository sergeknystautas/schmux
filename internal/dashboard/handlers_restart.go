package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/style"
)

// handleRestart disposes a session and re-spawns it in the same worktree,
// resuming the captured harness conversation by id, with fence settings
// re-resolved from current config. Local-only; requires a captured resume_id
// and a harness that declares resume_id_args.
//
// An optional JSON body { "target"?: string, "fence"?: boolean } (from the
// shift-click restart modal) overrides the session's target and/or fence. A
// target override must resolve to the same harness (the resume id is
// harness-native). An empty/absent body reproduces the pre-modal behavior: the
// session's current target and fence are reused.
func (h *SpawnHandlers) handleRestart(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	sess, ok := h.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "unknown session", http.StatusNotFound)
		return
	}

	// Optional body. Absent/empty ⇒ nil pointers ⇒ reuse the session's values
	// (identical to the pre-modal behavior). Plain click sends no body.
	var req contracts.RestartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tool, errMsg, status := h.restartEligibility(sess)
	if errMsg != "" {
		writeJSONError(w, errMsg, status)
		return
	}

	// Effective target: an override must resolve to the same harness (the resume
	// id is harness-native).
	targetName := sess.Target
	if req.Target != nil && *req.Target != "" && *req.Target != sess.Target {
		if h.resolveTargetTool(*req.Target) != tool {
			writeJSONError(w, "restart target must use the same harness", http.StatusBadRequest)
			return
		}
		targetName = *req.Target
	}

	// Effective fence.
	effectiveFence := sess.Fence
	if req.Fence != nil {
		effectiveFence = *req.Fence
	}

	// Re-resolve persona/style (verbatim ids) into the composed prompt, exactly
	// like handleSpawnPost. The snapshot carries the session's resolved ids
	// as-is — defaults are not re-resolved (out of scope for fence-restart).
	var personaObj *persona.Persona
	if sess.PersonaID != "" {
		personaObj, _ = h.personaManager.Get(sess.PersonaID)
	}
	var styleObj *style.Style
	if sess.StyleID != "" {
		styleObj, _ = h.styleManager.Get(sess.StyleID)
	}
	agentPrompt := formatAgentSystemPrompt(personaObj, styleObj)

	// Resolve the fence command from current config when the effective fence is
	// on. All validation above happens before Dispose — the session is only torn
	// down once the request is known to be valid.
	fenceCommand := ""
	if effectiveFence {
		var fenceErr string
		var fenceStatus int
		fenceCommand, fenceErr, fenceStatus = h.fenceCommandOrError()
		if fenceErr != "" {
			writeJSONError(w, fenceErr, fenceStatus)
			return
		}
		if modeMsg, modeStatus := h.fenceModeOrError(); modeMsg != "" {
			writeJSONError(w, modeMsg, modeStatus)
			return
		}
	}

	// Dispose the old session, then re-spawn resuming the same conversation.
	if err := h.session.Dispose(r.Context(), sess.ID); err != nil {
		writeJSONError(w, "failed to dispose session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	newSess, err := h.session.Spawn(r.Context(), session.SpawnOptions{
		WorkspaceID:   sess.WorkspaceID,
		TargetName:    targetName,
		Nickname:      sess.Nickname,
		PersonaID:     sess.PersonaID,
		PersonaPrompt: agentPrompt,
		StyleID:       sess.StyleID,
		Resume:        true,
		ResumeID:      sess.ResumeID,
		Fence:         effectiveFence,
		FenceCommand:  fenceCommand,
	})
	if err != nil {
		writeJSONError(w, "failed to restart session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Seed the new session's resume id immediately — we already know it (we just
	// passed it to the resume flags), so Restart stays available without waiting
	// for the hook to re-emit. The recurring hook re-emits the same id
	// idempotently (and overwrites if the harness forks on resume).
	if h.state.UpdateSessionResumeID(newSess.ID, sess.ResumeID) {
		_ = h.state.Save()
		go h.broadcastSessions()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(SessionResult{
		SessionID:   newSess.ID,
		WorkspaceID: newSess.WorkspaceID,
		Target:      newSess.Target,
		Nickname:    newSess.Nickname,
	}); err != nil {
		h.logger.Error("failed to encode restart response", "err", err)
	}
}

// resolveTargetTool returns the harness/tool a target resolves to the way spawn
// resolves it (model → ResolveToolForModel, bare tool → itself), or "" for an
// opaque run-target or unknown name. Unlike models.ResolveTargetToTool
// (FirstRunnerKey) this matches the tool the re-spawn will actually use.
func (h *SpawnHandlers) resolveTargetTool(name string) string {
	if model, ok := h.models.FindModel(name); ok {
		return h.models.ResolveToolForModel(model)
	}
	if detect.IsToolName(name) {
		return name
	}
	return ""
}

// restartEligibility applies the shared restart guards and returns the session's
// resolved harness tool. A non-empty errMsg means reject with the given status.
func (h *SpawnHandlers) restartEligibility(sess state.Session) (tool, errMsg string, status int) {
	if sess.ResumeID == "" {
		return "", "session has no resume id", http.StatusBadRequest
	}
	if sess.RemoteHostID != "" {
		return "", "restart is local-only", http.StatusBadRequest
	}
	if sess.Status == "disposing" {
		return "", "session is already disposing", http.StatusConflict
	}
	tool = h.resolveTargetTool(sess.Target)
	adapter := detect.GetAdapter(tool)
	if adapter == nil || adapter.ResumeIDArgs(nil, sess.ResumeID) == nil {
		return "", "harness does not support resume by id", http.StatusBadRequest
	}
	return tool, "", 0
}

// handleRestartOptions returns the enabled targets that run on the session's
// current harness (so the resume id stays valid), plus the current target, its
// fence state, and whether fence is togglable. Feeds the shift-click restart
// modal.
func (h *SpawnHandlers) handleRestartOptions(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	sess, ok := h.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "unknown session", http.StatusNotFound)
		return
	}
	tool, errMsg, status := h.restartEligibility(sess)
	if errMsg != "" {
		writeJSONError(w, errMsg, status)
		return
	}

	seen := map[string]bool{}
	var targets []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		if h.resolveTargetTool(name) == tool {
			seen[name] = true
			targets = append(targets, name)
		}
	}
	for modelID := range h.models.GetEnabledModels() {
		add(modelID)
	}
	for _, t := range h.models.DetectedToolNames() {
		add(t)
	}
	add(sess.Target)
	sort.Strings(targets)

	// fence_available mirrors SpawnPage: fence binary detected AND mode != disabled.
	_, fenceErr, _ := h.fenceCommandOrError()
	fenceAvailable := fenceErr == "" && h.config.GetFenceMode() != config.FenceModeDisabled

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(contracts.RestartOptionsResponse{
		CurrentTarget:  sess.Target,
		Targets:        targets,
		Fence:          sess.Fence,
		FenceAvailable: fenceAvailable,
	}); err != nil {
		h.logger.Error("failed to encode restart options", "err", err)
	}
}
