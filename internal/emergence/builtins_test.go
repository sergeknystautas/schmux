package emergence

import (
	"strings"
	"testing"
)

func TestListBuiltins(t *testing.T) {
	skills, err := ListBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) == 0 {
		t.Fatal("expected at least one built-in skill")
	}
	// Verify commit skill exists and has content
	found := false
	for _, s := range skills {
		if s.Name == "commit" {
			found = true
			if len(s.Content) < 100 {
				t.Error("commit skill content too short")
			}
			if !strings.Contains(s.Content, "Definition of Done") {
				t.Error("commit skill should contain Definition of Done")
			}
			if !strings.Contains(s.Content, "source: built-in") {
				t.Error("commit skill should have source: built-in in frontmatter")
			}
		}
	}
	if !found {
		t.Error("commit skill not found in builtins")
	}
}
