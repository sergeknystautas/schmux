//go:build notelemetry

package telemetry

import (
	"github.com/charmbracelet/log"
)

// Event represents a telemetry event (stub).
type Event struct {
	Name       string         `json:"event"`
	Properties map[string]any `json:"properties"`
}

// Telemetry defines the interface for tracking events.
type Telemetry interface {
	Track(event string, properties map[string]any)
	Shutdown()
}

// NoopTelemetry is a no-op implementation used when telemetry is excluded.
type NoopTelemetry struct{}

func (n *NoopTelemetry) Track(event string, properties map[string]any) {}
func (n *NoopTelemetry) Shutdown()                                     {}

// Client is a type alias for NoopTelemetry when telemetry is excluded.
type Client = NoopTelemetry

// New returns a no-op telemetry client when the module is excluded.
func New(_ string, _ *log.Logger) Telemetry {
	return &NoopTelemetry{}
}

// IsAvailable reports whether the telemetry module is included in this build.
func IsAvailable() bool { return false }
