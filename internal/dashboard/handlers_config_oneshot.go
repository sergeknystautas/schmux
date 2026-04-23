package dashboard

import (
	"fmt"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/directhttp"
	"github.com/sergeknystautas/schmux/internal/models"
)

// buildOneshotTargets assembles live data from the handler (config secrets,
// enabled-models map, Ollama probe cache) and delegates to the pure
// buildOneshotTargetsPure function so the filter rules are unit-testable.
func (h *ConfigHandlers) buildOneshotTargets(catalog *models.CatalogResult) []contracts.OneshotTarget {
	if catalog == nil {
		return nil
	}
	oauthToken, _ := config.GetAnthropicOAuthToken()
	return buildOneshotTargetsPure(
		catalog.Models,
		catalog.Runners,
		h.models.GetEnabledModels(),
		oauthToken,
		config.GetProviderSecrets,
		directhttp.GetOllamaModels(),
	)
}

// buildOneshotTargetsPure computes the one-shot picker's flat row list.
// Rules from docs/specs/2026-04-19-lightweight-oneshot.md §User experience → Dropdown:
//
//  1. CLI rows: model is SELECTED (enabledModels[m.ID] is set) AND the
//     pinned runner's YAML harness has "oneshot" in its capabilities.
//     No fallback to other runners — pinning a non-oneshot runner excludes
//     the model from the picker.
//  2. Third-party (API) rows: model is SELECTED, provider is NOT anthropic,
//     model's Runners include "claude" (spec: "support claude as their
//     harness"), and the provider has at least one secret configured.
//  3. Anthropic (API) rows: model is SELECTED, provider is anthropic,
//     the subscription OAuth token is stored.
//  4. Ollama rows: whatever the /api/tags probe returned; no selection gate.
//
// Ordering: iterate catalogModels in the order given (models.Manager
// returns them sorted by ID — we don't re-sort here). For each selected
// model, the (CLI) row comes first and the corresponding (API) row —
// Anthropic or third-party — immediately after. Ollama rows come last,
// in probe order.
func buildOneshotTargetsPure(
	catalogModels []contracts.Model,
	runners map[string]contracts.RunnerInfo,
	enabled map[string]string,
	anthropicOAuthToken string,
	providerSecrets func(provider string) (map[string]string, error),
	ollamaModels []string,
) []contracts.OneshotTarget {
	out := make([]contracts.OneshotTarget, 0, len(catalogModels)+len(ollamaModels))

	for _, m := range catalogModels {
		pinnedRunner, isSelected := enabled[m.ID]
		if !isSelected {
			continue
		}
		// CLI row — pinned runner must have "oneshot" capability.
		if runnerHasCapability(runners, pinnedRunner, "oneshot") {
			out = append(out, contracts.OneshotTarget{
				ID:     m.ID,
				Label:  fmt.Sprintf("%s (CLI)", m.DisplayName),
				Source: "cli",
			})
		}
		// API row — immediately after the CLI row (or solo if no CLI row).
		if m.Provider == "anthropic" {
			if anthropicOAuthToken != "" {
				out = append(out, contracts.OneshotTarget{
					ID:     m.ID + directhttp.APISuffix,
					Label:  fmt.Sprintf("%s (API)", m.DisplayName),
					Source: "anthropic_api",
				})
			}
			continue
		}
		// Third-party: requires "claude" harness support AND a provider secret.
		if !modelHasRunner(m, "claude") {
			continue
		}
		secrets, _ := providerSecrets(m.Provider)
		if len(secrets) == 0 {
			continue
		}
		out = append(out, contracts.OneshotTarget{
			ID:     m.ID + directhttp.APISuffix,
			Label:  fmt.Sprintf("%s (API)", m.DisplayName),
			Source: "third_party_api",
		})
	}

	// Ollama rows come last, in probe order — they are not "selected in the
	// catalog" (they aren't in the catalog at all) and the spec puts them
	// outside the catalog-derived source list.
	for _, id := range ollamaModels {
		out = append(out, contracts.OneshotTarget{
			ID:     id + directhttp.APISuffix,
			Label:  fmt.Sprintf("%s (Ollama API)", id),
			Source: "ollama_api",
		})
	}
	return out
}

func modelHasRunner(m contracts.Model, runner string) bool {
	for _, r := range m.Runners {
		if r == runner {
			return true
		}
	}
	return false
}

func runnerHasCapability(
	runners map[string]contracts.RunnerInfo,
	name, capability string,
) bool {
	if name == "" {
		return false
	}
	r, ok := runners[name]
	if !ok {
		return false
	}
	for _, c := range r.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}
