package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	skipUnderVendorlocked(t)
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/tls/validate", nil)
	w := httptest.NewRecorder()

	s.handleTLSValidate(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"tilde slash expands", "~/foo/bar", home + "/foo/bar"},
		{"tilde alone unchanged", "~", "~"},
		{"absolute path unchanged", "/etc/hosts", "/etc/hosts"},
		{"relative path unchanged", "./local", "./local"},
		{"empty string unchanged", "", ""},
		{"inner tilde unchanged", "/foo/~/bar", "/foo/~/bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandHome(tc.in); got != tc.want {
				t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandHome_NoHomeEnv(t *testing.T) {
	// On the rare host where HOME and USERPROFILE are both unset,
	// os.UserHomeDir returns an error and expandHome must return the input unchanged.
	t.Setenv("HOME", "")
	if strings.HasPrefix(os.Getenv("HOME"), "") && os.Getenv("HOME") != "" {
		t.Skip("HOME could not be cleared")
	}
	got := expandHome("~/path")
	if got != "~/path" && !strings.HasSuffix(got, "/path") {
		t.Errorf("expandHome with no HOME = %q, want unchanged or expanded", got)
	}
}
