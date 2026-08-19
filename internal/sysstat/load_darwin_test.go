//go:build darwin

package sysstat

import "testing"

func TestParseLoadavgBytes(t *testing.T) {
	// darwin vm.loadavg: three 32-bit fixed-point values with FSCALE=2048
	// (from xnu <sys/types.h>; verified against `uptime` on macOS 15).
	// Hardcoded here — NOT loadAvgFScale — so a wrong constant in the
	// implementation fails this test instead of round-tripping silently.
	// 2.0 -> 2*2048 = 4096 = 0x00001000 (little-endian: 00 10 00 00)
	mk := func(vals ...float64) []byte {
		b := make([]byte, 0, len(vals)*4)
		for _, v := range vals {
			u := uint32(v * 2048)
			b = append(b, byte(u), byte(u>>8), byte(u>>16), byte(u>>24))
		}
		return b
	}

	t.Run("typical", func(t *testing.T) {
		got, err := parseLoadavgBytes(mk(2.0, 3.0, 4.0))
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if got.One != 2.0 || got.Five != 3.0 || got.Fifteen != 4.0 {
			t.Errorf("got %+v, want {2 3 4}", got)
		}
	})

	t.Run("fractional", func(t *testing.T) {
		got, err := parseLoadavgBytes(mk(2.5, 0.25, 0.0))
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if got.One != 2.5 || got.Five != 0.25 || got.Fifteen != 0.0 {
			t.Errorf("got %+v, want {2.5 0.25 0}", got)
		}
	})

	t.Run("short buffer", func(t *testing.T) {
		if _, err := parseLoadavgBytes(mk(1.0, 2.0)); err == nil {
			t.Fatal("want error for 8-byte buffer")
		}
	})
}
