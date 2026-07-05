package events

import (
	"context"
	"encoding/json"
)

// ResumeIDHandler dispatches resume_id events (harness-native conversation id).
type ResumeIDHandler struct {
	set func(sessionID, resumeID string)
}

// NewResumeIDHandler creates a handler that forwards captured resume ids.
func NewResumeIDHandler(set func(sessionID, resumeID string)) *ResumeIDHandler {
	return &ResumeIDHandler{set: set}
}

func (h *ResumeIDHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	if raw.Type != "resume_id" {
		return
	}
	var ev struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &ev); err != nil || ev.ID == "" {
		return
	}
	h.set(sessionID, ev.ID)
}
