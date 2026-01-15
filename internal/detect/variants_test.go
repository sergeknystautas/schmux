package detect

import (
	"context"
	"testing"
)

func TestGetBuiltinVariants(t *testing.T) {
	variants := GetBuiltinVariants()

	if len(variants) != 3 {
		t.Fatalf("expected 3 builtin variants, got %d", len(variants))
	}

	// Check that expected variants exist
	variantNames := make(map[string]bool)
	for _, v := range variants {
		variantNames[v.Name] = true
	}

	expectedVariants := []string{"kimi-thinking", "glm-4.7", "minimax"}
	for _, name := range expectedVariants {
		if !variantNames[name] {
			t.Errorf("missing expected variant: %s", name)
		}
	}
}

func TestGetVariantByName(t *testing.T) {
	tests := []struct {
		name      string
		variant   string
		wantFound bool
	}{
		{"kimi-thinking exists", "kimi-thinking", true},
		{"glm-4.7 exists", "glm-4.7", true},
		{"minimax exists", "minimax", true},
		{"unknown variant", "unknown-variant", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variant, found := GetVariantByName(tt.variant)
			if found != tt.wantFound {
				t.Errorf("GetVariantByName(%q) found = %v, want %v", tt.variant, found, tt.wantFound)
			}
			if found && variant.Name != tt.variant {
				t.Errorf("GetVariantByName(%q) returned variant.Name = %q, want %q", tt.variant, variant.Name, tt.variant)
			}
		})
	}
}

func TestIsVariantName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"kimi-thinking is variant", "kimi-thinking", true},
		{"glm-4.7 is variant", "glm-4.7", true},
		{"minimax is variant", "minimax", true},
		{"claude is not variant", "claude", false},
		{"codex is not variant", "codex", false},
		{"unknown is not variant", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsVariantName(tt.input)
			if result != tt.expected {
				t.Errorf("IsVariantName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetAvailableVariants(t *testing.T) {
	// Test with no detected agents
	t.Run("no detected agents", func(t *testing.T) {
		agents := []Agent{}
		available := GetAvailableVariants(agents)
		if len(available) != 0 {
			t.Errorf("expected 0 available variants with no agents, got %d", len(available))
		}
	})

	// Test with claude detected
	t.Run("claude detected", func(t *testing.T) {
		agents := []Agent{
			{Name: "claude", Command: "~/.claude/local/claude", Agentic: true},
		}
		available := GetAvailableVariants(agents)
		if len(available) != 3 {
			t.Errorf("expected 3 available variants with claude, got %d", len(available))
		}
	})

	// Test with only codex detected
	t.Run("only codex detected", func(t *testing.T) {
		agents := []Agent{
			{Name: "codex", Command: "codex", Agentic: true},
		}
		available := GetAvailableVariants(agents)
		if len(available) != 0 {
			t.Errorf("expected 0 available variants with only codex, got %d", len(available))
		}
	})
}

func TestResolveVariantCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("kimi-thinking with secrets", func(t *testing.T) {
		variant, _ := GetVariantByName("kimi-thinking")
		secrets := map[string]string{
			"ANTHROPIC_AUTH_TOKEN": "sk-test-key-12345",
		}

		cmd, err := ResolveVariantCommand(ctx, variant, "~/.claude/local/claude", secrets)
		if err != nil {
			t.Fatalf("ResolveVariantCommand failed: %v", err)
		}

		// Check that command contains the expected environment variables
		if !contains(cmd, "export ANTHROPIC_BASE_URL='https://api.moonshot.ai/anthropic'") {
			t.Errorf("command missing ANTHROPIC_BASE_URL")
		}
		if !contains(cmd, "export ANTHROPIC_AUTH_TOKEN='sk-test-key-12345'") {
			t.Errorf("command missing ANTHROPIC_AUTH_TOKEN")
		}
		if !contains(cmd, "~/.claude/local/claude") {
			t.Errorf("command missing base tool command")
		}
	})

	t.Run("missing required secret", func(t *testing.T) {
		variant, _ := GetVariantByName("kimi-thinking")
		secrets := map[string]string{} // empty secrets

		_, err := ResolveVariantCommand(ctx, variant, "~/.claude/local/claude", secrets)
		if err == nil {
			t.Error("expected error for missing secret, got nil")
		}
	})
}

func TestVariantStructure(t *testing.T) {
	// Verify that all builtin variants have the required fields
	variants := GetBuiltinVariants()

	for _, v := range variants {
		if v.Name == "" {
			t.Errorf("variant has empty Name")
		}
		if v.DisplayName == "" {
			t.Errorf("variant %q has empty DisplayName", v.Name)
		}
		if v.BaseTool == "" {
			t.Errorf("variant %q has empty BaseTool", v.Name)
		}
		if len(v.Env) == 0 {
			t.Errorf("variant %q has empty Env", v.Name)
		}
		if len(v.RequiredSecrets) == 0 {
			t.Errorf("variant %q has empty RequiredSecrets", v.Name)
		}
		if v.UsageURL == "" {
			t.Errorf("variant %q has empty UsageURL", v.Name)
		}

		// Verify that all variants are based on claude for now
		if v.BaseTool != "claude" {
			t.Errorf("variant %q has BaseTool %q, expected 'claude'", v.Name, v.BaseTool)
		}

		// Verify that ANTHROPIC_BASE_URL is set
		if _, ok := v.Env["ANTHROPIC_BASE_URL"]; !ok {
			t.Errorf("variant %q missing ANTHROPIC_BASE_URL in Env", v.Name)
		}

		// Verify that ANTHROPIC_AUTH_TOKEN is in RequiredSecrets
		found := false
		for _, s := range v.RequiredSecrets {
			if s == "ANTHROPIC_AUTH_TOKEN" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("variant %q missing ANTHROPIC_AUTH_TOKEN in RequiredSecrets", v.Name)
		}
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		len(s) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
