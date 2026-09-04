package models

import "testing"

func TestGetProfile_Anthropic(t *testing.T) {
	p, ok := GetProviderProfile("anthropic")
	if !ok {
		t.Fatal("anthropic profile not found")
	}
	if p.Runner != "claude" {
		t.Errorf("expected runner 'claude', got %q", p.Runner)
	}
	if p.Category != "native" {
		t.Errorf("expected category 'native', got %q", p.Category)
	}
	if p.Endpoint != "" {
		t.Errorf("expected empty endpoint, got %q", p.Endpoint)
	}
}

func TestGetProfile_KimiForCoding(t *testing.T) {
	p, ok := GetProviderProfile("kimi-for-coding")
	if !ok {
		t.Fatal("kimi-for-coding profile not found")
	}
	if p.Runner != "claude" {
		t.Errorf("expected runner 'claude', got %q", p.Runner)
	}
	if p.SchmuxProvider != "moonshot" {
		t.Errorf("expected schmux_provider 'moonshot', got %q", p.SchmuxProvider)
	}
	if p.OpencodePrefix != "kimi-for-coding" {
		t.Errorf("expected opencode_prefix 'kimi-for-coding', got %q", p.OpencodePrefix)
	}
	if p.Endpoint != "https://api.kimi.com/coding" {
		t.Errorf("wrong endpoint: %q", p.Endpoint)
	}
	if len(p.RequiredSecrets) != 1 || p.RequiredSecrets[0] != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("wrong required secrets: %v", p.RequiredSecrets)
	}
}

// The metered Moonshot provider was replaced by the subscription plan; it must
// not come back silently, since its model IDs 404 on the subscription endpoint.
func TestGetProfile_MoonshotaiNotRegistered(t *testing.T) {
	if _, ok := GetProviderProfile("moonshotai"); ok {
		t.Error("moonshotai profile should not be registered")
	}
}

func TestGetProfile_Unknown(t *testing.T) {
	_, ok := GetProviderProfile("nonexistent")
	if ok {
		t.Error("expected false for unknown provider")
	}
}

func TestGetProfile_AllProviders(t *testing.T) {
	expected := []string{"anthropic", "openai", "google", "kimi-for-coding", "zai-coding-plan", "minimax"}
	for _, name := range expected {
		if _, ok := GetProviderProfile(name); !ok {
			t.Errorf("missing profile for %q", name)
		}
	}
}

func TestGetProfile_ZaiEnv(t *testing.T) {
	p, ok := GetProviderProfile("zai-coding-plan")
	if !ok {
		t.Fatal("zai-coding-plan profile not found")
	}
	want := map[string]string{
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "1000000",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"API_TIMEOUT_MS":                           "3000000",
	}
	if len(p.Env) != len(want) {
		t.Fatalf("Env has %d entries, want %d: %v", len(p.Env), len(want), p.Env)
	}
	for k, v := range want {
		if got := p.Env[k]; got != v {
			t.Errorf("Env[%q] = %q, want %q", k, got, v)
		}
	}
}

// Env is z.ai-specific. Other providers must not pick it up by accident.
func TestGetProfile_OnlyZaiHasEnv(t *testing.T) {
	for _, name := range []string{"anthropic", "openai", "google", "kimi-for-coding", "minimax"} {
		p, ok := GetProviderProfile(name)
		if !ok {
			t.Fatalf("%s profile not found", name)
		}
		if len(p.Env) != 0 {
			t.Errorf("%s: Env = %v, want empty", name, p.Env)
		}
	}
}

func TestCanonicalProvider(t *testing.T) {
	tests := []struct {
		modelsDevProvider string
		want              string
	}{
		{"anthropic", "anthropic"},
		{"kimi-for-coding", "moonshot"},
		{"zai-coding-plan", "zai"},
		{"minimax", "minimax"},
	}
	for _, tt := range tests {
		p, _ := GetProviderProfile(tt.modelsDevProvider)
		got := p.CanonicalProvider()
		if got != tt.want {
			t.Errorf("CanonicalProvider(%q) = %q, want %q", tt.modelsDevProvider, got, tt.want)
		}
	}
}
