package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/chat"
)

func TestChatStorageHandler_NormalResponseMapsCleanerFields(t *testing.T) {
	srv, _, _ := newTestServer(t)
	cleaner := chat.NewStorageCleaner(chat.StorageCleanerOptions{
		Root:           t.TempDir(),
		ActiveSessions: func() map[string]struct{} { return map[string]struct{}{} },
		RetentionDays:  func() int { return 14 },
		MaxBytes:       func() int64 { return 2048 * 1024 * 1024 },
		Now:            func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		Log:            nil,
	})
	cleaner.RunOnce(context.Background())
	srv.SetChatStorageCleaner(cleaner)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat/storage", nil)
	srv.handleChatStorageStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var resp contracts.ChatStorageStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.RetentionDays != 14 {
		t.Errorf("retention_days = %d, want 14", resp.RetentionDays)
	}
	if resp.MaxTotalStorageMB != 2048 {
		t.Errorf("max_total_storage_mb = %d, want 2048", resp.MaxTotalStorageMB)
	}
	if resp.LastCleanup == nil {
		t.Error("last_cleanup should be populated after a pass")
	} else if resp.LastCleanup.AgeDeleted < 0 {
		t.Errorf("age_deleted = %d, want >= 0", resp.LastCleanup.AgeDeleted)
	}
}

func TestChatStorageHandler_OmittedLastCleanupSerializesAsAbsent(t *testing.T) {
	srv, _, _ := newTestServer(t)
	// No cleaner installed: handler returns zero counters and no last_cleanup.

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat/storage", nil)
	srv.handleChatStorageStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	// last_cleanup is omitempty, so the key must be absent (not an empty
	// object) when no cleaner is installed.
	if contains(body, `"last_cleanup"`) {
		t.Errorf("expected last_cleanup key absent when cleaner is nil, got: %s", body)
	}
}

func TestChatStorageHandler_NilCleanerProducesNoCleanupResponse(t *testing.T) {
	srv, _, _ := newTestServer(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat/storage", nil)
	srv.handleChatStorageStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp contracts.ChatStorageStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.LastCleanup != nil {
		t.Errorf("nil cleaner should produce nil last_cleanup, got %+v", resp.LastCleanup)
	}
	if resp.SessionDirectories != 0 || resp.ActiveDirectories != 0 {
		t.Errorf("nil cleaner should produce zero counters, got session=%d active=%d",
			resp.SessionDirectories, resp.ActiveDirectories)
	}
}

func TestChatStorageHandler_ZeroCountersPreserved(t *testing.T) {
	srv, _, _ := newTestServer(t)
	cleaner := chat.NewStorageCleaner(chat.StorageCleanerOptions{
		Root:           t.TempDir(),
		ActiveSessions: func() map[string]struct{} { return map[string]struct{}{} },
		RetentionDays:  func() int { return 7 },
		MaxBytes:       func() int64 { return 4096 * 1024 * 1024 },
		Now:            func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		Log:            nil,
	})
	cleaner.RunOnce(context.Background())
	srv.SetChatStorageCleaner(cleaner)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat/storage", nil)
	srv.handleChatStorageStatus(rr, req)

	body := rr.Body.String()
	// Decode separately so we can inspect the raw body.
	var resp contracts.ChatStorageStatusResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	// 0 must serialize as 0, not be elided.
	for _, key := range []string{
		`"used_bytes":0`,
		`"active_bytes":0`,
		`"ended_bytes":0`,
		`"session_directories":0`,
		`"active_directories":0`,
	} {
		if !contains(body, key) {
			t.Errorf("expected %q in body, got: %s", key, body)
		}
	}
}

func TestChatStorageHandler_RouteReachableThroughRouter(t *testing.T) {
	srv, cfg, _ := newTestServer(t)
	cfg.SetListenPort(0)

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()
	select {
	case addr := <-srv.BoundAddr:
		if addr == nil {
			t.Fatalf("Start failed to bind: %v", <-startErr)
		}
		t.Cleanup(func() {
			_ = srv.Stop()
			<-startErr
		})
		resp, err := http.Get(fmt.Sprintf("http://%s/api/chat/storage", addr))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/chat/storage status = %d, want 200", resp.StatusCode)
		}
	case <-time.After(10 * time.Second): // failure backstop; Start signals BoundAddr on every path
		t.Fatal("Start never signalled BoundAddr")
	}
}

func TestChatStorageHandler_DoesNotRescan(t *testing.T) {
	srv, _, _ := newTestServer(t)
	cleaner := chat.NewStorageCleaner(chat.StorageCleanerOptions{
		Root:           t.TempDir(),
		ActiveSessions: func() map[string]struct{} { return map[string]struct{}{} },
		RetentionDays:  func() int { return 7 },
		MaxBytes:       func() int64 { return 4096 * 1024 * 1024 },
		Now:            func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		Log:            nil,
	})
	cleaner.RunOnce(context.Background())
	srv.SetChatStorageCleaner(cleaner)

	// Snapshot the cached status before the request.
	before := cleaner.Status()
	if before.LastCleanup == nil {
		t.Fatal("cleaner should have a snapshot before request")
	}

	rr := httptest.NewRecorder()
	srv.handleChatStorageStatus(rr, httptest.NewRequest(http.MethodGet, "/api/chat/storage", nil))

	// The cached snapshot must not have been replaced. The handler must
	// never invoke RunOnce.
	after := cleaner.Status()
	if after.LastCleanup == nil || !after.LastCleanup.At.Equal(before.LastCleanup.At) {
		t.Error("handler must not invoke RunOnce; cached At changed")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
