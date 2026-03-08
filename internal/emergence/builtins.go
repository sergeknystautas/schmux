package emergence

import (
	"embed"
	"path/filepath"
	"strings"
)

//go:embed builtins/*.md
var builtinFS embed.FS

// BuiltinSkill describes an embedded skill.
type BuiltinSkill struct {
	Name    string
	Content string
}

// ListBuiltins returns all embedded built-in skills.
func ListBuiltins() ([]BuiltinSkill, error) {
	entries, err := builtinFS.ReadDir("builtins")
	if err != nil {
		return nil, err
	}
	var skills []BuiltinSkill
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := builtinFS.ReadFile(filepath.Join("builtins", entry.Name()))
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		skills = append(skills, BuiltinSkill{
			Name:    name,
			Content: string(data),
		})
	}
	return skills, nil
}
