//go:build !noautolearn

package autolearn

import (
	"fmt"
	"strings"
)

// GenerateSkillFile produces a markdown skill file with YAML frontmatter from a Learning.
// The learning must be of KindSkill with a non-nil Skill field.
func GenerateSkillFile(learning Learning) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s\n", learning.Title)
	fmt.Fprintf(&sb, "description: %s\n", learning.Description)
	sb.WriteString("source: emerged\n")
	if learning.Skill != nil && len(learning.Skill.Triggers) > 0 {
		sb.WriteString("triggers:\n")
		for _, t := range learning.Skill.Triggers {
			fmt.Fprintf(&sb, "  - %q\n", t)
		}
	}
	sb.WriteString("---\n\n")
	if learning.Skill != nil {
		if learning.Skill.Procedure != "" {
			sb.WriteString("## Procedure\n\n")
			sb.WriteString(learning.Skill.Procedure)
			sb.WriteString("\n\n")
		}
		if learning.Skill.QualityCriteria != "" {
			sb.WriteString("## Quality Criteria\n\n")
			sb.WriteString(learning.Skill.QualityCriteria)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
