package dashboard

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

// TestConfigUpdateRepoNameSanitize covers repo names posted to POST /api/config:
// a name that is new or renamed gets its dots replaced with dashes, while a repo
// re-sent with the name already stored for its URL is stored exactly as sent.
func TestConfigUpdateRepoNameSanitize(t *testing.T) {
	tests := []struct {
		name       string
		seeded     []config.Repo
		posted     []contracts.Repo
		wantStatus int
		wantBody   string
		wantRepos  []config.Repo
	}{
		{
			name: "new sapling dotted name",
			posted: []contracts.Repo{
				{Name: "my.repo", URL: "https://x/my.repo", VCS: "sapling"},
			},
			wantStatus: http.StatusOK,
			wantRepos: []config.Repo{
				{Name: "my-repo", URL: "https://x/my.repo", BarePath: "my-repo", VCS: "sapling"},
			},
		},
		{
			name: "existing dotted name unchanged",
			seeded: []config.Repo{
				{Name: "old.name", URL: "https://x/old.name.git", BarePath: "old.name.git"},
			},
			posted: []contracts.Repo{
				{Name: "old.name", URL: "https://x/old.name.git"},
			},
			wantStatus: http.StatusOK,
			wantRepos: []config.Repo{
				{Name: "old.name", URL: "https://x/old.name.git", BarePath: "old.name.git"},
			},
		},
		{
			name: "rename to dotted name",
			seeded: []config.Repo{
				{Name: "keep", URL: "https://x/keep.git", BarePath: "keep.git"},
			},
			posted: []contracts.Repo{
				{Name: "new.name", URL: "https://x/keep.git"},
			},
			wantStatus: http.StatusOK,
			wantRepos: []config.Repo{
				{Name: "new-name", URL: "https://x/keep.git", BarePath: "keep.git"},
			},
		},
		{
			name: "sanitized name collides",
			seeded: []config.Repo{
				{Name: "a-b", URL: "https://x/a-b.git", BarePath: "a-b.git"},
			},
			posted: []contracts.Repo{
				{Name: "a-b", URL: "https://x/a-b.git"},
				{Name: "a.b", URL: "https://y/a.b", VCS: "sapling"},
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   "duplicate repo name",
			wantRepos: []config.Repo{
				{Name: "a-b", URL: "https://x/a-b.git", BarePath: "a-b.git"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, cfg, _ := newTestServer(t)
			h := newTestConfigHandlers(server)

			if len(tt.seeded) > 0 {
				cfg.Repos = tt.seeded
				if err := cfg.Save(); err != nil {
					t.Fatalf("failed to seed config: %v", err)
				}
			}

			rr := postConfig(t, h, contracts.ConfigUpdateRequest{Repos: tt.posted})

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.wantBody != "" && !strings.Contains(rr.Body.String(), tt.wantBody) {
				t.Errorf("response body %q does not contain %q", rr.Body.String(), tt.wantBody)
			}
			if got := cfg.GetRepos(); !reflect.DeepEqual(got, tt.wantRepos) {
				t.Errorf("stored repos = %+v, want %+v", got, tt.wantRepos)
			}
		})
	}
}
