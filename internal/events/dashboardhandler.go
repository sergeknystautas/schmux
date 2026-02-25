package events

import (
	"context"
	"encoding/json"
)

// StatusCallback is called when a status event is received.
type StatusCallback func(sessionID, state, message, intent, blockers string)

// DashboardHandler dispatches status events to the dashboard.
type DashboardHandler struct {
	callback StatusCallback
}

// NewDashboardHandler creates a handler that forwards status events.
func NewDashboardHandler(callback StatusCallback) *DashboardHandler {
	return &DashboardHandler{callback: callback}
}

func (h *DashboardHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	if raw.Type != "status" {
		return
	}
	var status StatusEvent
	if err := json.Unmarshal(data, &status); err != nil {
		return
	}
	h.callback(sessionID, status.State, status.Message, status.Intent, status.Blockers)
}
