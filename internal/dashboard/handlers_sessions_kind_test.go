package dashboard

import (
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestBuildSessionsResponse_ExposesKind(t *testing.T) {
	srv, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{ID: "s-chat", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{ID: "s-term", WorkspaceID: "ws-1", Target: "claude", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	response := srv.sessionHandlers.buildSessionsResponse()
	if len(response) != 1 || len(response[0].Sessions) != 2 {
		t.Fatalf("response: %+v", response)
	}
	byID := map[string]string{}
	for _, s := range response[0].Sessions {
		byID[s.ID] = s.Kind
	}
	if byID["s-chat"] != "chat" {
		t.Fatalf("chat kind not exposed: %v", byID)
	}
	if byID["s-term"] != "" {
		t.Fatalf("terminal session must have empty kind: %v", byID)
	}
}
