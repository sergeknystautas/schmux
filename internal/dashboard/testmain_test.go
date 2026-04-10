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
	os.Exit(m.Run())
}

func tcpLookupPortOwnerForTest(port int) (int, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return 0, fmt.Errorf("nothing listening on port %d", port)
	}
	conn.Close()
	return os.Getpid(), nil
}
