package events

import (
	"context"
	"testing"
)

func TestResumeIDHandler(t *testing.T) {
	var gotSession, gotID string
	h := NewResumeIDHandler(func(sessionID, resumeID string) {
		gotSession, gotID = sessionID, resumeID
	})

	data := []byte(`{"type":"resume_id","id":"conv-abc"}`)
	raw, _ := ParseRawEvent(data)
	h.HandleEvent(context.Background(), "s1", raw, data)
	if gotSession != "s1" || gotID != "conv-abc" {
		t.Fatalf("got (%q,%q), want (s1,conv-abc)", gotSession, gotID)
	}

	// Wrong type is ignored.
	gotSession, gotID = "", ""
	other := []byte(`{"type":"status","state":"working"}`)
	oraw, _ := ParseRawEvent(other)
	h.HandleEvent(context.Background(), "s1", oraw, other)
	if gotSession != "" || gotID != "" {
		t.Fatalf("status event should be ignored, got (%q,%q)", gotSession, gotID)
	}

	// Empty id is ignored.
	empty := []byte(`{"type":"resume_id","id":""}`)
	eraw, _ := ParseRawEvent(empty)
	h.HandleEvent(context.Background(), "s1", eraw, empty)
	if gotSession != "" {
		t.Fatal("empty id should be ignored")
	}
}
