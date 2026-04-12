// Package telemetry provides usage tracking via pluggable backends.
package telemetry

// Telemetry defines the interface for tracking events.
type Telemetry interface {
	Track(event string, properties map[string]any)
	Shutdown()
}

// NoopTelemetry is a no-op implementation used when telemetry is disabled.
type NoopTelemetry struct{}

func (n *NoopTelemetry) Track(event string, properties map[string]any) {}
func (n *NoopTelemetry) Shutdown()                                     {}
