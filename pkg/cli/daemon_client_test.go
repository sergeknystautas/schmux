package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetDefaultURL(t *testing.T) {
	url := GetDefaultURL()
	if url != "http://localhost:7337" {
		t.Errorf("got %q, want %q", url, "http://localhost:7337")
	}
}

func TestNewDaemonClient(t *testing.T) {
	baseURL := "http://example.com:8080"
	client := NewDaemonClient(baseURL)

	if client.baseURL != baseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, baseURL)
	}

	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", client.httpClient.Timeout)
	}
}

func TestClient_IsRunning(t *testing.T) {
	t.Run("returns true when healthz returns 200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/healthz" {
				t.Errorf("path = %q, want /api/healthz", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		if !client.IsRunning() {
			t.Error("expected true")
		}
	})

	t.Run("returns false when healthz returns non-200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		if client.IsRunning() {
			t.Error("expected false")
		}
	})

	t.Run("returns false when server is not reachable", func(t *testing.T) {
		client := NewDaemonClient("http://localhost:9999")
		if client.IsRunning() {
			t.Error("expected false")
		}
	})

	t.Run("times out after 2 seconds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(3 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		start := time.Now()
		running := client.IsRunning()
		elapsed := time.Since(start)

		if running {
			t.Error("expected false due to timeout")
		}
		if elapsed < 2*time.Second {
			t.Errorf("elapsed = %v, want > 2s", elapsed)
		}
		if elapsed > 3*time.Second {
			t.Errorf("elapsed = %v, want < 3s (should have timed out)", elapsed)
		}
	})
}

func TestClient_GetConfig(t *testing.T) {
	expectedCfg := Config{
		WorkspacePath: "/tmp/workspaces",
		Repos: []Repo{
			{Name: "test", URL: "git@github.com:test/test.git"},
		},
		RunTargets: []RunTarget{
			{Name: "glm-4.7", Type: "promptable", Command: "~/bin/glm-4.7"},
		},
	}

	t.Run("returns config on success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", r.Method)
			}
			if r.URL.Path != "/api/config" {
				t.Errorf("path = %q, want /api/config", r.URL.Path)
			}
			json.NewEncoder(w).Encode(expectedCfg)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		cfg, err := client.GetConfig()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.WorkspacePath != expectedCfg.WorkspacePath {
			t.Errorf("WorkspacePath = %q, want %q", cfg.WorkspacePath, expectedCfg.WorkspacePath)
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("bad request"))
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		_, err := client.GetConfig()

		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("returns error when server is unreachable", func(t *testing.T) {
		client := NewDaemonClient("http://localhost:9999")
		_, err := client.GetConfig()

		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		_, err := client.GetConfig()

		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestClient_GetWorkspaces(t *testing.T) {
	expectedWorkspaces := []Workspace{
		{ID: "ws-001", Path: "/path/to/ws-001"},
		{ID: "ws-002", Path: "/path/to/ws-002"},
	}

	t.Run("returns workspaces on success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", r.Method)
			}
			if r.URL.Path != "/api/workspaces" {
				t.Errorf("path = %q, want /api/workspaces", r.URL.Path)
			}
			json.NewEncoder(w).Encode(expectedWorkspaces)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		workspaces, err := client.GetWorkspaces()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(workspaces) != 2 {
			t.Errorf("len = %d, want 2", len(workspaces))
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		_, err := client.GetWorkspaces()

		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestClient_GetSessions(t *testing.T) {
	expectedSessions := []WorkspaceWithSessions{
		{
			ID:   "ws-001",
			Path: "/path/to/ws-001",
			Sessions: []Session{
				{ID: "ws-001-abc", Target: "claude"},
			},
		},
	}

	t.Run("returns sessions on success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", r.Method)
			}
			if r.URL.Path != "/api/sessions" {
				t.Errorf("path = %q, want /api/sessions", r.URL.Path)
			}
			json.NewEncoder(w).Encode(expectedSessions)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		sessions, err := client.GetSessions()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sessions) != 1 {
			t.Errorf("len = %d, want 1", len(sessions))
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		_, err := client.GetSessions()

		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestClient_Spawn(t *testing.T) {
	req := SpawnRequest{
		Repo:    "test",
		Branch:  "main",
		Prompt:  "test prompt",
		Targets: map[string]int{"claude": 1},
	}
	expectedResults := []SpawnResult{
		{SessionID: "ws-001-abc", WorkspaceID: "ws-001", Target: "claude"},
	}

	t.Run("returns results on success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if r.URL.Path != "/api/spawn" {
				t.Errorf("path = %q, want /api/spawn", r.URL.Path)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var decodedReq SpawnRequest
			if err := json.NewDecoder(r.Body).Decode(&decodedReq); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			json.NewEncoder(w).Encode(expectedResults)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		results, err := client.Spawn(context.Background(), req)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("len = %d, want 1", len(results))
		}
	})

	t.Run("uses default timeout when ctx is nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(expectedResults)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		results, err := client.Spawn(nil, req)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("len = %d, want 1", len(results))
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid request"))
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		_, err := client.Spawn(context.Background(), req)

		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("returns error on invalid request body", func(t *testing.T) {
		client := NewDaemonClient("://invalid-url")
		_, err := client.Spawn(context.Background(), req)

		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestClient_DisposeSession(t *testing.T) {
	t.Run("returns nil on success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if r.URL.Path != "/api/dispose/test-session" {
				t.Errorf("path = %q, want /api/dispose/test-session", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		err := client.DisposeSession(context.Background(), "test-session")

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("uses default timeout when ctx is nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		err := client.DisposeSession(nil, "test-session")

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("session not found"))
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		err := client.DisposeSession(context.Background(), "test-session")

		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestClient_ScanWorkspaces(t *testing.T) {
	expectedResult := &ScanResult{
		Added: []Workspace{
			{ID: "ws-003", Path: "/path/to/ws-003"},
		},
	}

	t.Run("returns result on success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if r.URL.Path != "/api/workspaces/scan" {
				t.Errorf("path = %q, want /api/workspaces/scan", r.URL.Path)
			}
			json.NewEncoder(w).Encode(expectedResult)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		result, err := client.ScanWorkspaces(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Added) != 1 {
			t.Errorf("len(Added) = %d, want 1", len(result.Added))
		}
	})

	t.Run("uses default timeout when ctx is nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(expectedResult)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		result, err := client.ScanWorkspaces(nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Added) != 1 {
			t.Errorf("len(Added) = %d, want 1", len(result.Added))
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewDaemonClient(server.URL)
		_, err := client.ScanWorkspaces(context.Background())

		if err == nil {
			t.Error("expected error")
		}
	})
}
