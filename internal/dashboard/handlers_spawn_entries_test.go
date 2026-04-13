package dashboard

import "testing"

func TestExtractSkillDescription(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "valid frontmatter",
			content: "---\nname: fix-tests\ndescription: Fix failing tests in the codebase\nsource: emerged\n---\n\n## Procedure\n",
			want:    "Fix failing tests in the codebase",
		},
		{
			name:    "no frontmatter",
			content: "Just a regular file",
			want:    "",
		},
		{
			name:    "no description field",
			content: "---\nname: something\nsource: emerged\n---\n",
			want:    "",
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSkillDescription(tt.content)
			if got != tt.want {
				t.Errorf("extractSkillDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}
