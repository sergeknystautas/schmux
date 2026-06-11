//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"testing"
	"time"
)

func TestBroadcastBuildMonitor_SendsTypedMessage(t *testing.T) {
	srv, _, _ := newTestServer(t)

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	// Consume the initial sessions snapshot
	readDashboardMsg(t, conn, 2*time.Second)

	srv.BroadcastBuildMonitor()

	msg := readDashboardMsg(t, conn, 3*time.Second)
	if msg["type"] != "build_monitor_updated" {
		t.Errorf("message type = %q, want build_monitor_updated", msg["type"])
	}
}
