package main

import (
	"testing"
)

func TestParseDaemonRunFlags(t *testing.T) {
	tests := []struct {
		args      []string
		wantProxy bool
		wantBg    bool
	}{
		{[]string{}, false, false},
		{[]string{"--dev-proxy"}, true, false},
		{[]string{"--background"}, false, true},
		{[]string{"--dev-proxy", "--background"}, true, true},
		{[]string{"--background", "--dev-proxy"}, true, true},
	}

	for _, tt := range tests {
		gotProxy, gotBg := parseDaemonRunFlags(tt.args)
		if gotProxy != tt.wantProxy || gotBg != tt.wantBg {
			t.Errorf("parseDaemonRunFlags(%v) = (%v, %v), want (%v, %v)",
				tt.args, gotProxy, gotBg, tt.wantProxy, tt.wantBg)
		}
	}
}
