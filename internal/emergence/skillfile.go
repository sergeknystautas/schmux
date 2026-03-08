package emergence

import (
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// GenerateSkillFile produces a markdown skill file with YAML frontmatter from a proposal.
func GenerateSkillFile(proposal contracts.SkillProposal) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s\n", proposal.Name)
	fmt.Fprintf(&sb, "description: %s\n", proposal.Description)
	sb.WriteString("source: emerged\n")
	if len(proposal.Triggers) > 0 {
		sb.WriteString("triggers:\n")
		for _, t := range proposal.Triggers {
			fmt.Fprintf(&sb, "  - %q\n", t)
		}
	}
	sb.WriteString("---\n\n")
	if proposal.Procedure != "" {
		sb.WriteString("## Procedure\n\n")
		sb.WriteString(proposal.Procedure)
		sb.WriteString("\n\n")
	}
	if proposal.QualityCriteria != "" {
		sb.WriteString("## Quality Criteria\n\n")
		sb.WriteString(proposal.QualityCriteria)
		sb.WriteString("\n")
	}
	return sb.String()
}
