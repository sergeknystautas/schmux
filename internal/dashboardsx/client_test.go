//go:build !nodashboardsx

package dashboardsx

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Heartbeat(t *testing.T) {
	var received HeartbeatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/heartbeat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "abc12")
	if _, err := client.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat() error: %v", err)
	}
	if received.InstanceKey != "test-key" {
		t.Errorf("InstanceKey = %q, want %q", received.InstanceKey, "test-key")
	}
}

func TestClient_Heartbeat_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", 503)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "abc12")
	if _, err := client.Heartbeat(); err == nil {
		t.Fatal("expected error from 503 response")
	}
}

func TestClient_CertProvisioningStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cert-provisioning/start" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req CertProvisioningStartRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		if req.InstanceKey != "test-key" {
			t.Errorf("InstanceKey = %q, want %q", req.InstanceKey, "test-key")
		}
		json.NewEncoder(w).Encode(CertProvisioningStartResponse{
			ChallengeToken: "tok-123",
			ExpiresIn:      300,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "abc12")
	resp, err := client.CertProvisioningStart()
	if err != nil {
		t.Fatalf("CertProvisioningStart() error: %v", err)
	}
	if resp.ChallengeToken != "tok-123" {
		t.Errorf("ChallengeToken = %q, want %q", resp.ChallengeToken, "tok-123")
	}
	if resp.ExpiresIn != 300 {
		t.Errorf("ExpiresIn = %d, want 300", resp.ExpiresIn)
	}
}

func TestClient_DNSChallengeCreate(t *testing.T) {
	var received DNSChallengeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dns-challenge" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "abc12")
	if err := client.DNSChallengeCreate("tok-123", "acme-value"); err != nil {
		t.Fatalf("DNSChallengeCreate() error: %v", err)
	}
	if received.ChallengeToken != "tok-123" {
		t.Errorf("ChallengeToken = %q, want %q", received.ChallengeToken, "tok-123")
	}
	if received.ChallengeValue != "acme-value" {
		t.Errorf("ChallengeValue = %q, want %q", received.ChallengeValue, "acme-value")
	}
}

func TestClient_DNSChallengeDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "abc12")
	if err := client.DNSChallengeDelete("tok-123"); err != nil {
		t.Fatalf("DNSChallengeDelete() error: %v", err)
	}
}

func TestClient_CallbackExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback/exchange" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req CallbackExchangeRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		if req.CallbackToken != "cb-token" {
			t.Errorf("CallbackToken = %q, want %q", req.CallbackToken, "cb-token")
		}
		json.NewEncoder(w).Encode(CallbackExchangeResponse{
			InstanceKey: "test-key",
			Code:        "abc12",
			Email:       "user@example.com",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "abc12")
	resp, err := client.CallbackExchange("cb-token")
	if err != nil {
		t.Fatalf("CallbackExchange() error: %v", err)
	}
	if resp.InstanceKey != "test-key" {
		t.Errorf("InstanceKey = %q, want %q", resp.InstanceKey, "test-key")
	}
	if resp.Code != "abc12" {
		t.Errorf("Code = %q, want %q", resp.Code, "abc12")
	}
	if resp.Email != "user@example.com" {
		t.Errorf("Email = %q, want %q", resp.Email, "user@example.com")
	}
}

func TestHeartbeatReturnsStatusCode(t *testing.T) {
	tests := []struct {
		name           string
		serverStatus   int
		expectedStatus int
		expectErr      bool
	}{
		{"success", 200, 200, false},
		{"forbidden", 403, 403, true},
		{"server error", 500, 500, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.serverStatus)
				w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()

			client := NewClient(srv.URL, "test-key", "12345")
			status, err := client.Heartbeat()
			if status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, status)
			}
			if tc.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewClient_DefaultServiceURL(t *testing.T) {
	client := NewClient("", "key", "code")
	if client.ServiceURL != DefaultServiceURL {
		t.Errorf("ServiceURL = %q, want %q", client.ServiceURL, DefaultServiceURL)
	}
}
