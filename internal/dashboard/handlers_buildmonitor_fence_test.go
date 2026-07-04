//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func fenceDetected() detect.DependencyReport {
	return detect.DependencyReport{Statuses: []detect.DependencyStatus{
		{Dependency: detect.Dependency{ID: "fence"}, Detected: true, Command: "fence"},
	}}
}

func TestBuildMonitorFenceOptions(t *testing.T) {
	tests := []struct {
		name        string
		toggle      bool
		fenceMode   string
		report      detect.DependencyReport
		wantFence   bool
		wantCommand string
	}{
		{"toggle off", false, "", fenceDetected(), false, ""},
		{"on + available + enabled", true, "", fenceDetected(), true, "fence"},
		{"on + fence unavailable", true, "", detect.DependencyReport{}, false, ""},
		{"on + mode disabled", true, config.FenceModeDisabled, fenceDetected(), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, cfg, _ := newTestServer(t)
			cfg.FenceBuildMonitor = tt.toggle
			cfg.FenceMode = tt.fenceMode
			server.depReport = tt.report

			fence, command := server.buildMonitorFenceOptions()
			if fence != tt.wantFence {
				t.Errorf("fence = %v, want %v", fence, tt.wantFence)
			}
			if command != tt.wantCommand {
				t.Errorf("command = %q, want %q", command, tt.wantCommand)
			}
		})
	}
}
