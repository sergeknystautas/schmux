package persona

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	input := `---
id: security-auditor
name: Security Auditor
icon: "\U0001f512"
color: "#e74c3c"
expectations: |
  Produce a structured report.
built_in: true
---

You are a security expert.
Check for vulnerabilities.
`
	p, err := ParsePersona([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != "security-auditor" {
		t.Errorf("ID = %q, want %q", p.ID, "security-auditor")
	}
	if p.Name != "Security Auditor" {
		t.Errorf("Name = %q, want %q", p.Name, "Security Auditor")
	}
	if p.Color != "#e74c3c" {
		t.Errorf("Color = %q, want %q", p.Color, "#e74c3c")
	}
	if p.BuiltIn != true {
		t.Errorf("BuiltIn = %v, want true", p.BuiltIn)
	}
	expectedPrompt := "You are a security expert.\nCheck for vulnerabilities."
	if p.Prompt != expectedPrompt {
		t.Errorf("Prompt = %q, want %q", p.Prompt, expectedPrompt)
	}
}

func TestParseFrontmatterMissingDelimiter(t *testing.T) {
	input := `id: security-auditor
name: Security Auditor
`
	_, err := ParsePersona([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing frontmatter delimiters")
	}
}

func TestParseFrontmatterEmptyBody(t *testing.T) {
	input := `---
id: test
name: Test
icon: "T"
color: "#000"
---
`
	p, err := ParsePersona([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Prompt != "" {
		t.Errorf("Prompt = %q, want empty", p.Prompt)
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	original := &Persona{
		ID: "test", Name: "Test", Icon: "T",
		Color: "#000", Prompt: "Do the thing.",
		Expectations: "Report format.", BuiltIn: false,
	}
	data, err := MarshalPersona(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	parsed, err := ParsePersona(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if parsed.ID != original.ID || parsed.Name != original.Name || parsed.Prompt != original.Prompt {
		t.Errorf("roundtrip mismatch: got %+v", parsed)
	}
}
