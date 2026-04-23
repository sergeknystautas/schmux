package directhttp

import (
	"errors"
	"testing"

	"github.com/sergeknystautas/schmux/internal/oneshotdecode"
)

func TestStripAPISuffix(t *testing.T) {
	cases := []struct {
		in        string
		wantID    string
		wantIsAPI bool
	}{
		{"claude-sonnet-4-6", "claude-sonnet-4-6", false},
		{"claude-sonnet-4-6::api", "claude-sonnet-4-6", true},
		{"llama3.2:latest::api", "llama3.2:latest", true},
		{"", "", false},
		{"::api", "", true},
		{"foo::api::api", "foo::api", true},
	}
	for _, c := range cases {
		gotID, gotIsAPI := StripAPISuffix(c.in)
		if gotID != c.wantID || gotIsAPI != c.wantIsAPI {
			t.Errorf("StripAPISuffix(%q) = (%q,%v), want (%q,%v)",
				c.in, gotID, gotIsAPI, c.wantID, c.wantIsAPI)
		}
	}
}

// TestDecodeAPIResponse_DecodeFailure_WrapsErrInvalidResponse proves that
// direct-HTTP decode failures surface the same errors.Is/errors.As semantics
// as the CLI one-shot path. Callers like commit.go and conflictresolve.go
// branch on oneshot.ErrInvalidResponse; this test locks in that contract.
func TestDecodeAPIResponse_DecodeFailure_WrapsErrInvalidResponse(t *testing.T) {
	type result struct {
		Branch string `json:"branch"`
	}
	// Non-JSON output — Decode should fail, decodeAPIResponse should wrap.
	_, err := decodeAPIResponse[result]("this is not json at all")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, oneshotdecode.ErrInvalidResponse) {
		t.Fatalf("errors.Is ErrInvalidResponse must match, got %v", err)
	}
	var ire *oneshotdecode.InvalidResponseError
	if !errors.As(err, &ire) {
		t.Fatalf("errors.As *InvalidResponseError must match, got %v", err)
	}
	if ire.Raw != "this is not json at all" {
		t.Errorf("Raw mismatch: got %q", ire.Raw)
	}
}

// TestDecodeAPIResponse_HappyPath_RoundTrips confirms the direct-HTTP path
// benefits from the shared decode's fence-stripping + object-locating logic
// — previously the package had a simpler stripFences-only path.
func TestDecodeAPIResponse_HappyPath_RoundTrips(t *testing.T) {
	type result struct {
		Branch string `json:"branch"`
	}
	// Input wraps JSON in a markdown fence — the shared decode handles this.
	got, err := decodeAPIResponse[result]("```json\n{\"branch\":\"feat/x\"}\n```")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Branch != "feat/x" {
		t.Errorf("Branch: got %q", got.Branch)
	}
}
