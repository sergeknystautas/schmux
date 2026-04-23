package oneshotdecode

import (
	"errors"
	"testing"
)

func TestNormalizeJSONPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty returns empty", "", ""},
		{"whitespace only returns empty", "   \t  ", ""},
		{"replaces curly double quotes", "“hello”", "\"hello\""},
		{"collapses multiple spaces", "a  b   c", "a b c"},
		{"replaces tabs with spaces", "a\tb", "a b"},
		{"trims surrounding whitespace", "  hello  ", "hello"},
		{"combined normalization", " “key” :  “value” ", "\"key\" : \"value\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeJSONPayload(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeJSONPayload(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecode_PlainObject(t *testing.T) {
	type result struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	got, err := Decode[result](`{"name":"foo","value":7}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" || got.Value != 7 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecode_StripsCodeFence(t *testing.T) {
	type result struct {
		Name string `json:"name"`
	}
	got, err := Decode[result]("```json\n{\"name\":\"bar\"}\n```")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "bar" {
		t.Errorf("got %+v", got)
	}
}

func TestDecode_NoJSONReturnsError(t *testing.T) {
	type result struct{ Name string }
	_, err := Decode[result]("no json here")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInvalidResponseError_Unwraps(t *testing.T) {
	inner := errors.New("json decode boom")
	ire := &InvalidResponseError{Raw: "raw payload here", Err: inner}

	if !errors.Is(ire, ErrInvalidResponse) {
		t.Fatalf("errors.Is ErrInvalidResponse should match, got false")
	}
	if !errors.Is(ire, inner) {
		t.Fatalf("errors.Is underlying error should match")
	}

	var extracted *InvalidResponseError
	if !errors.As(ire, &extracted) {
		t.Fatalf("errors.As should extract *InvalidResponseError")
	}
	if extracted.Raw != "raw payload here" {
		t.Fatalf("Raw mismatch: %q", extracted.Raw)
	}
}
