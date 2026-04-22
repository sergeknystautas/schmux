package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/session"
)

// stateWithEntry sets up a clipboardState with one pending broadcast already
// fired (i.e. live in the pending map with a real requestID). Used by the ack
// tests so they don't have to deal with the debounce window.
func stateWithEntry(t *testing.T, sid, text string) (*clipboardState, *capturingBroadcaster, string) {
	t.Helper()
	withTestDebounce(t, 10*time.Millisecond, 5*time.Second)
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)
	cs.onRequest(session.ClipboardRequest{SessionID: sid, Text: text, ByteCount: len(text)})
	// Poll for the debounce to fire (broadcast is the observable signal that
	// the entry has graduated to "broadcast" state with a stable requestID).
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatalf("debounce did not fire for %s", sid)
	}
	cs.mu.Lock()
	entry, ok := cs.pending[sid]
	if !ok {
		cs.mu.Unlock()
		t.Fatalf("expected pending entry for %s", sid)
	}
	rid := entry.requestID
	cs.mu.Unlock()
	return cs, b, rid
}

// dispatch wires a single chi router with the ack route mounted under /api,
// matching the production wiring, and runs one request against it.
func dispatch(t *testing.T, h http.HandlerFunc, sid, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Post("/sessions/{sessionID}/clipboard", h)
	})
	req := httptest.NewRequest("POST", "/api/sessions/"+sid+"/clipboard", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestClipboardAck_Approve_OK(t *testing.T) {
	cs, _, rid := stateWithEntry(t, "s1", "hi")
	body, _ := json.Marshal(contracts.ClipboardAckRequest{Action: "approve", RequestID: rid})
	rec := dispatch(t, makeClipboardAckHandler(cs), "s1", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp contracts.ClipboardAckResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
}

func TestClipboardAck_Reject_OK(t *testing.T) {
	cs, _, rid := stateWithEntry(t, "s1", "secret")
	body, _ := json.Marshal(contracts.ClipboardAckRequest{Action: "reject", RequestID: rid})
	rec := dispatch(t, makeClipboardAckHandler(cs), "s1", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp contracts.ClipboardAckResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
}

func TestClipboardAck_StaleRequestID(t *testing.T) {
	cs, _, _ := stateWithEntry(t, "s1", "hi")
	body, _ := json.Marshal(contracts.ClipboardAckRequest{Action: "approve", RequestID: "stale-id"})
	rec := dispatch(t, makeClipboardAckHandler(cs), "s1", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp contracts.ClipboardAckResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != "stale" {
		t.Errorf("status = %q, want %q", resp.Status, "stale")
	}
}

func TestClipboardAck_UnknownSession(t *testing.T) {
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)
	body, _ := json.Marshal(contracts.ClipboardAckRequest{Action: "approve", RequestID: "anything"})
	rec := dispatch(t, makeClipboardAckHandler(cs), "no-such-session", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (stale path)", rec.Code)
	}
	var resp contracts.ClipboardAckResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != "stale" {
		t.Errorf("status = %q, want %q (no pending entry)", resp.Status, "stale")
	}
}

func TestClipboardAck_RejectInvalidAction(t *testing.T) {
	cs, _, _ := stateWithEntry(t, "s1", "hi")
	body, _ := json.Marshal(map[string]string{"action": "delete-everything", "requestId": "x"})
	rec := dispatch(t, makeClipboardAckHandler(cs), "s1", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestClipboardAck_BadJSON(t *testing.T) {
	cs, _, _ := stateWithEntry(t, "s1", "hi")
	rec := dispatch(t, makeClipboardAckHandler(cs), "s1", "{not-json")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
