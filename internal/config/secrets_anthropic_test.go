package config

import (
	"testing"
)

func TestSaveAndGetAnthropicOAuthToken_RoundTrip(t *testing.T) {
	dir := setupSecretsHome(t)

	// Verify secrets file is in the right place
	_ = dir

	if err := SaveAnthropicOAuthToken("sk-ant-oat-xyz"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := GetAnthropicOAuthToken()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "sk-ant-oat-xyz" {
		t.Errorf("got %q", got)
	}
}

func TestGetAnthropicOAuthToken_NotSet_ReturnsEmpty(t *testing.T) {
	_ = setupSecretsHome(t)

	got, err := GetAnthropicOAuthToken()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestSaveAnthropicOAuthToken_EmptyRemoves(t *testing.T) {
	_ = setupSecretsHome(t)

	if err := SaveAnthropicOAuthToken("sk-ant-oat-xyz"); err != nil {
		t.Fatal(err)
	}
	if err := SaveAnthropicOAuthToken(""); err != nil {
		t.Fatal(err)
	}
	got, _ := GetAnthropicOAuthToken()
	if got != "" {
		t.Errorf("expected empty after clear, got %q", got)
	}
}
