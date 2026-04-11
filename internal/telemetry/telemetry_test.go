//go:build !notelemetry

package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNoopTelemetry(t *testing.T) {
	// Verify NoopTelemetry satisfies the Telemetry interface at compile time
	var _ Telemetry = (*NoopTelemetry)(nil)

	// Verify Track does not make HTTP requests
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
	}))
	defer server.Close()

	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()
	posthogEndpoint = server.URL

	noop := &NoopTelemetry{}
	noop.Track("test", map[string]any{"foo": "bar"})
	noop.Shutdown()
	// Shutdown is synchronous — if no requests were made by now, none will be
	if atomic.LoadInt32(&requestCount) != 0 {
		t.Error("NoopTelemetry should not make HTTP requests")
	}
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

	client := New("", nil).(*Client)

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
	// When installID is "", New() should generate a UUID.
	// When installID is provided, it should be used as-is.
	// This test verifies both cases and that the IDs differ.

	var generatedID, providedID string
	var mu sync.Mutex

	serverGenerated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload posthogPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return
		}
		mu.Lock()
		generatedID = payload.DistinctID
		mu.Unlock()
	}))
	defer serverGenerated.Close()

	serverProvided := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload posthogPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return
		}
		mu.Lock()
		providedID = payload.DistinctID
		mu.Unlock()
	}))
	defer serverProvided.Close()

	originalEndpoint := posthogEndpoint
	defer func() { posthogEndpoint = originalEndpoint }()

	// Case 1: empty installID — should auto-generate a UUID
	posthogEndpoint = serverGenerated.URL
	client1 := New("", nil)
	client1.Track("test_event", nil)
	client1.Shutdown()

	// Case 2: explicit installID — should use the provided value
	posthogEndpoint = serverProvided.URL
	client2 := New("my-explicit-id", nil)
	client2.Track("test_event", nil)
	client2.Shutdown()

	mu.Lock()
	defer mu.Unlock()

	if generatedID == "" {
		t.Error("expected generated install ID to be non-empty")
	}
	if providedID != "my-explicit-id" {
		t.Errorf("expected provided install ID 'my-explicit-id', got %q", providedID)
	}
	if generatedID == "my-explicit-id" {
		t.Error("generated install ID should not equal the explicit ID")
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

	client := New("test-install-id", nil).(*Client)

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

	client := New("test-install-id", nil).(*Client)
	defer client.Shutdown()

	// Track should return immediately even with slow server.
	// Each Track call is a non-blocking channel send, so 10 calls
	// should complete in microseconds regardless of server latency.
	start := time.Now()
	for i := 0; i < 10; i++ {
		client.Track("test_event", nil)
	}
	elapsed := time.Since(start)

	// Should complete much faster than 10 * 100ms = 1s.
	// Use 500ms threshold to be resilient to CI load spikes.
	if elapsed > 500*time.Millisecond {
		t.Errorf("Track took too long: %v (expected < 500ms for non-blocking sends)", elapsed)
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

	// Fill the queue — send more events than buffer capacity
	for i := 0; i < 5; i++ {
		client.Track("test_event", map[string]any{"index": i})
	}

	// Unblock the server so pending requests can complete
	close(blockCh)

	// Shutdown flushes remaining events and waits for worker to finish
	client.Shutdown()

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

	// Override endpoint and flush timeout for test — generous timeout for parallel load
	originalEndpoint := posthogEndpoint
	originalTimeout := flushTimeout
	defer func() {
		posthogEndpoint = originalEndpoint
		flushTimeout = originalTimeout
	}()
	posthogEndpoint = server.URL
	flushTimeout = 15 * time.Second

	client := New("test-install-id", nil).(*Client)

	// Send events
	for i := 0; i < 5; i++ {
		client.Track("test_event", nil)
	}

	// Shutdown should flush all pending events before returning
	client.Shutdown()

	receivedCountMu.Lock()
	defer receivedCountMu.Unlock()
	if receivedCount != 5 {
		t.Errorf("expected 5 events after shutdown, got %d", receivedCount)
	}
}
