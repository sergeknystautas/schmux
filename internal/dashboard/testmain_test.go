package dashboard

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/preview"
)

func TestMain(m *testing.M) {
	// Replace lsof-based port lookups with lightweight TCP-connect alternatives
	// to avoid freezing the machine when many tests run in parallel.
	preview.LookupPortOwnerFunc = tcpLookupPortOwnerForTest
	preview.BuildPortOwnerCacheFunc = func() preview.PortOwnerCache {
		return make(preview.PortOwnerCache)
	}
	detectPortsForPIDFunc = func(pid int) []preview.ListeningPort {
		return nil
	}

	// Point HOME at a temp dir for the whole package run. schmuxdir.Get() falls
	// back to $HOME/.schmux whenever its dir is unset — which the per-test
	// schmuxdir.Set("") cleanups leave it. Without this, a spawn-log write during
	// such a window would land in the developer's real ~/.schmux.
	tmpHome, err := os.MkdirTemp("", "schmux-dashboard-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create temp home:", err)
		os.Exit(1)
	}
	os.Setenv("HOME", tmpHome)

	code := m.Run()
	_ = os.RemoveAll(tmpHome)
	os.Exit(code)
}

func tcpLookupPortOwnerForTest(port int) (int, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return 0, fmt.Errorf("nothing listening on port %d", port)
	}
	conn.Close()
	return os.Getpid(), nil
}
