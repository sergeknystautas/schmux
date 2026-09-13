package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
)

// reauthCommands maps chat protocol to the login-flow command the reauth
// terminal runs in the chat session's workspace (spec, Recovery UX).
//
// Claude signs in through the REPL's /login dialog, not the standalone
// `claude auth login` subcommand: as of Claude Code 2.1.270 the standalone
// prompt reads the pasted code without echoing any of it and exits on a bad
// code, so in the dashboard it looks like a terminal that ignores
// keystrokes and then dies. The REPL dialog echoes the code and stays up.
var reauthCommands = map[string]string{
	chat.ProtocolClaude: "claude auth logout || true; claude /login",
	chat.ProtocolCodex:  "codex login",
}

// authGuard applies the shared guards: 404 unknown, 400 non-chat,
// 409 remote. Returns the session when eligible.
func (h *SpawnHandlers) authGuard(w http.ResponseWriter, r *http.Request) (state.Session, bool) {
	sessionID := chi.URLParam(r, "sessionID")
	sess, ok := h.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "unknown session", http.StatusNotFound)
		return state.Session{}, false
	}
	if !sess.IsChat() {
		writeJSONError(w, "not a chat session", http.StatusBadRequest)
		return state.Session{}, false
	}
	if sess.RemoteHostID != "" {
		writeJSONError(w, "remote chat sessions authenticate on their host", http.StatusConflict)
		return state.Session{}, false
	}
	return sess, true
}

// handleReauth spawns a terminal session in the chat session's workspace
// running the harness's real login flow, and returns it — the exact
// response shape and encode pattern of the restart handler.
func (h *SpawnHandlers) handleReauth(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.authGuard(w, r)
	if !ok {
		return
	}
	protocol := sess.EffectiveChatProtocol()
	cmd, found := reauthCommands[protocol]
	if !found {
		writeJSONError(w, "no login flow for chat protocol "+protocol, http.StatusBadRequest)
		return
	}
	// SpawnCommand, not Spawn: the login flow is a raw command in the
	// chat workspace, not a target launch — Spawn resolves a target first
	// and fails with "target not found" for command-only sessions.
	newSess, err := h.session.SpawnCommand(r.Context(), session.SpawnOptions{
		WorkspaceID: sess.WorkspaceID,
		Command:     cmd,
		Nickname:    "sign-in",
	})
	if err != nil {
		writeJSONError(w, "failed to spawn login session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Push the new session to dashboard clients now, as handleSpawnPost does:
	// the page's waitForSession resolves on the next session broadcast, and
	// without this it sits until an unrelated broadcast or its 8s timeout.
	go h.broadcastSessions()
	if err := json.NewEncoder(w).Encode(SessionResult{
		SessionID:   newSess.ID,
		WorkspaceID: newSess.WorkspaceID,
		Nickname:    newSess.Nickname,
	}); err != nil {
		h.logger.Error("failed to encode reauth response", "err", err)
	}
}

// handleAuthCheck runs the login-status check for the session's protocol.
// The response body is unused; state changes arrive via the session
// broadcast.
func (h *SpawnHandlers) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.authGuard(w, r)
	if !ok {
		return
	}
	protocol := sess.EffectiveChatProtocol()
	go func() {
		if h.runAuthCheck != nil {
			h.runAuthCheck(protocol)
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}
