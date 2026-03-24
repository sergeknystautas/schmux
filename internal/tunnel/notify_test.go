//go:build !notunnel

package tunnel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotifyNtfy(t *testing.T) {
	var receivedBody string
	var receivedTitle string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		receivedBody = string(body)
		receivedTitle = r.Header.Get("Title")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := sendNtfyNotification(server.URL, "schmux remote access", "https://test.trycloudflare.com")
	if err != nil {
		t.Fatalf("sendNtfyNotification() error: %v", err)
	}

	if !strings.Contains(receivedBody, "https://test.trycloudflare.com") {
		t.Errorf("body should contain URL, got %q", receivedBody)
	}
	if receivedTitle != "schmux remote access" {
		t.Errorf("title = %q, want %q", receivedTitle, "schmux remote access")
	}
}

func TestNotifyConfig(t *testing.T) {
	t.Run("no-op when nothing configured", func(t *testing.T) {
		nc := NotifyConfig{}
		err := nc.Send("https://test.trycloudflare.com", "tunnel up")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
