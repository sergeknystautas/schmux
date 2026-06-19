package models

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
)

// agy (Antigravity) is a multi-model harness whose model list is defined by the
// tool itself, auth-gated, and absent from models.dev. These helpers discover
// that list at runtime by running `agy models` and feed it into the catalog as a
// dedicated layer. agy owns its own auth, so discovered runners carry no
// secrets and no endpoint — schmux just shells out.

const (
	// antigravityRefreshInterval is how often `agy models` is re-run. Signing
	// into agy after the daemon starts surfaces models on the next tick without
	// a restart (only when agy was detected at startup; installing agy later
	// still needs a restart, same as every other harness).
	antigravityRefreshInterval = 15 * time.Minute
	// antigravityExecTimeout bounds a single `agy models` invocation so a hung
	// or sign-in-prompting run can't stall the refresh loop.
	antigravityExecTimeout = 15 * time.Second
)

// parseAntigravityModels parses the plain-text output of `agy models` (one model
// display string per line) into catalog models. Blank lines and obvious
// non-model output (sign-in prompts, errors, usage text) are skipped.
func parseAntigravityModels(output []byte) []detect.Model {
	var out []detect.Model
	for _, line := range strings.Split(string(output), "\n") {
		display := strings.TrimSpace(line)
		if !looksLikeAntigravityModel(display) {
			continue
		}
		out = append(out, detect.Model{
			ID:          antigravityModelID(display),
			DisplayName: display,
			Provider:    antigravityProvider(display),
			Category:    "native",
			Runners: map[string]detect.RunnerSpec{
				// ModelValue is the exact display string agy expects after --model.
				"antigravity": {ModelValue: display},
			},
		})
	}
	return out
}

// looksLikeAntigravityModel rejects blank lines and common non-model output
// `agy models` can emit when not authenticated or misconfigured (errors,
// sign-in prompts, usage/help text). Real model lines are bare display strings.
func looksLikeAntigravityModel(line string) bool {
	if line == "" {
		return false
	}
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"error", "sign in", "sign-in", "signin", "log in", "login",
		"authenticate", "unauthor", "not authorized", "usage:",
		"available models", "no models",
	} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	// Help/flag lines (e.g. "  -h, --help") start with '-'.
	if strings.HasPrefix(strings.TrimLeft(line, " \t"), "-") {
		return false
	}
	return true
}

// antigravityModelID builds a deterministic, collision-free catalog ID from a
// display string: lowercase, non-alphanumeric runs collapsed to '-', prefixed
// with "antigravity-". The prefix guarantees it never collides with registry,
// user-defined, or default (adapter-name) IDs.
func antigravityModelID(display string) string {
	var b strings.Builder
	prevSep := true // true collapses leading separators
	for _, r := range strings.ToLower(display) {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			prevSep = false
			continue
		}
		if !prevSep {
			b.WriteByte('-')
			prevSep = true
		}
	}
	return "antigravity-" + strings.TrimRight(b.String(), "-")
}

// antigravityProvider derives a cosmetic provider (for UI grouping only) from
// the display-string prefix. Empty when unknown.
func antigravityProvider(display string) string {
	switch {
	case strings.HasPrefix(strings.ToLower(display), "gemini"):
		return "google"
	case strings.HasPrefix(strings.ToLower(display), "claude"):
		return "anthropic"
	case strings.HasPrefix(strings.ToLower(display), "gpt"):
		return "openai"
	default:
		return ""
	}
}

// SetAntigravityModels sets the antigravity-discovered models layer and rebuilds
// the catalog. Called by the discovery loop; IDs are prefixed so they merge
// without collision. Returns true if the layer actually changed, so the caller
// can broadcast a catalog update only when there's something to broadcast.
func (m *Manager) SetAntigravityModels(models []detect.Model) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if antigravityModelsEqual(m.antigravityModels, models) {
		return false
	}
	m.antigravityModels = models
	m.rebuildCatalog()
	return true
}

// antigravityModelsEqual reports whether two discovered-model layers are
// equivalent for catalog purposes (same IDs, display names, and model values in
// the same order). Used to suppress redundant broadcasts every refresh tick.
func antigravityModelsEqual(a, b []detect.Model) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].DisplayName != b[i].DisplayName {
			return false
		}
		if a[i].Runners["antigravity"].ModelValue != b[i].Runners["antigravity"].ModelValue {
			return false
		}
	}
	return true
}

// notifyCatalogUpdated fires the catalog-updated callback (dashboard broadcast)
// outside the manager lock. Mirrors the registry fetch path.
func (m *Manager) notifyCatalogUpdated() {
	m.mu.RLock()
	cb := m.onCatalogUpdated
	m.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

// StartAntigravityDiscovery runs `agy models` periodically to keep the catalog
// populated with agy's auth-gated model list. It is a no-op when agy is not
// among the detected tools.
func (m *Manager) StartAntigravityDiscovery(ctx context.Context) {
	cmd := m.antigravityCommand()
	if cmd == "" {
		return
	}
	go m.antigravityLoop(ctx, cmd)
}

// antigravityCommand returns the detected agy invocation, or "" if agy was not
// detected at startup.
func (m *Manager) antigravityCommand() string {
	for _, t := range m.detectedTools {
		if t.Name == "antigravity" {
			return t.Command
		}
	}
	return ""
}

func (m *Manager) antigravityLoop(ctx context.Context, cmd string) {
	m.fetchAntigravityModels(ctx, cmd)

	ticker := time.NewTicker(antigravityRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.fetchAntigravityModels(ctx, cmd)
		}
	}
}

func (m *Manager) fetchAntigravityModels(ctx context.Context, cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}
	// Build args on a fresh slice so we never alias parts' backing array.
	args := append(append([]string{}, parts[1:]...), "models")
	execCtx, cancel := context.WithTimeout(ctx, antigravityExecTimeout)
	defer cancel()
	out, err := exec.CommandContext(execCtx, parts[0], args...).Output()
	if err != nil {
		// Keep the previously discovered list. A failed `agy models` (signed out,
		// timed out, transient network) must not wipe the catalog mid-session —
		// this mirrors the registry source, which retains stale models on fetch
		// error. The list refreshes on the next successful tick. A genuinely empty
		// model set only comes from a *successful* run returning no models.
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		if stderr != "" {
			m.logger.Warn("agy models discovery failed; keeping previous list", "err", err, "stderr", stderr)
		} else {
			m.logger.Warn("agy models discovery failed; keeping previous list", "err", err)
		}
		return
	}

	models := parseAntigravityModels(out)
	if m.SetAntigravityModels(models) {
		// Catalog changed — notify the dashboard (mirrors the registry fetch path).
		m.notifyCatalogUpdated()
		m.logger.Info("updated agy models", "count", len(models))
	}
}
