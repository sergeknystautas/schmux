//go:build !nomodelregistry

package models

import (
	"reflect"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestZaiModelsExposeCodexRunner(t *testing.T) {
	models := BuildDetectModels([]RegistryModel{
		{ID: "glm-5.3", DisplayName: "GLM-5.3", Provider: "zai-coding-plan",
			ContextWindow: 1000000, Description: "Flagship GLM model",
			InputModalities: []string{"text"}},
	})
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	m := models[0]
	runners := detect.SortedRunnerKeys(m.Runners)
	want := []string{"claude", "codex", "opencode"}
	if !reflect.DeepEqual(runners, want) {
		t.Fatalf("runners = %v, want %v", runners, want)
	}
	spec, ok := m.Runners["codex"]
	if !ok {
		t.Fatal("no codex runner")
	}
	if spec.Endpoint != "https://api.z.ai/api/v1" {
		t.Fatalf("codex endpoint = %q", spec.Endpoint)
	}
	if spec.ModelValue != "glm-5.3" {
		t.Fatalf("codex model value = %q", spec.ModelValue)
	}
	if !reflect.DeepEqual(spec.RequiredSecrets, []string{"ANTHROPIC_AUTH_TOKEN"}) {
		t.Fatalf("codex secrets = %v", spec.RequiredSecrets)
	}
}

func TestNativeModelsUnchanged(t *testing.T) {
	models := BuildDetectModels([]RegistryModel{
		{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", Provider: "anthropic"},
	})
	m := models[0]
	if runners := detect.SortedRunnerKeys(m.Runners); !reflect.DeepEqual(runners, []string{"claude", "opencode"}) {
		t.Fatalf("anthropic runners = %v, want [claude opencode]", runners)
	}
}

// Every third-party provider with a codex ExtraRunner exposes it with the
// plan's Responses endpoint, and the stored plan secret (ANTHROPIC_AUTH_TOKEN)
// satisfies it — one key per provider, claude and codex alike.
func TestThirdPartyCodexRunners(t *testing.T) {
	tests := []struct {
		provider          string
		modelID           string
		endpoint          string
		canonicalProvider string
	}{
		{"zai-coding-plan", "glm-5.3", "https://api.z.ai/api/v1", "zai"},
		{"kimi-code-plan-cn", "k3", "https://api.kimi.com/coding/v1", "moonshot"},
		{"minimax", "MiniMax-M3", "https://api.minimax.io/v1", "minimax"},
	}
	for _, tt := range tests {
		profile, ok := GetProviderProfile(tt.provider)
		if !ok {
			t.Fatalf("%s: no profile", tt.provider)
		}
		// The canonical provider names the catalog file and the codex
		// model_providers config key.
		if cp := profile.CanonicalProvider(); cp != tt.canonicalProvider {
			t.Fatalf("%s: canonical provider = %q, want %q", tt.provider, cp, tt.canonicalProvider)
		}
		models := BuildDetectModels([]RegistryModel{
			{ID: tt.modelID, DisplayName: tt.modelID, Provider: tt.provider, ContextWindow: 1000000},
		})
		if len(models) != 1 {
			t.Fatalf("%s: models = %d, want 1", tt.provider, len(models))
		}
		spec, ok := models[0].Runners["codex"]
		if !ok {
			t.Fatalf("%s: no codex runner", tt.provider)
		}
		if spec.Endpoint != tt.endpoint {
			t.Errorf("%s: codex endpoint = %q, want %q", tt.provider, spec.Endpoint, tt.endpoint)
		}
		if spec.ModelValue != tt.modelID {
			t.Errorf("%s: codex model value = %q, want %q", tt.provider, spec.ModelValue, tt.modelID)
		}
		if !reflect.DeepEqual(spec.RequiredSecrets, []string{"ANTHROPIC_AUTH_TOKEN"}) {
			t.Errorf("%s: codex secrets = %v", tt.provider, spec.RequiredSecrets)
		}
		// Default resolution stays claude (claude < codex < opencode).
		if runners := detect.SortedRunnerKeys(models[0].Runners); !reflect.DeepEqual(runners, []string{"claude", "codex", "opencode"}) {
			t.Errorf("%s: runners = %v, want [claude codex opencode]", tt.provider, runners)
		}
	}
}

func TestParseRegistryCarriesDescriptionAndModalities(t *testing.T) {
	data := []byte(`{"zai-coding-plan":{"name":"Z.AI Coding Plan","api":"https://api.z.ai/api/coding/paas/v4","models":{
		"glm-5.3":{"id":"glm-5.3","name":"GLM-5.3","tool_call":true,"release_date":"2026-08-01",
		"modalities":{"input":["text"],"output":["text"]},"limit":{"context":1000000,"output":131072},
		"cost":{"input":0.6,"output":2.2},"description":"Flagship GLM model"}}}}`)
	models, err := ParseRegistry(data, time.Now().AddDate(-1, 0, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	if models[0].Description != "Flagship GLM model" {
		t.Fatalf("description = %q", models[0].Description)
	}
	if !reflect.DeepEqual(models[0].InputModalities, []string{"text"}) {
		t.Fatalf("input modalities = %v", models[0].InputModalities)
	}
}
