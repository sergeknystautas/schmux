//go:build !notelemetry

// Package telemetry provides anonymous usage tracking via PostHog.
// Telemetry is enabled by default with opt-out available via config.
package telemetry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
)

// posthogAPIKey is the public write-only key for anonymous telemetry.
// This is safe to embed in source - it only allows writing events.
const posthogAPIKey = "phc_6060MNk0DiOAE7CEpLWRaHJQtNJRNq9enfiYm7DtO3w"

const (
	// defaultPosthogEndpoint is the default PostHog capture endpoint.
	defaultPosthogEndpoint = "https://us.posthog.com/capture/"

	// eventQueueSize is the maximum number of events to buffer.
	eventQueueSize = 100

	// flushTimeout is the maximum time to wait for pending events on shutdown.
	flushTimeout = 5 * time.Second

	// failureLogInterval is the minimum time between failure log messages.
	failureLogInterval = 1 * time.Minute
)

// posthogEndpoint allows overriding the endpoint for testing.
var posthogEndpoint = defaultPosthogEndpoint

// Event represents a telemetry event to be sent to PostHog.
type Event struct {
	Name       string         `json:"event"`
	Properties map[string]any `json:"properties"`
}

// Telemetry defines the interface for tracking events.
type Telemetry interface {
	Track(event string, properties map[string]any)
	Shutdown()
}

// NoopTelemetry is a no-op implementation used when telemetry is disabled.
type NoopTelemetry struct{}

func (n *NoopTelemetry) Track(event string, properties map[string]any) {}
func (n *NoopTelemetry) Shutdown()                                     {}

// Client is the PostHog telemetry client.
type Client struct {
	apiKey       string
	installID    string
	eventChan    chan Event
	stopChan     chan struct{}
	wg           sync.WaitGroup
	httpClient   *http.Client
	logger       *log.Logger
	lastFailLog  time.Time
	failLogMu    sync.Mutex
	shutdownOnce sync.Once
}

// New creates a new telemetry client.
// If installID is empty, a new UUID will be generated.
func New(installID string, logger *log.Logger) Telemetry {
	// Honor environment variable opt-out
	if os.Getenv("SCHMUX_TELEMETRY_OFF") != "" || os.Getenv("DO_NOT_TRACK") != "" {
		return &NoopTelemetry{}
	}

	if installID == "" {
		installID = uuid.New().String()
	}

	c := &Client{
		apiKey:     posthogAPIKey,
		installID:  installID,
		eventChan:  make(chan Event, eventQueueSize),
		stopChan:   make(chan struct{}),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}

	c.wg.Add(1)
	go c.worker()

	return c
}

// Track queues an event to be sent to PostHog.
// This method is non-blocking and returns immediately.
// If the event queue is full, the oldest event is dropped.
func (c *Client) Track(event string, properties map[string]any) {
	// Copy properties to avoid mutations
	props := make(map[string]any, len(properties))
	for k, v := range properties {
		props[k] = v
	}

	evt := Event{
		Name:       event,
		Properties: props,
	}

	select {
	case c.eventChan <- evt:
	default:
		// Queue full, drop the event
		c.logFailure("event queue full, dropping event", "event", event)
	}
}

// Shutdown stops the telemetry client and flushes pending events.
// It waits up to flushTimeout for events to be sent.
func (c *Client) Shutdown() {
	c.shutdownOnce.Do(func() {
		close(c.stopChan)
	})

	// Wait for worker to finish with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(flushTimeout):
		if c.logger != nil {
			c.logger.Warn("flush timeout exceeded")
		}
	}
}

// worker processes events from the queue and sends them to PostHog.
func (c *Client) worker() {
	defer c.wg.Done()

	for {
		select {
		case <-c.stopChan:
			// Drain remaining events
			for {
				select {
				case evt := <-c.eventChan:
					c.sendEvent(evt)
				default:
					return
				}
			}
		case evt := <-c.eventChan:
			c.sendEvent(evt)
		}
	}
}

// posthogPayload represents the JSON payload sent to PostHog.
type posthogPayload struct {
	APIKey     string         `json:"api_key"`
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties"`
	Timestamp  string         `json:"timestamp"`
}

// sendEvent sends a single event to PostHog.
func (c *Client) sendEvent(evt Event) {
	payload := posthogPayload{
		APIKey:     c.apiKey,
		Event:      evt.Name,
		DistinctID: c.installID,
		Properties: evt.Properties,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		c.logFailure("failed to marshal event", "err", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, posthogEndpoint, bytes.NewReader(body))
	if err != nil {
		c.logFailure("failed to create request", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logFailure("failed to send event", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		c.logFailure("posthog returned error status", "status", resp.StatusCode)
	}
}

// IsAvailable reports whether the telemetry module is included in this build.
func IsAvailable() bool { return true }

// logFailure logs a failure message, rate-limited to once per minute.
func (c *Client) logFailure(msg string, keyvals ...interface{}) {
	c.failLogMu.Lock()
	defer c.failLogMu.Unlock()

	if time.Since(c.lastFailLog) < failureLogInterval {
		return
	}

	c.lastFailLog = time.Now()
	if c.logger != nil {
		c.logger.Error(msg, keyvals...)
	}
}
