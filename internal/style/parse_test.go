package style

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	input := `---
id: pirate
name: Pirate
icon: "\U0001f3f4\u200d\u2620\ufe0f"
tagline: Speaks like a swashbuckling sea captain
built_in: true
---

Adopt the communication style of a pirate.
`
	s, err := ParseStyle([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ID != "pirate" {
		t.Errorf("ID = %q, want %q", s.ID, "pirate")
	}
	if s.Name != "Pirate" {
		t.Errorf("Name = %q, want %q", s.Name, "Pirate")
	}
	if s.Tagline != "Speaks like a swashbuckling sea captain" {
		t.Errorf("Tagline = %q", s.Tagline)
	}
	if !s.BuiltIn {
		t.Errorf("BuiltIn = false, want true")
	}
	if s.Prompt != "Adopt the communication style of a pirate." {
		t.Errorf("Prompt = %q", s.Prompt)
	}
}

func TestParseMissingDelimiter(t *testing.T) {
	input := `id: pirate
name: Pirate
`
	_, err := ParseStyle([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing frontmatter delimiters")
	}
}

func TestParseEmptyBody(t *testing.T) {
	input := `---
id: test
name: Test
icon: "T"
tagline: Test style
---
`
	s, err := ParseStyle([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Prompt != "" {
		t.Errorf("Prompt = %q, want empty", s.Prompt)
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	original := &Style{
		ID: "test", Name: "Test", Icon: "T",
		Tagline: "A test", Prompt: "Do the thing.",
		BuiltIn: false,
	}
	data, err := MarshalStyle(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	parsed, err := ParseStyle(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if parsed.ID != original.ID || parsed.Name != original.Name || parsed.Prompt != original.Prompt || parsed.Tagline != original.Tagline {
		t.Errorf("roundtrip mismatch: got %+v", parsed)
	}
}
