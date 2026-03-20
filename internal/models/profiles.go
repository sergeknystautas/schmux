package models

// ProviderProfile maps a models.dev provider to schmux runner config.
type ProviderProfile struct {
	Runner          string   // schmux runner name (claude, codex, gemini, opencode)
	Endpoint        string   // API endpoint override (empty = runner's default)
	RequiredSecrets []string // secrets needed for this provider
	SchmuxProvider  string   // internal provider name if different from models.dev name
	OpencodePrefix  string   // prefix for opencode runner (e.g., "zhipu" for zai)
	UsageURL        string   // signup/pricing page
	Category        string   // "native" or "third-party"
	SkipIDPatterns  []string // ID suffixes to skip during registry parse
}

// CanonicalProvider returns the schmux-internal provider name.
func (p ProviderProfile) CanonicalProvider() string {
	if p.SchmuxProvider != "" {
		return p.SchmuxProvider
	}
	return p.OpencodePrefix // for native providers, opencode prefix == provider name
}

var providerProfiles = map[string]ProviderProfile{
	"anthropic": {
		Runner:         "claude",
		Category:       "native",
		OpencodePrefix: "anthropic",
	},
	"openai": {
		Runner:         "codex",
		Category:       "native",
		OpencodePrefix: "openai",
		SkipIDPatterns: []string{"-chat-latest"},
	},
	"google": {
		Runner:         "gemini",
		Category:       "native",
		OpencodePrefix: "google",
	},
	"moonshotai": {
		Runner:          "claude",
		Endpoint:        "https://api.moonshot.ai/anthropic",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		SchmuxProvider:  "moonshot",
		OpencodePrefix:  "moonshot",
		UsageURL:        "https://platform.moonshot.ai/console/account",
		Category:        "third-party",
	},
	"zai": {
		Runner:          "claude",
		Endpoint:        "https://api.z.ai/api/anthropic",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		SchmuxProvider:  "zai",
		OpencodePrefix:  "zhipu",
		UsageURL:        "https://z.ai/manage-apikey/subscription",
		Category:        "third-party",
	},
	"minimax": {
		Runner:          "claude",
		Endpoint:        "https://api.minimax.io/anthropic",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		OpencodePrefix:  "minimax",
		UsageURL:        "https://platform.minimax.io/user-center/payment/coding-plan",
		Category:        "third-party",
	},
}

// GetProviderProfile returns the profile for a models.dev provider name.
func GetProviderProfile(modelsDevProvider string) (ProviderProfile, bool) {
	p, ok := providerProfiles[modelsDevProvider]
	return p, ok
}

// SupportedProviders returns the list of models.dev provider names we support.
func SupportedProviders() []string {
	out := make([]string, 0, len(providerProfiles))
	for k := range providerProfiles {
		out = append(out, k)
	}
	return out
}
