package events

import "context"

// EventHandler processes events dispatched by an EventWatcher.
type EventHandler interface {
	HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte)
}
