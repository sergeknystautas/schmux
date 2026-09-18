package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/authcheck"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestCodexAuthRecoveryScope(t *testing.T) {
	s := newAuthCheckServer(t)
	s.SetModelManager(models.New(&config.Config{}, []detect.Tool{{Name: "codex", Command: "codex"}}, "", discardLogger()))
	s.models.SetRegistryModels([]detect.Model{{
		ID: "provider", Runners: map[string]detect.RunnerSpec{
			"codex": {ModelValue: "glm-5.3", Endpoint: "https://gateway.example"},
		},
	}})
	if !s.models.RoutesToEndpoint("provider") {
		t.Fatal("fixture must resolve provider to its external endpoint")
	}
	for _, sess := range []state.Session{
		{ID: "codex", WorkspaceID: "ws-1", Target: "codex", Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex},
		{ID: "provider", WorkspaceID: "ws-1", Target: "provider", Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex},
	} {
		if err := s.state.AddSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	// The startup account rejection and turn errors use this same sink.
	// No login tool is needed to confirm an explicit rejection.
	t.Setenv("PATH", t.TempDir())
	for _, id := range []string{"codex", "provider"} {
		s.HandleChatTurnError(id, chat.TurnErrorEvent{Protocol: chat.ProtocolCodex, Text: "Codex is not logged in"})
		got, _ := s.state.GetSession(id)
		if got.SignedOut != (id == "codex") {
			t.Fatalf("%s signed_out=%v", id, got.SignedOut)
		}
	}
	// A completed status-tool check uses the same provider boundary.
	s.applyAuthAnswer(chat.ProtocolCodex, authcheck.LoggedOut)
	provider, _ := s.state.GetSession("provider")
	if provider.SignedOut {
		t.Fatal("global login check signed out provider session")
	}
	s.applyAuthAnswer(chat.ProtocolCodex, authcheck.LoggedIn)
	firstParty, _ := s.state.GetSession("codex")
	if firstParty.SignedOut {
		t.Fatal("successful login check did not clear first-party recovery")
	}
}
