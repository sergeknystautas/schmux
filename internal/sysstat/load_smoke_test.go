package sysstat

import "testing"

func TestLoadProbeReadSmoke(t *testing.T) {
	avg, err := NewLoadProbe().Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if avg.One < 0 || avg.Five < 0 || avg.Fifteen < 0 {
		t.Errorf("negative load average: %+v", avg)
	}
	// A load average above 10000 on any dev/CI machine means the parser
	// is wrong (e.g. fixed-point scaling bug), not that the host is busy.
	if avg.One > 10000 {
		t.Errorf("implausible load average: %+v", avg)
	}
}
