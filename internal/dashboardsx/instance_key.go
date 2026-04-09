//go:build !nodashboardsx

package dashboardsx

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// EnsureInstanceKey reads the existing instance key or generates a new one.
// Returns the 64-char hex string (32 random bytes).
func EnsureInstanceKey() (string, error) {
	if err := EnsureDir(); err != nil {
		return "", fmt.Errorf("failed to create dashboardsx directory: %w", err)
	}

	keyPath := InstanceKeyPath()

	// Try to read existing key
	data, err := os.ReadFile(keyPath)
	if err == nil {
		key := strings.TrimSpace(string(data))
		if len(key) == 64 {
			return key, nil
		}
	}

	// Generate new key
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	key := hex.EncodeToString(buf)

	if err := os.WriteFile(keyPath, []byte(key+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write instance key: %w", err)
	}

	return key, nil
}
