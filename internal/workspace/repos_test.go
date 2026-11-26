package workspace

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

// seedRepos adds repo entries to the state store the manager reads. The
// entries live in state now, not in the config.
func seedRepos(t *testing.T, st state.StateStore, repos []state.Repo) {
	t.Helper()
	for _, r := range repos {
		if err := st.AddRepo(r); err != nil {
			t.Fatalf("AddRepo(%q): %v", r.Name, err)
		}
	}
}
