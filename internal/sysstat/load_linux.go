//go:build linux

package sysstat

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readLoadAvg reads the host load average from /proc/loadavg.
func readLoadAvg() (LoadAvg, error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return LoadAvg{}, fmt.Errorf("read /proc/loadavg: %w", err)
	}
	return parseProcLoadavg(string(b))
}

// parseProcLoadavg parses Linux /proc/loadavg content.
// Format: "2.22 3.18 3.28 2/1234 56789" — only the first three fields matter.
// Lives here, not in load.go, so deadcode (which analyzes one GOOS at a
// time) sees it as reachable from its only caller.
func parseProcLoadavg(content string) (LoadAvg, error) {
	fields := strings.Fields(content)
	if len(fields) < 3 {
		return LoadAvg{}, fmt.Errorf("loadavg: expected at least 3 fields, got %d", len(fields))
	}
	one, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return LoadAvg{}, fmt.Errorf("loadavg: parsing 1m field: %w", err)
	}
	five, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return LoadAvg{}, fmt.Errorf("loadavg: parsing 5m field: %w", err)
	}
	fifteen, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return LoadAvg{}, fmt.Errorf("loadavg: parsing 15m field: %w", err)
	}
	return LoadAvg{One: one, Five: five, Fifteen: fifteen}, nil
}
