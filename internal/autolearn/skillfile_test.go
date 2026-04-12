//go:build !noautolearn

package autolearn

import (
	"strings"
	"testing"
)

func TestGenerateSkillFile(t *testing.T) {
	learning := Learning{
		Kind:        KindSkill,
		Title:       "code-review",
		Description: "Review pull requests for code quality",
		Skill: &SkillDetails{
			Triggers:        []string{"review this PR", "check this code"},
			Procedure:       "1. Read the diff\n2. Check for bugs\n3. Leave comments",
			QualityCriteria: "All critical issues flagged, no false positives",
		},
	}

	content := GenerateSkillFile(learning)

	// Check frontmatter
	if !strings.HasPrefix(content, "---\n") {
		t.Error("should start with YAML frontmatter")
	}
	if !strings.Contains(content, "name: code-review") {
		t.Error("should contain name in frontmatter")
	}
	if !strings.Contains(content, "description: Review pull requests for code quality") {
		t.Error("should contain description in frontmatter")
	}
	if !strings.Contains(content, "source: emerged") {
		t.Error("should contain source: emerged in frontmatter")
	}
	if !strings.Contains(content, "review this PR") {
		t.Error("should contain triggers")
	}

	// Check body
	if !strings.Contains(content, "## Procedure") {
		t.Error("should contain Procedure section")
	}
	if !strings.Contains(content, "1. Read the diff") {
		t.Error("should contain procedure content")
	}
	if !strings.Contains(content, "## Quality Criteria") {
		t.Error("should contain Quality Criteria section")
	}
	if !strings.Contains(content, "All critical issues flagged") {
		t.Error("should contain quality criteria content")
	}
}

func TestGenerateSkillFile_EmptyOptionals(t *testing.T) {
	learning := Learning{
		Kind:        KindSkill,
		Title:       "simple",
		Description: "A simple skill",
		Skill:       &SkillDetails{},
	}

	content := GenerateSkillFile(learning)

	if !strings.Contains(content, "name: simple") {
		t.Error("should contain name")
	}
	if strings.Contains(content, "## Procedure") {
		t.Error("should not contain Procedure section when empty")
	}
	if strings.Contains(content, "## Quality Criteria") {
		t.Error("should not contain Quality Criteria section when empty")
	}
	if strings.Contains(content, "triggers:") {
		t.Error("should not contain triggers when empty")
	}
}
