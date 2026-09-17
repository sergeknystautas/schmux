package models

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func codexFixtureModels() []RegistryModel {
	return []RegistryModel{
		{ID: "glm-5.3", DisplayName: "GLM-5.3", Provider: "zai-coding-plan",
			ContextWindow: 1000000, Description: "Flagship GLM model",
			InputModalities: []string{"text"}},
		{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", Provider: "zai-coding-plan",
			ContextWindow: 1000000, InputModalities: []string{"text"}},
	}
}

func TestCodexCatalogPath(t *testing.T) {
	got := CodexCatalogPath("/srv/.schmux", "zai")
	want := "/srv/.schmux/cache/codex-models-zai.json"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestBuildCodexCatalogGolden(t *testing.T) {
	dump, err := os.ReadFile("testdata/codex-dump.json")
	if err != nil {
		t.Fatal(err)
	}
	bi := ExtractBaseInstructions(dump)
	if !strings.Contains(bi, "You are Codex") {
		t.Fatalf("base instructions not lifted: %q", bi)
	}
	doc := BuildCodexCatalog(codexFixtureModels(), bi)
	if len(doc.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(doc.Models))
	}
	first := doc.Models[0]
	if first.Slug != "glm-5.3" || first.DisplayName != "GLM-5.3" {
		t.Fatalf("slug/display = %q/%q", first.Slug, first.DisplayName)
	}
	if first.ContextWindow != 1000000 || first.MaxContextWindow != 1000000 {
		t.Fatalf("context window = %d/%d", first.ContextWindow, first.MaxContextWindow)
	}
	if !reflect.DeepEqual(first.InputModalities, []string{"text"}) {
		t.Fatalf("modalities = %v", first.InputModalities)
	}
	if first.BaseInstructions != bi {
		t.Fatal("base instructions not embedded")
	}
	if first.DefaultReasoningLevel != "high" || len(first.SupportedReasoningLevels) != 3 {
		t.Fatalf("reasoning = %q/%d", first.DefaultReasoningLevel, len(first.SupportedReasoningLevels))
	}
	// Second model with no description gets a synthesized one.
	if doc.Models[1].Description == "" {
		t.Fatal("missing description not synthesized")
	}
	// Golden: stable JSON shape (spot-check one constant and the wrapper).
	raw, _ := json.Marshal(doc)
	if !strings.Contains(string(raw), `"effective_context_window_percent":95`) {
		t.Fatal("constant missing from output")
	}
}

func TestBuildCodexCatalogSkipsWindowless(t *testing.T) {
	in := append(codexFixtureModels(), RegistryModel{ID: "glm-x", Provider: "zai-coding-plan"})
	if doc := BuildCodexCatalog(in, "bi"); len(doc.Models) != 2 {
		t.Fatalf("models = %d, want 2 (windowless skipped)", len(doc.Models))
	}
}

func TestBuildCodexCatalogFiltersModalities(t *testing.T) {
	// glm-5.3-flash on models.dev declares text,image,video,pdf; codex's
	// input_modalities enum is text|image|audio and rejects the whole
	// catalog on any other value (observed: launch failure "unknown
	// variant `video`"). Out-of-enum values must be dropped.
	doc := BuildCodexCatalog([]RegistryModel{
		{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", Provider: "zai-coding-plan",
			ContextWindow: 1000000, InputModalities: []string{"text", "image", "video", "pdf"}},
		{ID: "glm-audio", DisplayName: "GLM Audio", Provider: "zai-coding-plan",
			ContextWindow: 1000000, InputModalities: []string{"audio", "video"}},
		{ID: "glm-video-only", DisplayName: "GLM Video", Provider: "zai-coding-plan",
			ContextWindow: 1000000, InputModalities: []string{"video"}},
	}, "bi")
	want := [][]string{
		{"text", "image"},
		{"audio"},
		{"text"}, // everything filtered out falls back to text
	}
	for i, w := range want {
		if got := doc.Models[i].InputModalities; !reflect.DeepEqual(got, w) {
			t.Errorf("model %d modalities = %v, want %v", i, got, w)
		}
	}
}

func TestExtractBaseInstructionsFallback(t *testing.T) {
	if got := ExtractBaseInstructions([]byte(`{"models":[{"slug":"x"}]}`)); got != "" {
		t.Fatalf("fallback = %q, want empty", got)
	}
}

func TestWithoutCatalogArgs(t *testing.T) {
	in := []string{"-c", "model_provider=zai", "-c", "model_catalog_json=/x/y.json", "extra"}
	want := []string{"-c", "model_provider=zai", "extra"}
	if got := WithoutCatalogArgs(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := WithoutCatalogArgs(want); !reflect.DeepEqual(got, want) {
		t.Fatalf("idempotence: got %v", got)
	}
}

func TestCatalogArgPathFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"absent", []string{"-c", "model_provider=zai"}, ""},
		{"present", []string{"-c", "model_provider=zai", "-c", "model_catalog_json=/x/y.json"}, "/x/y.json"},
	}
	for _, tt := range tests {
		if got := CatalogArgPath(tt.args); got != tt.want {
			t.Errorf("%s: CatalogArgPath = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestReasoningEffortFromEnv(t *testing.T) {
	tests := []struct {
		name, env, want string
	}{
		{"unset", "", "high"},
		{"declared level", "low", "low"},
		{"declared level max", "max", "max"},
		{"undeclared value falls back", "ultra", "high"},
		{"whitespace-padded", " low ", "low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ReasoningEffortEnvVar, tt.env)
			if got := ReasoningEffortFromEnv(); got != tt.want {
				t.Fatalf("ReasoningEffortFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildCodexCatalogEffortFromEnv(t *testing.T) {
	t.Setenv(ReasoningEffortEnvVar, "low")
	doc := BuildCodexCatalog(codexFixtureModels(), "base")
	if len(doc.Models) == 0 {
		t.Fatal("no models")
	}
	if doc.Models[0].DefaultReasoningLevel != "low" {
		t.Fatalf("default_reasoning_level = %q, want low", doc.Models[0].DefaultReasoningLevel)
	}
}

func TestRegenerateCodexCatalogsWritesFile(t *testing.T) {
	dir := t.TempDir()
	dump, err := os.ReadFile("testdata/codex-dump.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cache", "codex-models-zai.json")
	if err := regenerateCodexCatalogsWith(dir, dump, codexFixtureModels()); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("catalog not written: %v", err)
	}
	if !strings.Contains(string(raw), `"slug":"glm-5.3"`) {
		t.Fatal("catalog missing glm-5.3")
	}
}

func TestStartBackgroundFetchRewritesStaleCodexCatalog(t *testing.T) {
	dir := t.TempDir()
	dump, err := os.ReadFile("testdata/codex-dump.json")
	if err != nil {
		t.Fatal(err)
	}
	dumpPath := filepath.Join(dir, "codex-dump.json")
	if err := os.WriteFile(dumpPath, dump, 0o600); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(dir, "codex")
	script := "#!/bin/sh\ncat " + dumpPath + "\n"
	if err := os.WriteFile(codexPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	registry := []byte(`{"zai-coding-plan":{"models":{"glm-5.3-flash":{
		"id":"glm-5.3-flash","name":"GLM-5.3 Flash","tool_call":true,
		"release_date":"2999-01-01","modalities":{"input":["text","image","video","pdf"],"output":["text"]},
		"limit":{"context":1000000,"output":32768}}}}}`)
	if err := SaveCache(dir, registry); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, "cache")
	catalogPath := CodexCatalogPath(dir, "zai")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := `{"models":[{"slug":"stale","input_modalities":["video"]}]}`
	if err := os.WriteFile(catalogPath, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(nil, []detect.Tool{{Name: "codex", Command: codexPath}}, dir,
		log.NewWithOptions(io.Discard, log.Options{}))
	m.StartBackgroundFetch(ctx)

	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "video") {
		t.Fatalf("stale modality survived startup cache load: %s", raw)
	}
	var doc CodexCatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Models) != 1 || doc.Models[0].Slug != "glm-5.3-flash" {
		t.Fatalf("catalog models = %+v, want glm-5.3-flash", doc.Models)
	}
	want := []string{"text", "image"}
	if got := doc.Models[0].InputModalities; !reflect.DeepEqual(got, want) {
		t.Fatalf("modalities = %v, want %v", got, want)
	}
}
