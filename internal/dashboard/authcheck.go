package dashboard

import (
	"context"
	"net/http"

	"github.com/sergeknystautas/schmux/internal/authcheck"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

// chatSessionInScope reports whether a chat session participates in
// signed-out recovery: local chat session whose target does not route the
// harness to a non-first-party endpoint. Unresolvable targets are in scope
// (fail toward showing recovery). Kept narrow: only chat sessions, never
// sign-in helpers — HandleChatTurnError is chat-only.
func (s *Server) chatSessionInScope(sess state.Session) bool {
	return sess.IsChat() &&
		sess.RemoteHostID == "" &&
		!s.models.RoutesToEndpoint(sess.Target)
}

// authParticipantProtocol reports the protocol a session participates in
// for shared auth state, and whether the session is a participant at all.
// A local first-party chat session participates under its
// EffectiveChatProtocol(); a local terminal session with a non-empty
// SignInProtocol participates under that explicit protocol. Ordinary
// terminals, remote sessions, provider-routed chats, and sessions with
// empty protocol helpers do not participate.
func (s *Server) authParticipantProtocol(sess state.Session) (string, bool) {
	if sess.RemoteHostID != "" {
		return "", false
	}
	if sess.IsChat() {
		if s.models.RoutesToEndpoint(sess.Target) {
			return "", false
		}
		return sess.EffectiveChatProtocol(), true
	}
	if sess.SignInProtocol != "" {
		return sess.SignInProtocol, true
	}
	return "", false
}

// applyAuthAnswer sets or clears signed_out on every in-scope auth
// participant of the protocol, saves once, and broadcasts once. Chat
// sessions and local sign-in helpers are both participants under the
// same protocol-wide rule.
func (s *Server) applyAuthAnswer(protocol string, res authcheck.Result) {
	want := res == authcheck.LoggedOut
	changedAny := false
	for _, sess := range s.state.GetSessions() {
		proto, ok := s.authParticipantProtocol(sess)
		if !ok || proto != protocol {
			continue
		}
		if sess.SignedOut == want {
			continue
		}
		updated := s.state.UpdateSessionFunc(sess.ID, func(p *state.Session) {
			p.SignedOut = want
		})
		if updated {
			changedAny = true
		}
	}
	if !changedAny {
		return
	}
	if err := s.state.Save(); err != nil {
		logging.Sub(s.logger, "authcheck").Error("failed to save state", "err", err)
		return
	}
	go s.BroadcastSessions()
}

// RunAuthCheck runs the protocol's status tool once and applies the answer
// to every in-scope auth participant of that protocol. At most one check per
// protocol is in flight; a trigger arriving during a run is covered by it.
func (s *Server) RunAuthCheck(protocol string) {
	s.authMu.Lock()
	if s.authInFlight == nil {
		s.authInFlight = map[string]bool{}
	}
	if s.authInFlight[protocol] {
		s.authMu.Unlock()
		return
	}
	s.authInFlight[protocol] = true
	s.authMu.Unlock()
	defer func() {
		s.authMu.Lock()
		delete(s.authInFlight, protocol)
		s.authMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), authcheck.Timeout)
	defer cancel()
	res, raw := authcheck.Run(ctx, protocol)
	if res == authcheck.NoAnswer {
		logging.Sub(s.logger, "authcheck").Warn("status tool gave no answer", "protocol", protocol, "output", raw)
		return
	}
	s.applyAuthAnswer(protocol, res)
}

// HandleChatTurnError receives live turn errors and startup auth rejections.
// A first-party Claude 401 invalidates Claude's cached credential and signs
// out every in-scope Claude chat. Other matching statements set the flag on
// the failing session; other errors trigger the protocol status check.
func (s *Server) HandleChatTurnError(sessionID string, ev chat.TurnErrorEvent) {
	sess, inScope := s.state.GetSession(sessionID)
	inScope = inScope && s.chatSessionInScope(sess)
	if !inScope {
		return
	}

	// Claude's status command only checks whether a credential is cached. When
	// Anthropic has rejected that credential, clear Claude's cache first so
	// future status checks cannot overwrite signed_out with a stale answer.
	if ev.Protocol == chat.ProtocolClaude && ev.APIErrorStatus == http.StatusUnauthorized {
		ctx, cancel := context.WithTimeout(context.Background(), authcheck.Timeout)
		raw, err := authcheck.InvalidateClaude(ctx)
		cancel()
		if err != nil {
			logging.Sub(s.logger, "authcheck").Warn("failed to invalidate rejected Claude credential", "output", raw, "err", err)
		}
		s.applyAuthAnswer(ev.Protocol, authcheck.LoggedOut)
		return
	}

	if chat.MatchSignOutStatement(ev.Protocol, ev.Text) {
		if !sess.SignedOut {
			if s.state.UpdateSessionFunc(sessionID, func(p *state.Session) { p.SignedOut = true }) {
				if err := s.state.Save(); err != nil {
					logging.Sub(s.logger, "authcheck").Error("failed to save state", "session", sessionID, "err", err)
				} else {
					go s.BroadcastSessions()
				}
			}
		}
		// An explicit rejection is authoritative. A cached CLI credential
		// must not immediately clear it; the user can recheck after signing in.
		return
	}
	go s.RunAuthCheck(ev.Protocol)
}
