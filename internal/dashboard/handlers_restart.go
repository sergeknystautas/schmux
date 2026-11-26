package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/spawnlaunch"
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
// target override must resolve to the session's harness (the resume id is
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

	// Effective target.
	targetName := sess.Target
	if req.Target != nil && *req.Target != "" {
		targetName = *req.Target
	}
	if errMsg, status := h.restartEligibility(sess, targetName); errMsg != "" {
		writeJSONError(w, errMsg, status)
		return
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
	agentPrompt := spawnlaunch.FormatAgentSystemPrompt(personaObj, styleObj)

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

	workspace, ok := h.state.GetWorkspace(sess.WorkspaceID)
	if !ok {
		writeJSONError(w, "workspace not found", http.StatusInternalServerError)
		return
	}
	// Stop the old process before reading its harness history so the preparation
	// snapshot cannot race with a final output append.
	if err := h.session.Dispose(r.Context(), sess.ID); err != nil {
		writeJSONError(w, "failed to dispose session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resumeID, err := h.session.PrepareRestartResume(r.Context(), targetName, sess.ResumeID, workspace.Path)
	if err != nil {
		writeJSONError(w, "failed to prepare restart history: "+err.Error(), http.StatusInternalServerError)
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
		ResumeID:      resumeID,
		Fence:         effectiveFence,
		FenceCommand:  fenceCommand,
		Kind:          sess.Kind,
		ChatSeedFrom:  sess.ID,
	})
	if err != nil {
		writeJSONError(w, "failed to restart session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// A flight that assigned the old session follows it to the replacement so
	// monitoring and pilot delivery continue on the seeded conversation.
	if h.flightSessionRestarted != nil {
		if err := h.flightSessionRestarted(sess.ID, newSess.ID); err != nil {
			h.logger.Error("flight assignment did not follow restart", "old", sess.ID, "new", newSess.ID, "err", err)
		}
	}

	// Seed the new session's resume id immediately — we already know it (we just
	// passed it to the resume flags), so Restart stays available without waiting
	// for the hook to re-emit. The recurring hook re-emits the same id
	// idempotently (and overwrites if the harness forks on resume).
	if h.state.UpdateSessionResumeID(newSess.ID, resumeID) {
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
	return resolveTargetTool(h.models, name)
}

// resolveTargetTool maps a target name to its harness tool: a catalog model
// resolves through the model manager; a bare tool name resolves to itself.
func resolveTargetTool(m *models.Manager, name string) string {
	if model, ok := m.FindModel(name); ok {
		return m.ResolveToolForModel(model)
	}
	if detect.IsToolName(name) {
		return name
	}
	return ""
}

// chatHarnessNames maps chat protocol to the harness named in restart errors.
var chatHarnessNames = map[string]string{
	chat.ProtocolClaude: "claude",
	chat.ProtocolCodex:  "codex",
}

// restartEligibility applies the restart guards to restarting sess onto
// targetName. A non-empty errMsg means reject with the given status.
func (h *SpawnHandlers) restartEligibility(sess state.Session, targetName string) (errMsg string, status int) {
	if sess.ResumeID == "" {
		return "session has no resume id", http.StatusBadRequest
	}
	if sess.Status == "disposing" {
		return "session is already disposing", http.StatusConflict
	}
	tool := h.resolveTargetTool(targetName)
	// A chat's protocol is fixed at spawn, so when its target has since moved to
	// another protocol, the protocol check below identifies its harness instead.
	current := h.resolveTargetTool(sess.Target)
	moved := false
	if sess.IsChat() {
		adapter := detect.GetAdapter(current)
		moved = adapter == nil || adapter.ChatProtocol() != sess.EffectiveChatProtocol()
	}
	if tool != current && !moved {
		return "restart target must use the same harness", http.StatusBadRequest
	}
	adapter := detect.GetAdapter(tool)
	if adapter == nil {
		return "harness does not support resume by id", http.StatusBadRequest
	}
	if sess.IsChat() {
		if adapter.ChatArgs(nil, false, sess.ResumeID) == nil {
			return "harness does not support chat resume", http.StatusBadRequest
		}
		if adapter.ChatProtocol() != sess.EffectiveChatProtocol() {
			if targetName != sess.Target {
				return "restart target must use the same harness", http.StatusBadRequest
			}
			name := sess.Target
			if model, ok := h.models.FindModel(sess.Target); ok {
				name = model.DisplayName
			}
			return fmt.Sprintf("%s is running with %s. This is no longer configured, so you cannot restart.",
				name, chatHarnessNames[sess.EffectiveChatProtocol()]), http.StatusBadRequest
		}
	} else if adapter.ResumeIDArgs(nil, sess.ResumeID) == nil {
		return "harness does not support resume by id", http.StatusBadRequest
	}
	return "", 0
}

// handleRestartOptions returns the enabled targets the session can be restarted
// onto (so the resume id stays valid), plus the current target, its fence state,
// and whether fence is togglable. The current target is listed only when it is
// itself allowed. When nothing is allowed, the response is the current target's
// rejection. Feeds the shift-click restart modal.
func (h *SpawnHandlers) handleRestartOptions(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	sess, ok := h.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "unknown session", http.StatusNotFound)
		return
	}

	seen := map[string]bool{}
	var targets []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		if errMsg, _ := h.restartEligibility(sess, name); errMsg == "" {
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
	if len(targets) == 0 {
		errMsg, status := h.restartEligibility(sess, sess.Target)
		writeJSONError(w, errMsg, status)
		return
	}
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
