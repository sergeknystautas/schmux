//go:build darwin

package sysstat

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// readLoadAvg reads the host load average via the vm.loadavg sysctl.
func readLoadAvg() (LoadAvg, error) {
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return LoadAvg{}, fmt.Errorf("sysctl vm.loadavg: %w", err)
	}
	return parseLoadavgBytes(b)
}

// loadAvgFScale is darwin's fixed-point scale for vm.loadavg
// (FSCALE = 1<<11, from xnu's <sys/types.h>). Verified against `uptime`:
// a raw ldavg of 12670 reads as 6.19 with this divisor.
const loadAvgFScale = 2048

// parseLoadavgBytes parses darwin's vm.loadavg sysctl value: struct
// loadavg { fixpt_t ldavg[3]; long fscale; } — three 32-bit fixed-point
// numbers with scale loadAvgFScale. (The struct continues with fscale;
// we only need the first 12 bytes.) Lives here, not in load.go, so
// deadcode (which analyzes one GOOS at a time) sees it as reachable
// from its only caller.
func parseLoadavgBytes(b []byte) (LoadAvg, error) {
	if len(b) < 12 {
		return LoadAvg{}, fmt.Errorf("loadavg: expected 12 bytes, got %d", len(b))
	}
	return LoadAvg{
		One:     float64(binary.LittleEndian.Uint32(b[0:4])) / loadAvgFScale,
		Five:    float64(binary.LittleEndian.Uint32(b[4:8])) / loadAvgFScale,
		Fifteen: float64(binary.LittleEndian.Uint32(b[8:12])) / loadAvgFScale,
	}, nil
}
