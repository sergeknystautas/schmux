//go:build noposthog

package telemetry

import (
	"github.com/charmbracelet/log"
)

// Event represents a telemetry event (stub).
type Event struct {
	Name       string         `json:"event"`
	Properties map[string]any `json:"properties"`
}

// Client is a type alias for NoopTelemetry when PostHog is excluded.
type Client = NoopTelemetry

// New returns a no-op telemetry client when PostHog is excluded.
func New(_ string, _ *log.Logger) Telemetry {
	return &NoopTelemetry{}
}

// IsPostHogAvailable reports whether PostHog is included in this build.
func IsPostHogAvailable() bool { return false }
