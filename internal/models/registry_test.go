//go:build !nomodelregistry

package models

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testRegistryJSON = `{
	"anthropic": {
		"name": "Anthropic",
		"env": ["ANTHROPIC_API_KEY"],
		"models": {
			"claude-opus-4-6": {
				"id": "claude-opus-4-6",
				"name": "Claude Opus 4.6",
				"tool_call": true,
				"release_date": "2026-02-05",
				"modalities": {"input": ["text", "image"], "output": ["text"]},
				"limit": {"context": 1000000, "output": 128000},
				"cost": {"input": 5, "output": 25},
				"reasoning": true
			},
			"old-model": {
				"id": "old-model",
				"name": "Old Model",
				"tool_call": true,
				"release_date": "2023-01-01",
				"modalities": {"input": ["text"], "output": ["text"]}
			}
		}
	},
	"moonshotai": {
		"name": "Moonshot AI",
		"api": "https://api.moonshot.ai/v1",
		"env": ["MOONSHOT_API_KEY"],
		"models": {
			"kimi-k2-thinking": {
				"id": "kimi-k2-thinking",
				"name": "Kimi K2 Thinking",
				"tool_call": true,
				"release_date": "2025-11-06",
				"modalities": {"input": ["text"], "output": ["text"]},
				"limit": {"context": 262144, "output": 262144},
				"cost": {"input": 0.6, "output": 2.5}
			}
		}
	},
	"unknown_provider": {
		"models": {
			"some-model": {
				"id": "some-model",
				"name": "Some Model",
				"tool_call": true,
				"release_date": "2026-01-01",
				"modalities": {"input": ["text"], "output": ["text"]}
			}
		}
	},
	"anthropic_embed": {
		"models": {
			"embed-model": {
				"id": "embed-model",
				"name": "Embedding",
				"tool_call": false,
				"release_date": "2026-01-01",
				"modalities": {"input": ["text"], "output": ["text"]}
			}
		}
	}
}`

func TestParseRegistry(t *testing.T) {
	models, err := ParseRegistry([]byte(testRegistryJSON), time.Date(2025, 3, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	// Should include claude-opus-4-6 (recent, tool_call, text output, known provider)
	// Should include kimi-k2-thinking (recent, tool_call, text output, known provider)
	// Should exclude old-model (too old)
	// Should exclude some-model (unknown provider)
	// Should exclude embed-model (tool_call false)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	var claude, kimi *RegistryModel
	for i := range models {
		switch models[i].ID {
		case "claude-opus-4-6":
			claude = &models[i]
		case "kimi-k2-thinking":
			kimi = &models[i]
		}
	}

	if claude == nil {
		t.Fatal("claude-opus-4-6 not found")
	}
	if claude.DisplayName != "Claude Opus 4.6" {
		t.Errorf("wrong display name: %q", claude.DisplayName)
	}
	if claude.Provider != "anthropic" {
		t.Errorf("wrong provider: %q", claude.Provider)
	}
	if claude.ContextWindow != 1000000 {
		t.Errorf("wrong context window: %d", claude.ContextWindow)
	}
	if claude.CostInput != 5 {
		t.Errorf("wrong cost input: %f", claude.CostInput)
	}
	if !claude.Reasoning {
		t.Error("expected reasoning=true")
	}

	if kimi == nil {
		t.Fatal("kimi-k2-thinking not found")
	}
	if kimi.Provider != "moonshotai" {
		t.Errorf("wrong provider: %q", kimi.Provider)
	}
}

func TestParseRegistry_InvalidJSON(t *testing.T) {
	_, err := ParseRegistry([]byte("not json"), time.Now())
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseRegistry_MissingFields(t *testing.T) {
	// Model with no release_date should be skipped
	json := `{"anthropic": {"models": {"m1": {"id": "m1", "name": "M1", "tool_call": true, "modalities": {"output": ["text"]}}}}}`
	models, err := ParseRegistry([]byte(json), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models (missing release_date), got %d", len(models))
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"test":true}`)

	if err := SaveCache(dir, data); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	loaded, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if string(loaded) != string(data) {
		t.Errorf("got %q, want %q", loaded, data)
	}
}

func TestLoadCache_Missing(t *testing.T) {
	data, err := LoadCache(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Error("expected nil for missing cache")
	}
}

func TestLoadCache_Corrupt(t *testing.T) {
	dir := t.TempDir()
	cachePath := CachePath(dir)
	os.MkdirAll(filepath.Dir(cachePath), 0755)
	os.WriteFile(cachePath, []byte("not json"), 0644)

	data, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Error("expected nil for corrupt cache")
	}
}

func TestBuildDetectModels(t *testing.T) {
	registry := []RegistryModel{
		{
			ID:            "claude-opus-4-6",
			DisplayName:   "Claude Opus 4.6",
			Provider:      "anthropic",
			ContextWindow: 1000000,
			CostInput:     5,
			CostOutput:    25,
			Reasoning:     true,
			ReleaseDate:   "2026-02-05",
		},
		{
			ID:            "kimi-k2-thinking",
			DisplayName:   "Kimi K2 Thinking",
			Provider:      "moonshotai",
			ContextWindow: 262144,
			CostInput:     0.6,
			CostOutput:    2.5,
			ReleaseDate:   "2025-11-06",
		},
	}

	models := BuildDetectModels(registry)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	// Claude model
	claude := models[0]
	if claude.Provider != "anthropic" {
		t.Errorf("provider: got %q, want 'anthropic'", claude.Provider)
	}
	if claude.Category != "native" {
		t.Errorf("category: got %q, want 'native'", claude.Category)
	}
	// Should have claude + opencode runners
	claudeRunner, ok := claude.RunnerFor("claude")
	if !ok {
		t.Fatal("missing claude runner")
	}
	if claudeRunner.ModelValue != "claude-opus-4-6" {
		t.Errorf("claude ModelValue: got %q", claudeRunner.ModelValue)
	}
	ocRunner, ok := claude.RunnerFor("opencode")
	if !ok {
		t.Fatal("missing opencode runner")
	}
	if ocRunner.ModelValue != "anthropic/claude-opus-4-6" {
		t.Errorf("opencode ModelValue: got %q", ocRunner.ModelValue)
	}

	// Kimi model
	kimi := models[1]
	if kimi.Provider != "moonshot" {
		t.Errorf("provider: got %q, want 'moonshot'", kimi.Provider)
	}
	if kimi.Category != "third-party" {
		t.Errorf("category: got %q, want 'third-party'", kimi.Category)
	}
	kimiRunner, ok := kimi.RunnerFor("claude")
	if !ok {
		t.Fatal("missing claude runner for kimi")
	}
	if kimiRunner.Endpoint != "https://api.moonshot.ai/anthropic" {
		t.Errorf("endpoint: got %q", kimiRunner.Endpoint)
	}
	if len(kimiRunner.RequiredSecrets) != 1 || kimiRunner.RequiredSecrets[0] != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("secrets: got %v", kimiRunner.RequiredSecrets)
	}
	kimiOC, ok := kimi.RunnerFor("opencode")
	if !ok {
		t.Fatal("missing opencode runner for kimi")
	}
	if kimiOC.ModelValue != "moonshot/kimi-k2-thinking" {
		t.Errorf("opencode ModelValue: got %q", kimiOC.ModelValue)
	}
}

func TestDeduplicateModels_LatestDisplayName(t *testing.T) {
	// Models with "(latest)" in display name are floating aliases — should be removed
	models := []RegistryModel{
		{ID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5 (latest)", Provider: "anthropic"},
		{ID: "claude-opus-4-5-20251101", DisplayName: "Claude Opus 4.5", Provider: "anthropic"},
		{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", Provider: "anthropic"},
	}
	result := deduplicateModels(models)
	ids := make(map[string]bool)
	for _, m := range result {
		ids[m.ID] = true
	}
	if ids["claude-opus-4-5"] {
		t.Error("floating alias claude-opus-4-5 with '(latest)' display name should be removed")
	}
	if !ids["claude-opus-4-5-20251101"] {
		t.Error("pinned claude-opus-4-5-20251101 should be kept")
	}
	if !ids["claude-opus-4-6"] {
		t.Error("claude-opus-4-6 should be kept")
	}
}

func TestDeduplicateModels_DatedVariants(t *testing.T) {
	models := []RegistryModel{
		{ID: "claude-opus-4-1", Provider: "anthropic"},
		{ID: "claude-opus-4-1-20250805", Provider: "anthropic"},
		{ID: "claude-sonnet-4-0", Provider: "anthropic"},
		{ID: "claude-sonnet-4-20250514", Provider: "anthropic"},
	}
	result := deduplicateModels(models)
	ids := make(map[string]bool)
	for _, m := range result {
		ids[m.ID] = true
	}
	if ids["claude-opus-4-1-20250805"] {
		t.Error("dated variant claude-opus-4-1-20250805 should be deduped")
	}
	if !ids["claude-opus-4-1"] {
		t.Error("alias claude-opus-4-1 should be kept")
	}
	if ids["claude-sonnet-4-20250514"] {
		t.Error("dated variant claude-sonnet-4-20250514 should be deduped")
	}
	if !ids["claude-sonnet-4-0"] {
		t.Error("alias claude-sonnet-4-0 should be kept")
	}
}

func TestDeduplicateModels_LatestSuffix(t *testing.T) {
	models := []RegistryModel{
		{ID: "claude-3-5-haiku-20241022", Provider: "anthropic"},
		{ID: "claude-3-5-haiku-latest", Provider: "anthropic"},
	}
	result := deduplicateModels(models)
	ids := make(map[string]bool)
	for _, m := range result {
		ids[m.ID] = true
	}
	if ids["claude-3-5-haiku-latest"] {
		t.Error("-latest variant should be deduped when dated variant exists")
	}
	if !ids["claude-3-5-haiku-20241022"] {
		t.Error("dated variant should be kept")
	}
}

func TestDeduplicateModels_ChatLatest(t *testing.T) {
	models := []RegistryModel{
		{ID: "gpt-5.1", Provider: "openai"},
		{ID: "gpt-5.1-chat-latest", Provider: "openai"},
	}
	result := deduplicateModels(models)
	ids := make(map[string]bool)
	for _, m := range result {
		ids[m.ID] = true
	}
	if ids["gpt-5.1-chat-latest"] {
		t.Error("-chat-latest should be skipped via SkipIDPatterns")
	}
	if !ids["gpt-5.1"] {
		t.Error("base model should be kept")
	}
}

func TestDeduplicateModels_DifferentModelsNotDeduped(t *testing.T) {
	// Same release date, both released together — should NOT be deduped
	models := []RegistryModel{
		{ID: "claude-opus-4-0", Provider: "anthropic"},
		{ID: "claude-sonnet-4-0", Provider: "anthropic"},
	}
	result := deduplicateModels(models)
	if len(result) != 2 {
		t.Errorf("expected 2 models (different models, not aliases), got %d", len(result))
	}
}

func TestDeduplicateModels_PrefixOverlapNotDeduped(t *testing.T) {
	// claude-opus-4 and claude-opus-4-1 are DIFFERENT models, not aliases.
	// The -1 is a version number, not a dated suffix.
	models := []RegistryModel{
		{ID: "claude-opus-4", Provider: "anthropic"},
		{ID: "claude-opus-4-1", Provider: "anthropic"},
		{ID: "claude-opus-4-5", Provider: "anthropic"},
	}
	result := deduplicateModels(models)
	if len(result) != 3 {
		ids := make([]string, len(result))
		for i, m := range result {
			ids[i] = m.ID
		}
		t.Errorf("expected 3 models (different versions, not aliases), got %d: %v", len(result), ids)
	}
}

func TestDeduplicateModels_Dash0Convention(t *testing.T) {
	// claude-opus-4-20250514 should be deduped because claude-opus-4-0 exists
	models := []RegistryModel{
		{ID: "claude-opus-4-0", Provider: "anthropic"},
		{ID: "claude-opus-4-20250514", Provider: "anthropic"},
	}
	result := deduplicateModels(models)
	ids := make(map[string]bool)
	for _, m := range result {
		ids[m.ID] = true
	}
	if ids["claude-opus-4-20250514"] {
		t.Error("dated variant should be deduped when -0 alias exists")
	}
	if !ids["claude-opus-4-0"] {
		t.Error("-0 alias should be kept")
	}
}
