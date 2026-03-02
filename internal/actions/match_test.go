package actions

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestMatchPrompt_ExactMatch(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors in src/",
	})

	id, edited := r.MatchPrompt("Fix lint errors in src/")
	if id != action.ID {
		t.Errorf("id = %q, want %q", id, action.ID)
	}
	if edited {
		t.Error("expected edited=false for exact match")
	}
}

func TestMatchPrompt_ExactMatchCaseInsensitive(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors in src/",
	})

	id, edited := r.MatchPrompt("fix lint errors in src/")
	if id != action.ID {
		t.Errorf("id = %q, want %q", id, action.ID)
	}
	if edited {
		t.Error("expected edited=false for case-insensitive exact match")
	}
}

func TestMatchPrompt_PrefixMatchWithDifferentParam(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors in {{path}}",
		Parameters: []contracts.ActionParameter{
			{Name: "path", Default: "src/"},
		},
	})

	id, edited := r.MatchPrompt("Fix lint errors in lib/")
	if id != action.ID {
		t.Errorf("id = %q, want %q", id, action.ID)
	}
	if !edited {
		t.Error("expected edited=true when parameter differs from default")
	}
}

func TestMatchPrompt_PrefixMatchWithDefault(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors in {{path}}",
		Parameters: []contracts.ActionParameter{
			{Name: "path", Default: "src/"},
		},
	})

	id, edited := r.MatchPrompt("Fix lint errors in src/")
	if id != action.ID {
		t.Errorf("id = %q, want %q", id, action.ID)
	}
	if edited {
		t.Error("expected edited=false when prompt matches template with defaults")
	}
}

func TestMatchPrompt_NoMatch(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors in {{path}}",
	})

	id, _ := r.MatchPrompt("Run all tests")
	if id != "" {
		t.Errorf("expected empty id for no match, got %q", id)
	}
}

func TestMatchPrompt_LongestPrefixWins(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	r.Create(contracts.CreateActionRequest{
		Name:     "Fix",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix {{thing}}",
	})
	specific, _ := r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors in {{path}}",
	})

	id, _ := r.MatchPrompt("Fix lint errors in src/")
	if id != specific.ID {
		t.Errorf("expected longest prefix match (specific action), got %q", id)
	}
}

func TestMatchPrompt_CommandActionsNeverMatch(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	r.Create(contracts.CreateActionRequest{
		Name:    "Build",
		Type:    contracts.ActionTypeCommand,
		Command: "go build ./...",
	})

	id, _ := r.MatchPrompt("go build ./...")
	if id != "" {
		t.Errorf("command actions should never match, got %q", id)
	}
}

func TestMatchPrompt_EmptyTemplate(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	r.Create(contracts.CreateActionRequest{
		Name:     "Empty",
		Type:     contracts.ActionTypeAgent,
		Template: "",
	})

	id, _ := r.MatchPrompt("anything")
	if id != "" {
		t.Errorf("empty template should never match, got %q", id)
	}
}

func TestMatchPrompt_EmptyPrompt(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors",
	})

	id, _ := r.MatchPrompt("")
	if id != "" {
		t.Errorf("empty prompt should never match, got %q", id)
	}
}

func TestMatchPrompt_DismissedActionsIgnored(t *testing.T) {
	r := newTestRegistry(t)
	r.Load()

	action, _ := r.Create(contracts.CreateActionRequest{
		Name:     "Fix lint",
		Type:     contracts.ActionTypeAgent,
		Template: "Fix lint errors",
	})
	r.Dismiss(action.ID)

	id, _ := r.MatchPrompt("Fix lint errors")
	if id != "" {
		t.Errorf("dismissed actions should not match, got %q", id)
	}
}

func TestExtractStaticPrefix(t *testing.T) {
	tests := []struct {
		template string
		want     string
	}{
		{"Fix lint errors in {{path}}", "Fix lint errors in "},
		{"Fix lint errors", "Fix lint errors"},
		{"{{path}} needs fixing", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractStaticPrefix(tt.template)
		if got != tt.want {
			t.Errorf("extractStaticPrefix(%q) = %q, want %q", tt.template, got, tt.want)
		}
	}
}

func TestFillDefaults(t *testing.T) {
	tests := []struct {
		template string
		params   []contracts.ActionParameter
		want     string
	}{
		{
			"Fix lint in {{path}}",
			[]contracts.ActionParameter{{Name: "path", Default: "src/"}},
			"Fix lint in src/",
		},
		{
			"Fix {{what}} in {{path}}",
			[]contracts.ActionParameter{
				{Name: "what", Default: "lint"},
				{Name: "path", Default: "src/"},
			},
			"Fix lint in src/",
		},
		{
			"Fix lint in {{path}}",
			[]contracts.ActionParameter{{Name: "path"}}, // no default
			"Fix lint in {{path}}",
		},
		{
			"No params here",
			nil,
			"No params here",
		},
	}

	for _, tt := range tests {
		got := fillDefaults(tt.template, tt.params)
		if got != tt.want {
			t.Errorf("fillDefaults(%q, ...) = %q, want %q", tt.template, got, tt.want)
		}
	}
}
