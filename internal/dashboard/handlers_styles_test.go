package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func styleRequest(method, url string, body interface{}) *http.Request {
	var req *http.Request
	if body != nil {
		data, _ := json.Marshal(body)
		req = httptest.NewRequest(method, url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, url, nil)
	}
	return req
}

func styleRequestWithID(method, url, id string, body interface{}) *http.Request {
	req := styleRequest(method, url, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

func TestHandleListStyles_ReturnsBuiltins(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := styleRequest("GET", "/api/styles", nil)
	rr := httptest.NewRecorder()
	server.handleListStyles(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp contracts.StyleListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Styles) == 0 {
		t.Fatal("expected built-in styles, got none")
	}

	// Verify at least one known built-in exists
	found := false
	for _, s := range resp.Styles {
		if s.ID == "pirate" {
			found = true
			if !s.BuiltIn {
				t.Error("pirate style should be built_in=true")
			}
			break
		}
	}
	if !found {
		t.Error("expected to find built-in 'pirate' style")
	}
}

func TestHandleCreateStyle_Validation(t *testing.T) {
	tests := []struct {
		name       string
		body       contracts.StyleCreateRequest
		wantCode   int
		wantSubstr string
	}{
		{
			name:       "missing id",
			body:       contracts.StyleCreateRequest{Name: "Test", Icon: "x", Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "id, name, icon, and prompt are required",
		},
		{
			name:       "missing name",
			body:       contracts.StyleCreateRequest{ID: "test", Icon: "x", Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "id, name, icon, and prompt are required",
		},
		{
			name:       "missing icon",
			body:       contracts.StyleCreateRequest{ID: "test", Name: "Test", Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "id, name, icon, and prompt are required",
		},
		{
			name:       "missing prompt",
			body:       contracts.StyleCreateRequest{ID: "test", Name: "Test", Icon: "x"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "id, name, icon, and prompt are required",
		},
		{
			name:       "invalid slug with uppercase",
			body:       contracts.StyleCreateRequest{ID: "Bad-Slug", Name: "Test", Icon: "x", Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "URL-safe slug",
		},
		{
			name:       "invalid slug with spaces",
			body:       contracts.StyleCreateRequest{ID: "bad slug", Name: "Test", Icon: "x", Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "URL-safe slug",
		},
		{
			name:       "reserved id create",
			body:       contracts.StyleCreateRequest{ID: "create", Name: "Test", Icon: "x", Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "reserved ID",
		},
		{
			name:       "reserved id none",
			body:       contracts.StyleCreateRequest{ID: "none", Name: "Test", Icon: "x", Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "reserved ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, _ := newTestServer(t)
			req := styleRequest("POST", "/api/styles", tt.body)
			rr := httptest.NewRecorder()
			server.handleCreateStyle(rr, req)

			if rr.Code != tt.wantCode {
				t.Errorf("got status %d, want %d; body: %s", rr.Code, tt.wantCode, rr.Body.String())
			}
			if !bytes.Contains(rr.Body.Bytes(), []byte(tt.wantSubstr)) {
				t.Errorf("body %q should contain %q", rr.Body.String(), tt.wantSubstr)
			}
		})
	}
}

func TestHandleCreateStyle_SuccessAndDuplicate(t *testing.T) {
	server, _, _ := newTestServer(t)

	body := contracts.StyleCreateRequest{
		ID:      "my-style",
		Name:    "My Style",
		Icon:    "x",
		Tagline: "A test style",
		Prompt:  "Talk like a test.",
	}

	// First create should succeed
	req := styleRequest("POST", "/api/styles", body)
	rr := httptest.NewRecorder()
	server.handleCreateStyle(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var created contracts.Style
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if created.ID != "my-style" {
		t.Errorf("expected ID 'my-style', got %q", created.ID)
	}
	if created.BuiltIn {
		t.Error("custom style should not be built_in")
	}

	// Duplicate create should return 409
	req = styleRequest("POST", "/api/styles", body)
	rr = httptest.NewRecorder()
	server.handleCreateStyle(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDeleteStyle_BuiltInResets(t *testing.T) {
	server, _, _ := newTestServer(t)

	// Delete a built-in style (pirate) — should reset, not remove
	req := styleRequestWithID("DELETE", "/api/styles/pirate", "pirate", nil)
	rr := httptest.NewRecorder()
	server.handleDeleteStyle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify it still exists after "delete" (reset)
	req = styleRequestWithID("GET", "/api/styles/pirate", "pirate", nil)
	rr = httptest.NewRecorder()
	server.handleGetStyle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("built-in style should still exist after delete (reset), got %d", rr.Code)
	}

	var st contracts.Style
	if err := json.NewDecoder(rr.Body).Decode(&st); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !st.BuiltIn {
		t.Error("reset built-in style should still have built_in=true")
	}
}

func TestHandleDeleteStyle_CustomRemoves(t *testing.T) {
	server, _, _ := newTestServer(t)

	// Create a custom style first
	createBody := contracts.StyleCreateRequest{
		ID: "ephemeral", Name: "Ephemeral", Icon: "x", Prompt: "Be fleeting.",
	}
	req := styleRequest("POST", "/api/styles", createBody)
	rr := httptest.NewRecorder()
	server.handleCreateStyle(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Delete the custom style
	req = styleRequestWithID("DELETE", "/api/styles/ephemeral", "ephemeral", nil)
	rr = httptest.NewRecorder()
	server.handleDeleteStyle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify it's gone (404)
	req = styleRequestWithID("GET", "/api/styles/ephemeral", "ephemeral", nil)
	rr = httptest.NewRecorder()
	server.handleGetStyle(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 after deleting custom style, got %d", rr.Code)
	}
}

func TestHandleDeleteStyle_NotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := styleRequestWithID("DELETE", "/api/styles/nonexistent", "nonexistent", nil)
	rr := httptest.NewRecorder()
	server.handleDeleteStyle(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}
