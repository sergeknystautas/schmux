package events

import "context"

// MonitorCallback is called for every event, regardless of type.
type MonitorCallback func(sessionID string, raw RawEvent, data []byte)

// MonitorHandler forwards all events to a callback for dev-mode monitoring.
type MonitorHandler struct {
	callback MonitorCallback
}

// NewMonitorHandler creates a handler that forwards all events.
func NewMonitorHandler(callback MonitorCallback) *MonitorHandler {
	return &MonitorHandler{callback: callback}
}

func (h *MonitorHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	h.callback(sessionID, raw, data)
}
