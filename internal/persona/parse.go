package persona

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Persona represents a behavioral profile for an agent.
type Persona struct {
	ID           string `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Icon         string `yaml:"icon" json:"icon"`
	Color        string `yaml:"color" json:"color"`
	Expectations string `yaml:"expectations" json:"expectations"`
	BuiltIn      bool   `yaml:"built_in" json:"built_in"`
	Prompt       string `yaml:"-" json:"prompt"` // from body, not frontmatter
}

// ParsePersona parses a YAML file with frontmatter metadata and a body prompt.
func ParsePersona(data []byte) (*Persona, error) {
	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing opening frontmatter delimiter")
	}

	// Find closing delimiter
	rest := content[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		// Check for closing delimiter at end of file
		if strings.HasSuffix(rest, "\n---") {
			idx = len(rest) - 4
		} else if strings.HasSuffix(rest, "\n---\n") {
			idx = len(rest) - 5
		} else {
			return nil, fmt.Errorf("missing closing frontmatter delimiter")
		}
	}

	frontmatter := rest[:idx]
	body := ""
	if idx+4 < len(rest) {
		body = strings.TrimSpace(rest[idx+4:])
	}

	var p Persona
	if err := yaml.Unmarshal([]byte(frontmatter), &p); err != nil {
		return nil, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	p.Prompt = body
	return &p, nil
}

// MarshalPersona serializes a Persona to YAML frontmatter + body format.
func MarshalPersona(p *Persona) ([]byte, error) {
	meta := struct {
		ID           string `yaml:"id"`
		Name         string `yaml:"name"`
		Icon         string `yaml:"icon"`
		Color        string `yaml:"color"`
		Expectations string `yaml:"expectations,omitempty"`
		BuiltIn      bool   `yaml:"built_in"`
	}{
		ID: p.ID, Name: p.Name, Icon: p.Icon,
		Color: p.Color, Expectations: p.Expectations, BuiltIn: p.BuiltIn,
	}

	fm, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n")
	if p.Prompt != "" {
		buf.WriteString("\n")
		buf.WriteString(p.Prompt)
		buf.WriteString("\n")
	}
	return []byte(buf.String()), nil
}
