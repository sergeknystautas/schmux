package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// makeClipboardAckHandler returns the HTTP handler for
// POST /api/sessions/{sessionID}/clipboard. It accepts approve/reject and
// reports whether the requestId still matches the latest pending entry
// ("ok") or has been superseded ("stale").
func makeClipboardAckHandler(cs *clipboardState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionID")
		var req contracts.ClipboardAckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "invalid body", http.StatusBadRequest)
			return
		}
		if req.Action != "approve" && req.Action != "reject" {
			writeJSONError(w, "invalid action", http.StatusBadRequest)
			return
		}
		// Both approve and reject clear the pending entry; the difference is
		// purely client-side (browser writes to system clipboard on approve).
		// Stale ack (requestId no longer matches) returns "stale" so the UI
		// can decide whether to surface the newer pending request.
		cleared := cs.clear(sessionID, req.RequestID)
		status := "ok"
		if !cleared {
			status = "stale"
		}
		writeJSON(w, contracts.ClipboardAckResponse{Status: status})
	}
}
