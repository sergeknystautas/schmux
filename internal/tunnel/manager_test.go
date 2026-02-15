package tunnel

import (
	"testing"
)

func TestTunnelState_InitiallyOff(t *testing.T) {
	m := NewManager(ManagerConfig{})
	status := m.Status()
	if status.State != StateOff {
		t.Errorf("expected StateOff, got %s", status.State)
	}
	if status.URL != "" {
		t.Errorf("expected empty URL, got %q", status.URL)
	}
}

func TestTunnelState_StartRequiresAuth(t *testing.T) {
	m := NewManager(ManagerConfig{
		AuthEnabled:     false,
		AllowedUsersSet: false,
	})

	err := m.Start()
	if err == nil {
		t.Fatal("expected error when auth not enabled")
	}
}

func TestTunnelState_StartRequiresAllowlist(t *testing.T) {
	m := NewManager(ManagerConfig{
		AuthEnabled:     true,
		AllowedUsersSet: false,
	})

	err := m.Start()
	if err == nil {
		t.Fatal("expected error when allowlist empty")
	}
}

func TestTunnelState_StartRequiresNotDisabled(t *testing.T) {
	m := NewManager(ManagerConfig{
		Disabled:        true,
		AuthEnabled:     true,
		AllowedUsersSet: true,
	})

	err := m.Start()
	if err == nil {
		t.Fatal("expected error when remote access disabled")
	}
}

func TestParseCloudflaredURL(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{
			"2024-01-15T10:30:00Z INF +--------------------------------------------------------------------------------------------+",
			"",
		},
		{
			"2024-01-15T10:30:00Z INF |  https://random-words-here.trycloudflare.com                                             |",
			"https://random-words-here.trycloudflare.com",
		},
		{
			"some other log line",
			"",
		},
	}

	for _, tt := range tests {
		got := parseCloudflaredURL(tt.line)
		if got != tt.want {
			t.Errorf("parseCloudflaredURL(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
