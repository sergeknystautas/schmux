package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

func TestChatTelemetryCollectsBrowserStages(t *testing.T) {
	srv, _, _ := newTestServer(t)
	var generalLog bytes.Buffer
	srv.logger = log.NewWithOptions(&generalLog, log.Options{})
	body := `{"loads":[{"sessionId":"chat-1","at":"2026-09-24T14:00:00Z","start":"click","frameChars":15000000,"records":5700,"routeToSocketMs":20,"socketOpenMs":5,"historyWaitMs":200,"parseMs":16,"reduceMs":47,"commitMs":60,"afterPaintMs":40,"totalMs":388}],"images":[{"path":"/api/chat/chat-1/images/image-1/0","at":"2026-09-24T14:00:01Z","resourceMs":123,"loadMs":130,"transferBytes":456,"decodedBytes":789,"width":1000,"height":800}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat/telemetry", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleChatTelemetry(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	events := readChatPerformanceEvents(t)
	if len(events) != 2 || events[0].Kind != "browser_load" || events[1].Kind != "browser_image" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Data["session"] != "chat-1" || events[0].Data["frame_chars"] != float64(15000000) || events[0].Data["total_ms"] != float64(388) {
		t.Errorf("load event = %+v", events[0])
	}
	if events[1].Data["transfer_bytes"] != float64(456) {
		t.Errorf("image event = %+v", events[1])
	}
	if generalLog.Len() != 0 {
		t.Errorf("chat performance leaked into general log: %s", generalLog.String())
	}
}

func TestChatTelemetryRejectsOversizedBatch(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := `{"loads":[` + strings.Repeat(`{},`, 20) + `{}` + `]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat/telemetry", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleChatTelemetry(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestChatTelemetryDetailedFieldsRespectConfigGate(t *testing.T) {
	srv, cfg, _ := newTestServer(t)
	body := `{"loads":[{"sessionId":"chat-1","loadId":"load-1","reduction":{"categories":[{"category":"user_message","records":1,"durationMs":2,"maxMs":2}],"items":1}}],"images":[{"path":"/api/chat/chat-1/images/u1/0","ttfbMs":3,"downloadMs":4,"postResponseMs":5}]}`
	post := func() {
		req := httptest.NewRequest(http.MethodPost, "/api/chat/telemetry", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.handleChatTelemetry(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	}
	post()
	events := readChatPerformanceEvents(t)
	if _, ok := events[0].Data["reduction"]; ok {
		t.Fatalf("reduction captured while gate off: %+v", events[0])
	}
	if _, ok := events[1].Data["ttfb_ms"]; ok {
		t.Fatalf("image phases captured while gate off: %+v", events[1])
	}
	cfg.ChatLoadProfilingEnabled = true
	post()
	events = readChatPerformanceEvents(t)
	if events[2].Data["load_id"] != "load-1" || events[2].Data["reduction"] == nil {
		t.Errorf("detailed load = %+v", events[2])
	}
	if events[3].Data["ttfb_ms"] != float64(3) || events[3].Data["download_ms"] != float64(4) {
		t.Errorf("detailed image = %+v", events[3])
	}
}
