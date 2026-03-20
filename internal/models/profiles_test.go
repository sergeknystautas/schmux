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

func TestGetProfile_Moonshotai(t *testing.T) {
	p, ok := GetProviderProfile("moonshotai")
	if !ok {
		t.Fatal("moonshotai profile not found")
	}
	if p.Runner != "claude" {
		t.Errorf("expected runner 'claude', got %q", p.Runner)
	}
	if p.SchmuxProvider != "moonshot" {
		t.Errorf("expected schmux_provider 'moonshot', got %q", p.SchmuxProvider)
	}
	if p.OpencodePrefix != "moonshot" {
		t.Errorf("expected opencode_prefix 'moonshot', got %q", p.OpencodePrefix)
	}
	if p.Endpoint != "https://api.moonshot.ai/anthropic" {
		t.Errorf("wrong endpoint: %q", p.Endpoint)
	}
	if len(p.RequiredSecrets) != 1 || p.RequiredSecrets[0] != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("wrong required secrets: %v", p.RequiredSecrets)
	}
}

func TestGetProfile_Unknown(t *testing.T) {
	_, ok := GetProviderProfile("nonexistent")
	if ok {
		t.Error("expected false for unknown provider")
	}
}

func TestGetProfile_AllProviders(t *testing.T) {
	expected := []string{"anthropic", "openai", "google", "moonshotai", "zai", "minimax"}
	for _, name := range expected {
		if _, ok := GetProviderProfile(name); !ok {
			t.Errorf("missing profile for %q", name)
		}
	}
}

func TestCanonicalProvider(t *testing.T) {
	tests := []struct {
		modelsDevProvider string
		want              string
	}{
		{"anthropic", "anthropic"},
		{"moonshotai", "moonshot"},
		{"zai", "zai"},
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
