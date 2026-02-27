package dashboardsx

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/charmbracelet/log"
)

// pkgLogger is the package-level logger for dashboardsx operations.
// Set via SetLogger from the daemon initialization.
var pkgLogger *log.Logger

// SetLogger sets the package-level logger for dashboardsx operations.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

const (
	heartbeatBaseInterval = 24 * time.Hour
	heartbeatJitter       = 2 * time.Hour // ±2 hours
)

// StartHeartbeat runs a background heartbeat loop that sends keep-alive
// signals to the dashboard.sx service. It sends one immediately, then
// every 24h ± 2h (randomized to prevent surveillance).
// The goroutine exits when ctx is cancelled.
func StartHeartbeat(ctx context.Context, client *Client) {
	// Send initial heartbeat immediately
	if err := client.Heartbeat(); err != nil {
		if pkgLogger != nil {
			pkgLogger.Error("heartbeat failed", "err", err)
		}
	} else {
		if pkgLogger != nil {
			pkgLogger.Info("heartbeat sent")
		}
	}

	for {
		interval := heartbeatInterval()
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			if err := client.Heartbeat(); err != nil {
				if pkgLogger != nil {
					pkgLogger.Error("heartbeat failed", "err", err)
				}
			}
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

// heartbeatInterval returns the base interval ± random jitter.
func heartbeatInterval() time.Duration {
	// Generate random jitter in range [-heartbeatJitter, +heartbeatJitter]
	jitterRange := int64(heartbeatJitter) * 2
	n, err := rand.Int(rand.Reader, big.NewInt(jitterRange))
	if err != nil {
		return heartbeatBaseInterval
	}
	jitter := time.Duration(n.Int64()) - heartbeatJitter
	return heartbeatBaseInterval + jitter
}
