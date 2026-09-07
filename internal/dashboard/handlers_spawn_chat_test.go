package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleSpawnPost_ChatValidation(t *testing.T) {
	cases := []struct {
		name string
		flag bool
		body string
		want string
	}{
		{"flag off", false, `{"repo":"r","branch":"b","prompt":"p","targets":{"claude":1},"kind":"chat"}`, "chat sessions are disabled"},
		{"unknown kind", true, `{"repo":"r","branch":"b","prompt":"p","targets":{"claude":1},"kind":"tui"}`, "unknown session kind"},
		{"remote", true, `{"prompt":"p","targets":{"claude":1},"kind":"chat","remote_profile_id":"rp"}`, "chat sessions are local-only"},
		{"command", true, `{"repo":"r","branch":"b","command":"ls","kind":"chat"}`, "chat sessions require a target"},
		{"no chat mode", true, `{"repo":"r","branch":"b","prompt":"p","targets":{"gemini":1},"kind":"chat"}`, "has no chat mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, cfg, _ := newTestServer(t)
			cfg.ChatSessions = tc.flag
			spawnH := newTestSpawnHandlers(server)
			req := httptest.NewRequest("POST", "/api/spawn", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			spawnH.handleSpawnPost(rr, req)
			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), tc.want) {
				t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

// Chat resume is allowed: the request meets the same resume rules as a
// terminal spawn instead of being rejected for being chat. The test server
// resolves no chat-capable target, so the request stops at the target check;
// what matters is which check.
func TestHandleSpawnPost_ChatResumeNotRejectedAsChat(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	cfg.ChatSessions = true
	spawnH := newTestSpawnHandlers(server)
	body := `{"repo":"r","branch":"b","prompt":"","targets":{"claude":1},"kind":"chat","resume":true}`
	req := httptest.NewRequest("POST", "/api/spawn", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	spawnH.handleSpawnPost(rr, req)
	if strings.Contains(rr.Body.String(), "resume mode") {
		t.Fatalf("chat resume must not be rejected on resume grounds: %s", rr.Body.String())
	}
}
