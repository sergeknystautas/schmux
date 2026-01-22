package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateCSRF_EmptyToken(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	// No X-CSRF-Token header

	if s.validateCSRF(req) {
		t.Error("expected false for missing CSRF token")
	}
}

func TestValidateCSRF_EmptyTokenWithWhitespace(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "   ")

	if s.validateCSRF(req) {
		t.Error("expected false for whitespace-only CSRF token")
	}
}

func TestValidateCSRF_MissingCookie(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "some-token")
	// No cookie set

	if s.validateCSRF(req) {
		t.Error("expected false for missing CSRF cookie")
	}
}

func TestValidateCSRF_Mismatch(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "token-a")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token-b"})

	if s.validateCSRF(req) {
		t.Error("expected false for mismatched CSRF tokens")
	}
}

func TestValidateCSRF_Match(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "matching-token")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "matching-token"})

	if !s.validateCSRF(req) {
		t.Error("expected true for matching CSRF tokens")
	}
}

func TestValidateCSRF_MatchWithWhitespace(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "  matching-token  ")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "  matching-token  "})

	if !s.validateCSRF(req) {
		t.Error("expected true for matching CSRF tokens with whitespace trimmed")
	}
}

func TestDecodeSessionSecret_Valid(t *testing.T) {
	// Create a valid base64-encoded secret (32 bytes -> 43 chars in base64 raw)
	secret := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY" // 32 bytes decoded
	key, err := decodeSessionSecret(secret)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if key == nil {
		t.Error("expected non-nil key")
	}
}

func TestDecodeSessionSecret_InvalidBase64(t *testing.T) {
	// Invalid base64 characters should fail
	secret := "not-valid-base64!!!"
	_, err := decodeSessionSecret(secret)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestRandomToken(t *testing.T) {
	token, err := randomToken(32)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
	// Token should be base64 encoded 32 bytes
	if len(token) < 40 {
		t.Errorf("token seems too short: %d chars", len(token))
	}

	// Ensure tokens are unique
	token2, _ := randomToken(32)
	if token == token2 {
		t.Error("expected unique tokens")
	}
}

func TestRandomToken_DifferentLengths(t *testing.T) {
	tests := []int{16, 32, 64}
	for _, length := range tests {
		token, err := randomToken(length)
		if err != nil {
			t.Errorf("length %d: expected no error, got %v", length, err)
		}
		if token == "" {
			t.Errorf("length %d: expected non-empty token", length)
		}
	}
}
