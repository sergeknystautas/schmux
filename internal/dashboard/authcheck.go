package dashboard

import (
	"context"

	"github.com/sergeknystautas/schmux/internal/authcheck"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

// chatSessionInScope reports whether a session participates in signed-out
// recovery: local chat session whose target does not route the harness to
// a non-first-party endpoint. Unresolvable targets are in scope (fail
// toward showing recovery).
func (s *Server) chatSessionInScope(sess state.Session) bool {
	return sess.IsChat() &&
		sess.RemoteHostID == "" &&
		!s.models.RoutesToEndpoint(sess.Target)
}

// applyAuthAnswer sets or clears signed_out on every in-scope chat session
// of the protocol, saves once, and broadcasts once.
func (s *Server) applyAuthAnswer(protocol string, res authcheck.Result) {
	want := res == authcheck.LoggedOut
	changedAny := false
	for _, sess := range s.state.GetSessions() {
		if sess.EffectiveChatProtocol() != protocol || !s.chatSessionInScope(sess) {
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
// to every in-scope chat session of that protocol. At most one check per
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

// HandleChatTurnError is the in-process sink for live chat turn errors.
// A matching sign-out statement sets the flag on the failing session when
// it is in scope; every turn error also triggers the protocol check (the
// error is a trigger, never a decider — this is the retraction path).
func (s *Server) HandleChatTurnError(sessionID string, ev chat.TurnErrorEvent) {
	if chat.MatchSignOutStatement(ev.Protocol, ev.Text) {
		if sess, ok := s.state.GetSession(sessionID); ok && s.chatSessionInScope(sess) && !sess.SignedOut {
			if s.state.UpdateSessionFunc(sessionID, func(p *state.Session) { p.SignedOut = true }) {
				if err := s.state.Save(); err != nil {
					logging.Sub(s.logger, "authcheck").Error("failed to save state", "session", sessionID, "err", err)
				} else {
					go s.BroadcastSessions()
				}
			}
		}
	}
	go s.RunAuthCheck(ev.Protocol)
}
