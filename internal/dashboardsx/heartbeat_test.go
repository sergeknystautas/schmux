//go:build !nodashboardsx

package dashboardsx

import (
	"bytes"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

func TestHeartbeatInterval(t *testing.T) {
	// Run multiple times to check randomization stays within bounds
	for i := 0; i < 100; i++ {
		interval := heartbeatInterval()
		minInterval := heartbeatBaseInterval - heartbeatJitter
		maxInterval := heartbeatBaseInterval + heartbeatJitter

		if interval < minInterval || interval > maxInterval {
			t.Errorf("heartbeatInterval() = %v, want between %v and %v", interval, minInterval, maxInterval)
		}
	}
}

func TestLegoLogAdapter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{Level: log.DebugLevel})

	adapter := &legoLogAdapter{l: logger}

	tests := []struct {
		name      string
		input     string
		wantLevel string
		wantMsg   string
	}{
		{"info prefix", "[INFO] [example.com] acme: obtaining cert", "INFO", "[example.com] acme: obtaining cert"},
		{"warn prefix", "[WARN] rate limited", "WARN", "rate limited"},
		{"no prefix", "some other message", "INFO", "some other message"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			adapter.Printf("%s", tc.input)
			output := buf.String()
			if !bytes.Contains([]byte(output), []byte(tc.wantLevel)) {
				t.Errorf("expected level %s in output %q", tc.wantLevel, output)
			}
			if !bytes.Contains([]byte(output), []byte(tc.wantMsg)) {
				t.Errorf("expected message %q in output %q", tc.wantMsg, output)
			}
		})
	}
}

func TestHeartbeatInterval_NotConstant(t *testing.T) {
	// Verify we get at least two different values over 20 samples
	seen := make(map[time.Duration]bool)
	for i := 0; i < 20; i++ {
		seen[heartbeatInterval()] = true
	}
	if len(seen) < 2 {
		t.Error("heartbeatInterval() returned the same value 20 times; expected randomization")
	}
}
