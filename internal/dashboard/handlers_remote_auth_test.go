package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/tunnel"
)

func TestValidateRemoteCookie_ExpiredCookie(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	// Simulate tunnel connect to generate session secret
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Create a cookie with a timestamp 25 hours in the past
	oldTimestamp := fmt.Sprintf("%d", time.Now().Add(-25*time.Hour).Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(oldTimestamp))
	sig := hex.EncodeToString(mac.Sum(nil))

	cookieValue := oldTimestamp + "." + sig

	if server.validateRemoteCookie(cookieValue) {
		t.Error("expected expired cookie to be rejected")
	}
}

func TestValidateRemoteCookie_FreshCookie(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Create a cookie with the current timestamp
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))

	cookieValue := nowStr + "." + sig

	if !server.validateRemoteCookie(cookieValue) {
		t.Error("expected fresh cookie to be accepted")
	}
}
