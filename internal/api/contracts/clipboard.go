package contracts

// ClipboardRequestEvent is broadcast on /ws/dashboard when a TUI emits OSC 52.
// The frontend renders an approve/reject banner so the user can copy the text
// into their system clipboard explicitly.
type ClipboardRequestEvent struct {
	Type                 string `json:"type"` // "clipboardRequest"
	SessionID            string `json:"sessionId"`
	RequestID            string `json:"requestId"`
	Text                 string `json:"text"`
	ByteCount            int    `json:"byteCount"`
	StrippedControlChars int    `json:"strippedControlChars"`
}

// ClipboardClearedEvent is broadcast when a pending clipboard request is
// resolved (approved, rejected, superseded, or expired).
type ClipboardClearedEvent struct {
	Type      string `json:"type"` // "clipboardCleared"
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
}

// ClipboardAckRequest is the body of POST /api/sessions/{sessionID}/clipboard.
type ClipboardAckRequest struct {
	Action    string `json:"action"` // "approve" | "reject"
	RequestID string `json:"requestId"`
}

// ClipboardAckResponse is the response from POST /api/sessions/{sessionID}/clipboard.
// status is "ok" if the request was found and cleared, or "stale" if the
// requestId no longer matches the pending entry (raced another approval).
type ClipboardAckResponse struct {
	Status string `json:"status"`
}
