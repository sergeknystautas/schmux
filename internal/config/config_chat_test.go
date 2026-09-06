package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatSessionsFlag_RoundTrip(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"chat_sessions":true}`), &cfg.ConfigData); err != nil {
		t.Fatal(err)
	}
	if !cfg.GetChatSessions() {
		t.Fatal("expected chat_sessions true")
	}
	out, _ := json.Marshal(cfg.ConfigData)
	if !strings.Contains(string(out), `"chat_sessions":true`) {
		t.Fatalf("flag not serialized: %s", out)
	}
}
