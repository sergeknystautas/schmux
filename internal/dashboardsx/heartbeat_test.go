//go:build !nodashboardsx

package dashboardsx

import (
	"testing"
	"time"
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
