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
