package tunnel

import (
	"os"
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

func TestTunnelState_StartRequiresPasswordHash(t *testing.T) {
	m := NewManager(ManagerConfig{PasswordHashSet: func() bool { return false }})
	err := m.Start()
	if err == nil {
		t.Fatal("expected error when password not configured")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("error should mention password, got: %s", err.Error())
	}
}

func TestTunnelState_StartRequiresNotDisabled(t *testing.T) {
	m := NewManager(ManagerConfig{
		Disabled:        func() bool { return true },
		PasswordHashSet: func() bool { return true },
	})

	err := m.Start()
	if err == nil {
		t.Fatal("expected error when remote access disabled")
	}
}

func TestTunnelState_StartRejectsNonLoopbackBind(t *testing.T) {
	tests := []struct {
		name        string
		bindAddress string
		wantErr     bool
	}{
		{"empty (default)", "", false},
		{"loopback IPv4", "127.0.0.1", false},
		{"loopback IPv6", "::1", false},
		{"all interfaces", "0.0.0.0", true},
		{"LAN address", "192.168.1.100", true},
		{"all IPv6", "::", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(ManagerConfig{
				PasswordHashSet: func() bool { return true },
				BindAddress:     tt.bindAddress,
			})
			err := m.Start()
			if err == nil {
				// Start will proceed to find cloudflared — it may fail there,
				// but it should NOT fail with a bind address error
				if tt.wantErr {
					t.Error("expected bind address error, but Start did not return one")
				}
				m.Stop()
				return
			}
			isBind := strings.Contains(err.Error(), "non-loopback")
			if tt.wantErr && !isBind {
				t.Errorf("expected bind address error, got: %v", err)
			}
			if !tt.wantErr && isBind {
				t.Errorf("did not expect bind address error, got: %v", err)
			}
		})
	}
}

func TestTunnelState_StartRejectsAutoDownloadDisabled(t *testing.T) {
	// Set PATH to empty dir so cloudflared won't be found
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", origPath)

	m := NewManager(ManagerConfig{
		PasswordHashSet:   func() bool { return true },
		AllowAutoDownload: false,
		SchmuxBinDir:      t.TempDir(),
	})
	err := m.Start()
	if err == nil {
		m.Stop()
		t.Fatal("expected error when cloudflared not found and auto-download disabled")
	}
	if !strings.Contains(err.Error(), "auto-download is disabled") {
		t.Errorf("error should mention auto-download is disabled, got: %v", err)
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
