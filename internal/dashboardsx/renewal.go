package dashboardsx

import (
	"context"
	"time"
)

const (
	renewalCheckInterval = 24 * time.Hour
	renewalThresholdDays = 30
)

// StartAutoRenewal runs a background loop that checks cert expiry daily
// and auto-renews when the certificate is within 30 days of expiration.
// The goroutine exits when ctx is cancelled.
func StartAutoRenewal(ctx context.Context, client *Client, email string) {
	for {
		timer := time.NewTimer(renewalCheckInterval)
		select {
		case <-timer.C:
			checkAndRenew(client, email)
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

// checkAndRenew checks cert expiry and renews if needed.
func checkAndRenew(client *Client, email string) {
	expiry, err := GetCertExpiry()
	if err != nil {
		if pkgLogger != nil {
			pkgLogger.Error("auto-renewal: failed to read cert expiry", "err", err)
		}
		return
	}

	daysLeft := int(time.Until(expiry).Hours() / 24)
	if daysLeft > renewalThresholdDays {
		return
	}

	if pkgLogger != nil {
		pkgLogger.Info("auto-renewal: cert expiring soon, renewing", "days_left", daysLeft)
	}

	provider, err := NewServiceDNSProvider(client)
	if err != nil {
		if pkgLogger != nil {
			pkgLogger.Error("auto-renewal: failed to create DNS provider", "err", err)
		}
		return
	}

	if err := ProvisionCert(client.Code, email, false, provider, nil); err != nil {
		if pkgLogger != nil {
			pkgLogger.Error("auto-renewal: failed", "err", err)
		}
		return
	}

	if pkgLogger != nil {
		pkgLogger.Info("auto-renewal: certificate renewed successfully")
	}
}
