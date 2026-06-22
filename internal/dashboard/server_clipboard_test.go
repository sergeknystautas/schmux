package dashboard

import (
	"sort"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestServer_ManagedLocalSockets(t *testing.T) {
	server, cfg, st := newTestServer(t)
	// Configured socket defaults to "schmux".
	if got := cfg.GetTmuxSocketName(); got != "schmux" {
		t.Fatalf("setup: configured socket = %q, want schmux", got)
	}

	// restored local session on a stale socket; blank socket → "default";
	// remote session is excluded.
	for _, sess := range []state.Session{
		{ID: "local-old", TmuxSocket: "old-socket"},
		{ID: "local-blank", TmuxSocket: ""},
		{ID: "remote", TmuxSocket: "irrelevant", RemoteHostID: "host-1"},
	} {
		if err := st.AddSession(sess); err != nil {
			t.Fatalf("AddSession: %v", err)
		}
	}

	got := server.managedLocalSockets()
	sort.Strings(got)
	want := []string{"default", "old-socket", "schmux"}
	if len(got) != len(want) {
		t.Fatalf("managedLocalSockets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("managedLocalSockets = %v, want %v", got, want)
			break
		}
	}
}
