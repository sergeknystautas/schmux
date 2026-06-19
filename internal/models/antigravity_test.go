package models

import (
	"context"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

// realAgyModelsOutput is the verified output of `agy models` (v1.0.9).
const realAgyModelsOutput = `Gemini 3.5 Flash (Medium)
Gemini 3.5 Flash (High)
Gemini 3.5 Flash (Low)
Gemini 3.1 Pro (Low)
Gemini 3.1 Pro (High)
Claude Sonnet 4.6 (Thinking)
Claude Opus 4.6 (Thinking)
GPT-OSS 120B (Medium)
`

func TestParseAntigravityModels(t *testing.T) {
	models := parseAntigravityModels([]byte(realAgyModelsOutput))
	if len(models) != 8 {
		t.Fatalf("expected 8 models, got %d: %+v", len(models), models)
	}

	// Spot-check each: ID (slug+prefix), DisplayName (exact), Provider (prefix),
	// and a single antigravity runner whose ModelValue is the exact display
	// string with no secrets/endpoint.
	cases := []struct {
		display  string
		id       string
		provider string
	}{
		{"Gemini 3.5 Flash (Medium)", "antigravity-gemini-3-5-flash-medium", "google"},
		{"Gemini 3.5 Flash (Low)", "antigravity-gemini-3-5-flash-low", "google"},
		{"Gemini 3.1 Pro (High)", "antigravity-gemini-3-1-pro-high", "google"},
		{"Claude Sonnet 4.6 (Thinking)", "antigravity-claude-sonnet-4-6-thinking", "anthropic"},
		{"Claude Opus 4.6 (Thinking)", "antigravity-claude-opus-4-6-thinking", "anthropic"},
		{"GPT-OSS 120B (Medium)", "antigravity-gpt-oss-120b-medium", "openai"},
	}
	byID := map[string]detect.Model{}
	for _, m := range models {
		byID[m.ID] = m
	}
	for _, c := range cases {
		m, ok := byID[c.id]
		if !ok {
			t.Errorf("missing model %q (for %q)", c.id, c.display)
			continue
		}
		if m.DisplayName != c.display {
			t.Errorf("%q DisplayName = %q, want %q", c.id, m.DisplayName, c.display)
		}
		if m.Provider != c.provider {
			t.Errorf("%q Provider = %q, want %q", c.id, m.Provider, c.provider)
		}
		spec, ok := m.RunnerFor("antigravity")
		if !ok {
			t.Errorf("%q: missing antigravity runner", c.id)
			continue
		}
		if spec.ModelValue != c.display {
			t.Errorf("%q runner ModelValue = %q, want exact display %q", c.id, spec.ModelValue, c.display)
		}
		if spec.Endpoint != "" {
			t.Errorf("%q runner Endpoint = %q, want empty", c.id, spec.Endpoint)
		}
		if len(spec.RequiredSecrets) != 0 {
			t.Errorf("%q runner RequiredSecrets = %v, want empty", c.id, spec.RequiredSecrets)
		}
		if len(m.Runners) != 1 {
			t.Errorf("%q: expected exactly 1 runner, got %d (%v)", c.id, len(m.Runners), m.Runners)
		}
	}

	// IDs must all carry the antigravity- prefix (no collision with registry/user/default IDs).
	for _, m := range models {
		if len(m.ID) < len("antigravity-") || m.ID[:len("antigravity-")] != "antigravity-" {
			t.Errorf("model ID %q missing antigravity- prefix", m.ID)
		}
	}
}

func TestParseAntigravityModels_EmptyAndMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"whitespace":     "   \n\n  \t\n",
		"sign-in prompt": "Error: not signed in. Run `agy auth login`.\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if got := parseAntigravityModels([]byte(in)); len(got) != 0 {
				t.Errorf("parseAntigravityModels(%q) = %d models, want 0", in, len(got))
			}
		})
	}
}

func TestParseAntigravityModels_BlankLinesSkipped(t *testing.T) {
	in := []byte("\nClaude Opus 4.6 (Thinking)\n\n  \nGemini 3.5 Flash (Low)\n")
	got := parseAntigravityModels(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 models (blank lines skipped), got %d: %+v", len(got), got)
	}
}

func TestAntigravityModelsCatalog_PresentWhenDetected(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "antigravity", Command: "agy"}}, "", testLogger)
	mm.SetAntigravityModels(parseAntigravityModels([]byte(realAgyModelsOutput)))

	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	ids := map[string]bool{}
	for _, m := range catalog.Models {
		ids[m.ID] = true
	}
	if !ids["antigravity-claude-opus-4-6-thinking"] {
		t.Errorf("antigravity model missing from catalog when agy is detected; have %v", ids)
	}
}

func TestSetAntigravityModels_ChangeDetection(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "antigravity", Command: "agy"}}, "", testLogger)
	models := parseAntigravityModels([]byte(realAgyModelsOutput))

	if !mm.SetAntigravityModels(models) {
		t.Error("first set (nil -> models) should report changed")
	}
	if mm.SetAntigravityModels(models) {
		t.Error("identical re-set should report unchanged (suppresses redundant broadcast)")
	}
	if !mm.SetAntigravityModels(models[:len(models)-1]) {
		t.Error("removing a model should report changed")
	}
	if !mm.SetAntigravityModels(nil) {
		t.Error("clearing a non-empty layer should report changed (so the clear is broadcast)")
	}
}

func TestFetchAntigravityModels_KeepsStaleOnError(t *testing.T) {
	mm := New(&config.Config{}, []detect.Tool{{Name: "antigravity", Command: "agy"}}, "", testLogger)
	mm.SetAntigravityModels(parseAntigravityModels([]byte(realAgyModelsOutput)))

	// A failing `agy models` (here `false models`, which exits non-zero) must not
	// wipe the previously discovered layer — only a successful empty result clears.
	mm.fetchAntigravityModels(context.Background(), "false")

	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	found := false
	for _, m := range catalog.Models {
		if m.ID == "antigravity-claude-opus-4-6-thinking" {
			found = true
		}
	}
	if !found {
		t.Error("antigravity models were wiped on a failed discovery; want them retained")
	}
}

func TestAntigravityModelsCatalog_AbsentWhenNotDetected(t *testing.T) {
	// agy NOT detected (only claude). Discovered models have only an antigravity
	// runner, so GetCatalog must exclude them (no available runner).
	mm := New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", testLogger)
	mm.SetAntigravityModels(parseAntigravityModels([]byte(realAgyModelsOutput)))

	catalog, err := mm.GetCatalog()
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	for _, m := range catalog.Models {
		if len(m.ID) >= len("antigravity-") && m.ID[:len("antigravity-")] == "antigravity-" {
			if m.ID == "antigravity" {
				continue // the bare default entry is also excluded (no detected runner), but be safe
			}
			t.Errorf("antigravity model %q should be absent when agy not detected", m.ID)
		}
	}
}
