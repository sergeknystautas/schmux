package models

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CodexCatalogPath returns the path of the generated codex catalog for a
// canonical provider, inside the schmux dir's cache (beside models-dev.json).
func CodexCatalogPath(schmuxDir, provider string) string {
	return filepath.Join(schmuxDir, "cache", "codex-models-"+provider+".json")
}

// WithoutCatalogArgs removes any model_catalog_json override token and its
// preceding flag from args. Remote spawns embed daemon-local paths; the
// catalog has no remote counterpart, so its arg is dropped (spec §7).
func WithoutCatalogArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "model_catalog_json=") {
			if len(out) > 0 && strings.HasPrefix(out[len(out)-1], "-") {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// CatalogArgPath extracts the model_catalog_json path from provider-routing
// args, "" when the set carries none. Callers that hold the structured args
// (ResolvedModel.Args) use this instead of scanning a serialized command
// string, which cannot distinguish the arg from prompt text that merely
// mentions the key.
func CatalogArgPath(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "model_catalog_json=") {
			return strings.TrimPrefix(a, "model_catalog_json=")
		}
	}
	return ""
}

type codexReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type codexTruncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

// codexCatalogEntry carries exactly the field set codex 0.153.4-0.154.0
// requires per model (spike-established; missing fields abort launch).
type codexCatalogEntry struct {
	Slug                           string                `json:"slug"`
	DisplayName                    string                `json:"display_name"`
	Description                    string                `json:"description"`
	ContextWindow                  int                   `json:"context_window"`
	MaxContextWindow               int                   `json:"max_context_window"`
	EffectiveContextWindowPercent  int                   `json:"effective_context_window_percent"`
	InputModalities                []string              `json:"input_modalities"`
	DefaultReasoningLevel          string                `json:"default_reasoning_level"`
	SupportedReasoningLevels       []codexReasoningLevel `json:"supported_reasoning_levels"`
	TruncationPolicy               codexTruncationPolicy `json:"truncation_policy"`
	BaseInstructions               string                `json:"base_instructions"`
	ShellType                      string                `json:"shell_type"`
	Visibility                     string                `json:"visibility"`
	SupportedInAPI                 bool                  `json:"supported_in_api"`
	Priority                       int                   `json:"priority"`
	DefaultReasoningSummary        string                `json:"default_reasoning_summary"`
	SupportVerbosity               bool                  `json:"support_verbosity"`
	DefaultVerbosity               string                `json:"default_verbosity"`
	ApplyPatchToolType             string                `json:"apply_patch_tool_type"`
	WebSearchToolType              string                `json:"web_search_tool_type"`
	SupportsImageDetailOriginal    bool                  `json:"supports_image_detail_original"`
	SupportsSearchTool             bool                  `json:"supports_search_tool"`
	IncludeSkillsUsageInstructions bool                  `json:"include_skills_usage_instructions"`
	IncludePluginUsageInstructions bool                  `json:"include_plugin_usage_instructions"`
	IncludeAppsUsageInstructions   bool                  `json:"include_apps_usage_instructions"`
	ExperimentalSupportedTools     []string              `json:"experimental_supported_tools"`
	UseResponsesLite               bool                  `json:"use_responses_lite"`
	NodeReplAutoReviewRequired     bool                  `json:"node_repl_auto_review_required"`
	NodeReplDisabled               bool                  `json:"node_repl_disabled"`
}

// CodexCatalogDoc is the wire shape of a codex model_catalog_json file.
type CodexCatalogDoc struct {
	Models []codexCatalogEntry `json:"models"`
}

// ReasoningEffortEnvVar names the environment variable that overrides the
// default reasoning effort baked into generated codex catalogs. Codex itself
// has no effort env var (effort is the config key model_reasoning_effort /
// the catalog's default_reasoning_level), so the daemon-side env var controls
// what the generated catalog declares.
const ReasoningEffortEnvVar = "SCHMUX_CODEX_REASONING_EFFORT"

// defaultReasoningEffort is the effort the catalog declares when the env
// variable is unset or holds a value outside the declared levels.
const defaultReasoningEffort = "high"

// supportedReasoningEfforts is the level set every generated entry declares.
var supportedReasoningEfforts = []codexReasoningLevel{
	{Effort: "low", Description: "Fast responses with lighter reasoning"},
	{Effort: "high", Description: "Greater reasoning depth for complex problems"},
	{Effort: "max", Description: "Maximum reasoning depth"},
}

// ReasoningEffortFromEnv resolves the catalog's default reasoning effort:
// the value of ReasoningEffortEnvVar when it names a declared level, else
// defaultReasoningEffort.
func ReasoningEffortFromEnv() string {
	if v := strings.TrimSpace(os.Getenv(ReasoningEffortEnvVar)); v != "" {
		for _, l := range supportedReasoningEfforts {
			if v == l.Effort {
				return v
			}
		}
	}
	return defaultReasoningEffort
}

// codexInputModalities is codex's input_modalities enum. The feed declares
// values outside it (glm-5.3-flash: text,image,video,pdf), and one
// out-of-enum value makes codex reject the entire catalog at launch
// ("unknown variant"), so unknown values are dropped, not passed through.
var codexInputModalities = map[string]bool{"text": true, "image": true, "audio": true}

// BuildCodexCatalog maps filtered registry models to codex catalog entries.
// Models without a context window are skipped: a wrong window mis-sizes
// truncation, which is the failure the catalog exists to prevent.
func BuildCodexCatalog(models []RegistryModel, baseInstructions string) *CodexCatalogDoc {
	doc := &CodexCatalogDoc{Models: []codexCatalogEntry{}}
	effort := ReasoningEffortFromEnv()
	for _, rm := range models {
		if rm.ContextWindow <= 0 {
			continue
		}
		description := rm.Description
		if description == "" {
			description = rm.DisplayName + " served over the Responses API."
		}
		modalities := make([]string, 0, len(rm.InputModalities))
		for _, m := range rm.InputModalities {
			if codexInputModalities[m] {
				modalities = append(modalities, m)
			}
		}
		if len(modalities) == 0 {
			modalities = []string{"text"}
		}
		doc.Models = append(doc.Models, codexCatalogEntry{
			Slug:                           rm.ID,
			DisplayName:                    rm.DisplayName,
			Description:                    description,
			ContextWindow:                  rm.ContextWindow,
			MaxContextWindow:               rm.ContextWindow,
			EffectiveContextWindowPercent:  95,
			InputModalities:                modalities,
			DefaultReasoningLevel:          effort,
			SupportedReasoningLevels:       supportedReasoningEfforts,
			TruncationPolicy:               codexTruncationPolicy{Mode: "bytes", Limit: 10000},
			BaseInstructions:               baseInstructions,
			ShellType:                      "unified_exec",
			Visibility:                     "list",
			SupportedInAPI:                 true,
			Priority:                       1,
			DefaultReasoningSummary:        "none",
			SupportVerbosity:               true,
			DefaultVerbosity:               "low",
			ApplyPatchToolType:             "freeform",
			WebSearchToolType:              "text_and_image",
			IncludeSkillsUsageInstructions: false,
			IncludePluginUsageInstructions: false,
			IncludeAppsUsageInstructions:   false,
			ExperimentalSupportedTools:     []string{},
			UseResponsesLite:               false,
			NodeReplAutoReviewRequired:     false,
			NodeReplDisabled:               false,
		})
	}
	return doc
}

// ExtractBaseInstructions lifts the codex agent system prompt from a
// `codex debug models` dump: prefer the stable gpt-5.5 built-in, else the
// first entry carrying instructions. Empty when the dump has none.
func ExtractBaseInstructions(dump []byte) string {
	var doc struct {
		Models []struct {
			Slug             string `json:"slug"`
			BaseInstructions string `json:"base_instructions"`
		} `json:"models"`
	}
	if err := json.Unmarshal(dump, &doc); err != nil {
		return ""
	}
	first := ""
	for _, m := range doc.Models {
		if m.BaseInstructions == "" {
			continue
		}
		if m.Slug == "gpt-5.5" {
			return m.BaseInstructions
		}
		if first == "" {
			first = m.BaseInstructions
		}
	}
	return first
}

// regenerateCodexCatalogsWith writes the per-provider catalogs for every
// profile that declares a codex ExtraRunner. Split from the Manager method
// so tests inject the dump instead of shelling out.
func regenerateCodexCatalogsWith(schmuxDir string, codexDump []byte, models []RegistryModel) error {
	base := ExtractBaseInstructions(codexDump)
	if base == "" {
		return fmt.Errorf("codex dump carries no base_instructions")
	}
	byProvider := map[string][]RegistryModel{}
	for _, rm := range models {
		profile, ok := GetProviderProfile(rm.Provider)
		if !ok {
			continue
		}
		if _, hasCodex := profile.ExtraRunners["codex"]; !hasCodex {
			continue
		}
		byProvider[profile.CanonicalProvider()] = append(byProvider[profile.CanonicalProvider()], rm)
	}
	for provider, pms := range byProvider {
		doc := BuildCodexCatalog(pms, base)
		path := CodexCatalogPath(schmuxDir, provider)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		encoded, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// regenerateCodexCatalogs lifts base_instructions from the installed codex
// and regenerates the provider catalogs. Best-effort: failures log and
// leave the previous file, so sessions degrade to fallback metadata, never
// a broken launch. The dump runs under a timeout so a hung codex binary
// cannot stall the caller (startup, or the post-fetch catalog broadcast).
func (m *Manager) regenerateCodexCatalogs(models []RegistryModel) {
	codexCmd := ""
	for _, t := range m.detectedTools {
		if t.Name == "codex" && t.Command != "" {
			codexCmd = strings.Fields(t.Command)[0]
			break
		}
	}
	if codexCmd == "" {
		return // undetected codex: skip generation entirely
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, codexCmd, "debug", "models").Output()
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("codex catalog: debug models failed", "err", err)
		}
		return
	}
	if err := regenerateCodexCatalogsWith(m.schmuxDir, out, models); err != nil {
		if m.logger != nil {
			m.logger.Warn("codex catalog: regeneration failed", "err", err)
		}
	}
}
