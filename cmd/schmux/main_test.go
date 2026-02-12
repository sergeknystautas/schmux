package main

import (
	"testing"
)

func TestParseDaemonRunFlags(t *testing.T) {
	tests := []struct {
		args      []string
		wantProxy bool
		wantBg    bool
		wantDev   bool
	}{
		{[]string{}, false, false, false},
		{[]string{"--dev-proxy"}, true, false, false},
		{[]string{"--background"}, false, true, false},
		{[]string{"--dev-proxy", "--background"}, true, true, false},
		{[]string{"--background", "--dev-proxy"}, true, true, false},
		{[]string{"--dev-mode"}, true, false, true}, // --dev-mode implies --dev-proxy
		{[]string{"--dev-mode", "--background"}, true, true, true},
	}

	for _, tt := range tests {
		gotProxy, gotBg, gotDev := parseDaemonRunFlags(tt.args)
		if gotProxy != tt.wantProxy || gotBg != tt.wantBg || gotDev != tt.wantDev {
			t.Errorf("parseDaemonRunFlags(%v) = (%v, %v, %v), want (%v, %v, %v)",
				tt.args, gotProxy, gotBg, gotDev, tt.wantProxy, tt.wantBg, tt.wantDev)
		}
	}
}
