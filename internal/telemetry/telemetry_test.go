package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNoopTelemetry(t *testing.T) {
	noop := &NoopTelemetry{}
	// Should not panic
	noop.Track("test", map[string]any{"foo": "bar"})
	noop.Shutdown()
}

func TestNewSendsEvents(t *testing.T) {
	var received struct {
		mu    sync.Mutex
		count int
		id    string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload posthogPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
			return
		}
		received.mu.Lock()
		received.count++
		received.id = payload.DistinctID
		received.mu.Unlock()
	}))
	defer server.Close()

	// Override endpoint for test
	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()
	posthogEndpoint = server.URL

	client := New("").(*Client)

	client.Track("test_event", map[string]any{"foo": "bar"})

	// Shutdown flushes all pending events before returning
	client.Shutdown()

	received.mu.Lock()
	defer received.mu.Unlock()

	if received.count != 1 {
		t.Errorf("expected 1 event, got %d", received.count)
	}
	if received.id == "" {
		t.Error("expected non-empty install ID")
	}
}

func TestGeneratesInstallIDWhenEmpty(t *testing.T) {
	var received struct {
		mu    sync.Mutex
		count int
		id    string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload posthogPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
			return
		}
		received.mu.Lock()
		received.count++
		received.id = payload.DistinctID
		received.mu.Unlock()
	}))
	defer server.Close()

	// Override endpoint for test
	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()
	posthogEndpoint = server.URL

	client := New("").(*Client)

	client.Track("test_event", map[string]any{"foo": "bar"})

	// Shutdown flushes all pending events before returning
	client.Shutdown()

	received.mu.Lock()
	defer received.mu.Unlock()

	if received.count != 1 {
		t.Errorf("expected 1 event, got %d", received.count)
	}
	if received.id == "" {
		t.Error("expected non-empty install ID")
	}
}

func TestTrackSendsEvent(t *testing.T) {
	var received struct {
		mu     sync.Mutex
		event  string
		props  map[string]any
		distID string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload posthogPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
			return
		}
		received.mu.Lock()
		received.event = payload.Event
		received.props = payload.Properties
		received.distID = payload.DistinctID
		received.mu.Unlock()
	}))
	defer server.Close()

	// Override endpoint for test
	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()
	posthogEndpoint = server.URL

	client := New("test-install-id").(*Client)

	client.Track("workspace_created", map[string]any{
		"workspace_id": "ws-001",
		"repo_host":    "github.com",
		"branch":       "main",
	})

	// Shutdown flushes all pending events before returning
	client.Shutdown()

	received.mu.Lock()
	defer received.mu.Unlock()

	if received.event != "workspace_created" {
		t.Errorf("expected event 'workspace_created', got %q", received.event)
	}
	if received.distID != "test-install-id" {
		t.Errorf("expected distinct_id 'test-install-id', got %q", received.distID)
	}
	if received.props["workspace_id"] != "ws-001" {
		t.Errorf("expected workspace_id 'ws-001', got %v", received.props["workspace_id"])
	}
}

func TestTrackNonBlocking(t *testing.T) {
	// Create a slow server that delays responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	// Override endpoint for test
	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()
	posthogEndpoint = server.URL

	client := New("test-install-id").(*Client)
	defer client.Shutdown()

	// Track should return immediately even with slow server
	start := time.Now()
	for i := 0; i < 10; i++ {
		client.Track("test_event", nil)
	}
	elapsed := time.Since(start)

	// Should complete much faster than 10 * 100ms = 1s
	if elapsed > 200*time.Millisecond {
		t.Errorf("Track took too long: %v (expected < 200ms)", elapsed)
	}
}

func TestTrackDropsOnFullQueue(t *testing.T) {
	// Create a server that blocks until we signal
	blockCh := make(chan struct{})
	var requestCountMu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh // Block until test signals
		requestCountMu.Lock()
		requestCount++
		requestCountMu.Unlock()
	}))
	defer server.Close()

	// Override endpoint for test
	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()
	posthogEndpoint = server.URL

	// Create client with small queue
	client := &Client{
		apiKey:     "test-key",
		installID:  "test-install-id",
		eventChan:  make(chan Event, 2), // Very small queue
		stopChan:   make(chan struct{}),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	client.wg.Add(1)
	go client.worker()
	defer client.Shutdown()

	// Fill the queue
	for i := 0; i < 5; i++ {
		client.Track("test_event", map[string]any{"index": i})
	}

	// Unblock the server
	close(blockCh)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Not all events should have been sent due to queue overflow
	requestCountMu.Lock()
	defer requestCountMu.Unlock()
	if requestCount > 3 {
		t.Errorf("expected some events to be dropped, but got %d requests", requestCount)
	}
}

func TestShutdownFlushesEvents(t *testing.T) {
	var receivedCountMu sync.Mutex
	receivedCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCountMu.Lock()
		receivedCount++
		receivedCountMu.Unlock()
	}))
	defer server.Close()

	// Override endpoint for test
	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()
	posthogEndpoint = server.URL

	client := New("test-install-id").(*Client)

	// Send events
	for i := 0; i < 5; i++ {
		client.Track("test_event", nil)
	}

	// Shutdown should flush
	client.Shutdown()

	// Wait a bit for any late arrivals
	time.Sleep(50 * time.Millisecond)

	receivedCountMu.Lock()
	defer receivedCountMu.Unlock()
	if receivedCount != 5 {
		t.Errorf("expected 5 events after shutdown, got %d", receivedCount)
	}
}
