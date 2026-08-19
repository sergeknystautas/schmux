package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGetBuildMonitorIntervalSeconds(t *testing.T) {
	tests := []struct {
		name string
		bm   *BuildMonitorConfig
		want time.Duration
	}{
		{"nil config defaults to 60s", nil, 60 * time.Second},
		{"zero defaults to 60s", &BuildMonitorConfig{}, 60 * time.Second},
		{"floor of 15s", &BuildMonitorConfig{IntervalSeconds: 5}, 15 * time.Second},
		{"explicit value", &BuildMonitorConfig{IntervalSeconds: 300}, 300 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{ConfigData: ConfigData{BuildMonitor: tt.bm}}
			if got := c.GetBuildMonitorIntervalSeconds(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Legacy minutes-based "interval" keys in existing configs must be ignored,
// landing installs on the new 60s default.
func TestLegacyIntervalKeyIgnored(t *testing.T) {
	c := &Config{}
	if err := json.Unmarshal([]byte(`{"build_monitor":{"enabled":true,"interval":5}}`), c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := c.GetBuildMonitorIntervalSeconds(); got != 60*time.Second {
		t.Errorf("legacy interval leaked through: got %v, want 60s", got)
	}
}
