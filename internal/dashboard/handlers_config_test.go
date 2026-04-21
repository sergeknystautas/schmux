package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

func TestIsValidSocketName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"alphanumeric", "mySocket1", true},
		{"with hyphens", "my-socket", true},
		{"with underscores", "my_socket", true},
		{"mixed", "My-Socket_v2", true},
		{"empty", "", false},
		{"spaces", "my socket", false},
		{"dots", "my.socket", false},
		{"slashes", "my/socket", false},
		{"path traversal", "../etc/passwd", false},
		{"semicolon", "sock;rm -rf", false},
		{"unicode", "sock\u00e9t", false},
		{"single char", "a", true},
		{"numbers only", "12345", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidSocketName(tt.input); got != tt.want {
				t.Errorf("isValidSocketName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestReposEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []config.Repo
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []config.Repo{}, []config.Repo{}, true},
		{"same single", []config.Repo{{Name: "r1", URL: "u1"}}, []config.Repo{{Name: "r1", URL: "u1"}}, true},
		{"different length", []config.Repo{{Name: "r1"}}, []config.Repo{}, false},
		{"different name", []config.Repo{{Name: "r1", URL: "u1"}}, []config.Repo{{Name: "r2", URL: "u1"}}, false},
		{"different url", []config.Repo{{Name: "r1", URL: "u1"}}, []config.Repo{{Name: "r1", URL: "u2"}}, false},
		{"order matters", []config.Repo{{Name: "a"}, {Name: "b"}}, []config.Repo{{Name: "b"}, {Name: "a"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reposEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("reposEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSnapshotRestartRelevant_StableUnderRoundtrip pins the architectural
// invariant: snapshotRestartRelevant uses getters, so nil-section /
// empty-field / explicit-default-value all collapse to the same snapshot
// value. The lazy-init mutation that handleConfigUpdate performs when the
// dashboard form posts back getter-defaulted values must NOT change the
// snapshot — otherwise the form's auto-save would spuriously trip the
// restart-needed flag.
func TestSnapshotRestartRelevant_StableUnderRoundtrip(t *testing.T) {
	// Empty config (everything nil/zero) — like a fresh install.
	empty := &config.Config{}
	emptySnap := snapshotRestartRelevant(empty)

	// Same config but with Network and AccessControl explicitly populated to
	// the values the getters synthesize from nil. This is what the backend
	// produces after applying the dashboard form's roundtrip POST.
	populated := &config.Config{ConfigData: config.ConfigData{
		Network: &config.NetworkConfig{
			BindAddress: "127.0.0.1",
			Port:        7337,
			TLS:         &config.TLSConfig{},
		},
		AccessControl: &config.AccessControlConfig{
			Enabled:           false,
			Provider:          config.DefaultAuthProvider,
			SessionTTLMinutes: config.DefaultAuthSessionTTLMinutes,
		},
		TmuxSocketName: "schmux",
	}}
	populatedSnap := snapshotRestartRelevant(populated)

	if emptySnap != populatedSnap {
		t.Errorf("snapshot of empty config != snapshot of populated-with-defaults config\n  empty     = %+v\n  populated = %+v",
			emptySnap, populatedSnap)
	}
}

// TestRestartFlag_NotSetOnFirstSaveWithUnchangedFields pins the FTUE
// invariant from the user's perspective: on a fresh install, an API write
// that posts unchanged network/access_control/tmux values back — exactly
// as the dashboard form does on every save — MUST NOT set NeedsRestart.
//
// If a future change adds a getter that participates in the restart check
// without snapshotRestartRelevant being updated to include it, this test
// will fail. That is the test's purpose; do not weaken it.
//
// Vendorlocked: under -tags=vendorlocked, GetPublicBaseURL synthesizes a
// value from the port; the form mirrors that value back into the field.
// The snapshot is still stable (same value before and after), so this test
// passes under both build tags. Run with both to verify.
func TestRestartFlag_NotSetOnFirstSaveWithUnchangedFields(t *testing.T) {
	server, cfg, st := newTestServer(t)
	handlers := newTestConfigHandlers(server)

	// 1. Read the API GET response, mirroring what the dashboard does on
	//    page load.
	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	handlers.handleConfigGet(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, body = %s", getRR.Code, getRR.Body.String())
	}
	var apiResp contracts.ConfigResponse
	if err := json.NewDecoder(getRR.Body).Decode(&apiResp); err != nil {
		t.Fatalf("GET decode: %v", err)
	}

	// 2. Build the POST body the dashboard form would build (modeled after
	//    assets/dashboard/src/routes/config/buildConfigUpdate.ts).
	//    Network, access_control, and tmux fields all echo the GET response —
	//    the user has not touched them. The only "user change" is the
	//    non-restart field recycle_workspaces.
	bindAddr := apiResp.Network.BindAddress
	pubURL := apiResp.Network.PublicBaseURL
	// On a fresh install apiResp.Network.TLS is nil because buildTLS returns
	// nil when both paths are empty. The form posts tls: {cert_path: "",
	// key_path: ""} regardless, so mirror that.
	var tlsCert, tlsKey string
	if apiResp.Network.TLS != nil {
		tlsCert = apiResp.Network.TLS.CertPath
		tlsKey = apiResp.Network.TLS.KeyPath
	}
	recycle := true
	tmuxBin := apiResp.TmuxBinary
	tmuxSock := apiResp.TmuxSocketName
	body := contracts.ConfigUpdateRequest{
		RecycleWorkspaces: &recycle,
		Network: &contracts.NetworkUpdate{
			BindAddress:   &bindAddr,
			PublicBaseURL: &pubURL,
			TLS: &contracts.TLSUpdate{
				CertPath: &tlsCert,
				KeyPath:  &tlsKey,
			},
		},
		AccessControl: &contracts.AccessControlUpdate{
			Enabled:           &apiResp.AccessControl.Enabled,
			Provider:          &apiResp.AccessControl.Provider,
			SessionTTLMinutes: &apiResp.AccessControl.SessionTTLMinutes,
		},
		TmuxBinary:     &tmuxBin,
		TmuxSocketName: &tmuxSock,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Snapshot the runtime-effective config BEFORE the POST. After the POST,
	// even if the lazy-init populates fields, the snapshot must be unchanged
	// (because the snapshot reads via getters that handle nil/empty/explicit
	// uniformly).
	preSnap := snapshotRestartRelevant(cfg)

	// 3. POST.
	postReq := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(bodyBytes))
	postRR := httptest.NewRecorder()
	handlers.handleConfigUpdate(postRR, postReq)
	if postRR.Code != http.StatusOK {
		t.Fatalf("POST: status = %d, body = %s", postRR.Code, postRR.Body.String())
	}

	// 4. Sanity: the POST actually applied something. Catches a silent
	//    pass where the handler rejected the body.
	if !cfg.RecycleWorkspaces {
		t.Fatalf("POST did not apply RecycleWorkspaces; test setup wrong")
	}

	// 5. Snapshot equality is the load-bearing invariant — if this fails,
	//    the failure message tells future-you exactly which field drifted.
	postSnap := snapshotRestartRelevant(cfg)
	if preSnap != postSnap {
		t.Errorf("restart-relevant snapshot mutated by roundtrip:\n  pre  = %+v\n  post = %+v",
			preSnap, postSnap)
	}

	// 6. The user-visible symptom: the restart banner must NOT fire.
	if st.GetNeedsRestart() {
		t.Errorf("NeedsRestart was set to true after roundtrip-only save; " +
			"expected false. Indicates a getter/snapshot drift.")
	}
}
