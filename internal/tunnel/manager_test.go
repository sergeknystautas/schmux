package tunnel

import (
	"strings"
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

func TestTunnelState_StartRequiresPinHash(t *testing.T) {
	m := NewManager(ManagerConfig{PinHashSet: false})
	err := m.Start()
	if err == nil {
		t.Fatal("expected error when PIN not configured")
	}
	if !strings.Contains(err.Error(), "PIN") {
		t.Errorf("error should mention PIN, got: %s", err.Error())
	}
}

func TestTunnelState_StartRequiresNotDisabled(t *testing.T) {
	m := NewManager(ManagerConfig{
		Disabled:   true,
		PinHashSet: true,
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
