package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestHandleTLSValidate_MissingPaths(t *testing.T) {
	s := &Server{}
	body, _ := json.Marshal(contracts.TLSValidateRequest{CertPath: "", KeyPath: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/tls/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleTLSValidate(w, req)

	var resp contracts.TLSValidateResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Valid {
		t.Error("expected invalid for empty paths")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleTLSValidate_NonexistentFiles(t *testing.T) {
	s := &Server{}
	body, _ := json.Marshal(contracts.TLSValidateRequest{
		CertPath: "/nonexistent/cert.pem",
		KeyPath:  "/nonexistent/key.pem",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tls/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleTLSValidate(w, req)

	var resp contracts.TLSValidateResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Valid {
		t.Error("expected invalid for nonexistent files")
	}
}

func TestHandleTLSValidate_MethodNotAllowed(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/tls/validate", nil)
	w := httptest.NewRecorder()

	s.handleTLSValidate(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
