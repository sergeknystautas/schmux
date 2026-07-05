package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/style"
)

// handleRestart disposes a session and re-spawns it in the same worktree,
// resuming the captured harness conversation by id, with fence settings
// re-resolved from current config. Local-only; requires a captured resume_id
// and a harness that declares resume_id_args.
func (h *SpawnHandlers) handleRestart(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	sess, ok := h.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "unknown session", http.StatusNotFound)
		return
	}
	if sess.ResumeID == "" {
		writeJSONError(w, "session has no resume id", http.StatusBadRequest)
		return
	}
	if sess.RemoteHostID != "" {
		writeJSONError(w, "restart is local-only", http.StatusBadRequest)
		return
	}
	if sess.Status == "disposing" {
		writeJSONError(w, "session is already disposing", http.StatusConflict)
		return
	}
	// The resolved harness must support by-id resume, else the re-spawn would
	// error in buildCommand — reject up front with a clear message.
	baseTool := h.models.ResolveTargetToTool(sess.Target)
	adapter := detect.GetAdapter(baseTool)
	if adapter == nil || adapter.ResumeIDArgs(nil, sess.ResumeID) == nil {
		writeJSONError(w, "harness does not support resume by id", http.StatusBadRequest)
		return
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

	// Re-resolve fence from current config (this is the "fresh settings" effect).
	fenceCommand := ""
	if sess.Fence {
		var errMsg string
		var status int
		fenceCommand, errMsg, status = h.fenceCommandOrError()
		if errMsg != "" {
			writeJSONError(w, errMsg, status)
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
		TargetName:    sess.Target,
		Nickname:      sess.Nickname,
		PersonaID:     sess.PersonaID,
		PersonaPrompt: agentPrompt,
		StyleID:       sess.StyleID,
		Resume:        true,
		ResumeID:      sess.ResumeID,
		Fence:         sess.Fence,
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
