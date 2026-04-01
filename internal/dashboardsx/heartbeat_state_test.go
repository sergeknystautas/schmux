//go:build !nodashboardsx

package dashboardsx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type mockStatusWriter struct {
	mu     sync.Mutex
	status *HeartbeatStatus
}

func (m *mockStatusWriter) SetHeartbeatStatus(s *HeartbeatStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.status = &cp
}

func (m *mockStatusWriter) get() *HeartbeatStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil {
		return nil
	}
	cp := *m.status
	return &cp
}

func TestStartHeartbeatPersistsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte("registration not found"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key", "12345")
	writer := &mockStatusWriter{}

	ctx, cancel := context.WithCancel(context.Background())
	go StartHeartbeat(ctx, client, writer)

	// Wait for initial heartbeat to be processed
	deadline := time.After(2 * time.Second)
	for {
		if s := writer.get(); s != nil {
			if s.StatusCode != 403 {
				t.Errorf("expected 403, got %d", s.StatusCode)
			}
			if s.Error == "" {
				t.Error("expected non-empty error")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for heartbeat status")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
}
