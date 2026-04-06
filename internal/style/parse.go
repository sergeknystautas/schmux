package style

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Style represents a communication style for an agent.
type Style struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Icon    string `yaml:"icon" json:"icon"`
	Tagline string `yaml:"tagline" json:"tagline"`
	BuiltIn bool   `yaml:"built_in" json:"built_in"`
	Prompt  string `yaml:"-" json:"prompt"` // from body, not frontmatter
}

// ParseStyle parses a YAML file with frontmatter metadata and a body prompt.
func ParseStyle(data []byte) (*Style, error) {
	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing opening frontmatter delimiter")
	}

	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
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

	var s Style
	if err := yaml.Unmarshal([]byte(frontmatter), &s); err != nil {
		return nil, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	s.Prompt = body
	return &s, nil
}

// MarshalStyle serializes a Style to YAML frontmatter + body format.
func MarshalStyle(s *Style) ([]byte, error) {
	meta := struct {
		ID      string `yaml:"id"`
		Name    string `yaml:"name"`
		Icon    string `yaml:"icon"`
		Tagline string `yaml:"tagline"`
		BuiltIn bool   `yaml:"built_in"`
	}{
		ID: s.ID, Name: s.Name, Icon: s.Icon,
		Tagline: s.Tagline, BuiltIn: s.BuiltIn,
	}

	fm, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n")
	if s.Prompt != "" {
		buf.WriteString("\n")
		buf.WriteString(s.Prompt)
		buf.WriteString("\n")
	}
	return []byte(buf.String()), nil
}
