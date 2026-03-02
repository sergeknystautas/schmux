package actions

import (
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// MatchPrompt finds the best matching pinned agent action for a given prompt.
// Returns the action ID and whether the prompt was edited (differs from template with defaults filled).
// Returns empty string if no match.
func (r *Registry) MatchPrompt(prompt string) (actionID string, edited bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	normalizedPrompt := strings.TrimSpace(strings.ToLower(prompt))
	if normalizedPrompt == "" {
		return "", false
	}

	var bestID string
	var bestLen int
	var bestEdited bool

	for _, a := range r.actions {
		if a.State != contracts.ActionStatePinned || a.Type != contracts.ActionTypeAgent {
			continue
		}
		if a.Template == "" {
			continue
		}

		prefix := extractStaticPrefix(a.Template)
		if prefix == "" {
			continue
		}

		normalizedPrefix := strings.TrimSpace(strings.ToLower(prefix))
		if normalizedPrefix == "" {
			continue
		}

		if strings.HasPrefix(normalizedPrompt, normalizedPrefix) && len(normalizedPrefix) > bestLen {
			bestID = a.ID
			bestLen = len(normalizedPrefix)

			// Check if the prompt exactly matches the template with defaults substituted.
			filled := fillDefaults(a.Template, a.Parameters)
			bestEdited = strings.TrimSpace(strings.ToLower(filled)) != normalizedPrompt
		}
	}

	return bestID, bestEdited
}

// extractStaticPrefix returns the text before the first "{{" in a template.
func extractStaticPrefix(template string) string {
	idx := strings.Index(template, "{{")
	if idx < 0 {
		return template // No parameters, entire template is the prefix.
	}
	return template[:idx]
}

// fillDefaults substitutes parameter defaults into a template.
// E.g., "Fix lint in {{path}}" with path default "src/" => "Fix lint in src/"
func fillDefaults(template string, params []contracts.ActionParameter) string {
	result := template
	for _, p := range params {
		if p.Default != "" {
			result = strings.ReplaceAll(result, "{{"+p.Name+"}}", p.Default)
		}
	}
	return result
}
