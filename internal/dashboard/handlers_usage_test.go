package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleUsageGet(t *testing.T) {
	t.Run("collects allowed quota with panel disabled and bare tool target", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.UI.Panels = nil
		if err := st.AddSession(state.Session{ID: "claude-plan", Target: "claude", Kind: state.SessionKindChat}); err != nil {
			t.Fatal(err)
		}
		server.observeChatUsage("claude-plan", chat.NewHarness([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.27,"resetsAt":1788606600}}}}`)))

		req := httptest.NewRequest(http.MethodGet, "/api/usage", nil)
		rr := httptest.NewRecorder()
		server.handleUsageGet(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp contracts.UsageSnapshotResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Providers) != 1 || resp.Providers[0].Provider != "anthropic" {
			t.Fatalf("providers = %+v", resp.Providers)
		}
		if got := resp.Providers[0]; got.Status != "allowed" || len(got.Windows) != 1 || got.Windows[0].UsedPercent == nil || *got.Windows[0].UsedPercent != 27 || got.UpdatedAt == "" {
			t.Errorf("usage = %+v", got)
		}
	})

	t.Run("attributes codex telemetry through the session target", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.models.SetRegistryModels([]detect.Model{{
			ID: "glm-5.3", Provider: "zai", Runners: map[string]detect.RunnerSpec{"codex": {}},
		}})
		if err := st.AddWorkspace(state.Workspace{ID: "ws-usage", Path: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		err := st.AddSession(state.Session{
			ID: "codex-zai", WorkspaceID: "ws-usage", Target: "glm-5.3",
			Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex,
		})
		if err != nil {
			t.Fatal(err)
		}
		paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-usage", "codex-zai"))
		if err := paths.Ensure(); err != nil {
			t.Fatal(err)
		}
		runtime, err := server.session.GetChatRuntime("codex-zai")
		if err != nil {
			t.Fatal(err)
		}
		_, live, err := runtime.Subscribe()
		if err != nil {
			t.Fatal(err)
		}

		output, err := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		// Synthetic provider payload proves routing; it does not establish
		// that this third-party endpoint currently emits quota telemetry.
		line := `{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"zai-plan","primary":{"usedPercent":17,"windowDurationMins":300,"resetsAt":1788606600}}}}` + "\n"
		if _, err := output.WriteString(line); err != nil {
			t.Fatal(err)
		}
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}

		select {
		case <-live:
		case <-time.After(2 * time.Second):
			t.Fatal("usage record was not delivered")
		}
		providers := server.usageManager.Snapshot()
		if len(providers) != 1 || providers[0].Provider != "zai" {
			t.Fatalf("providers = %+v; want zai", providers)
		}
		if got := providers[0]; got.LimitID != "zai-plan" || len(got.Windows) != 1 || got.Windows[0].UsedPercent == nil || *got.Windows[0].UsedPercent != 17 || got.Windows[0].DurationMinutes != 300 {
			t.Fatalf("zai usage = %+v", got)
		}
	})

	t.Run("rejects non-GET requests", func(t *testing.T) {
		server, _, _ := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/usage", nil)
		rr := httptest.NewRecorder()
		server.handleUsageGet(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rr.Code)
		}
	})
}
