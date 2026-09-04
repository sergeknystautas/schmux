package models

// ProviderProfile maps a models.dev provider to schmux runner config.
type ProviderProfile struct {
	Runner          string            // schmux runner name (claude, codex, gemini, opencode)
	Endpoint        string            // API endpoint override (empty = runner's default)
	RequiredSecrets []string          // secrets needed for this provider
	SchmuxProvider  string            // internal provider name if different from models.dev name
	OpencodePrefix  string            // prefix for opencode runner (e.g., "zhipu" for zai)
	UsageURL        string            // signup/pricing page
	Category        string            // "native" or "third-party"
	SkipIDPatterns  []string          // ID suffixes to skip during registry parse
	Env             map[string]string // static env vars the runner needs for this provider
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
	// Kimi's subscription plan, not the metered "moonshotai" provider. Both serve
	// Kimi models but from different hosts with different model IDs, so schmux
	// registers only the subscription one. SchmuxProvider stays "moonshot" to keep
	// existing secrets.json entries working.
	"kimi-for-coding": {
		Runner:          "claude",
		Endpoint:        "https://api.kimi.com/coding",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		SchmuxProvider:  "moonshot",
		OpencodePrefix:  "kimi-for-coding",
		UsageURL:        "https://www.kimi.com/code",
		Category:        "third-party",
	},
	"zai-coding-plan": {
		Runner:          "claude",
		Endpoint:        "https://api.z.ai/api/anthropic",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		SchmuxProvider:  "zai",
		OpencodePrefix:  "zhipu",
		UsageURL:        "https://z.ai/manage-apikey/subscription",
		Category:        "third-party",
		// From https://docs.z.ai/devpack/tool/claude#manual-configuration.
		Env: map[string]string{
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "1000000",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"API_TIMEOUT_MS":                           "3000000",
		},
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
