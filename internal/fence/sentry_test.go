package fence

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The preset changes the Foundation home on macOS, without adding host access.
func TestWrapSentryPreset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CFFIXED_USER_HOME", filepath.Join(home, "inherited"))
	for _, tc := range []struct {
		name    string
		presets []string
	}{
		{name: "disabled"},
		{name: "enabled", presets: []string{"sentry"}},
		{name: "duplicate", presets: []string{"sentry", "sentry"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := filepath.Join(t.TempDir(), "workspace with 'quotes'")
			dir := t.TempDir()
			if _, err := Wrap(context.Background(), Config{
				FenceCommand: "fence", WorkspacePath: ws, DataDir: dir, Presets: tc.presets,
			}, `printf '%s\n%s\n' "$CFFIXED_USER_HOME" "$HOME"`); err != nil {
				t.Fatalf("Wrap: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			var got settings
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			want := settings{
				Extends: "code",
				Network: &settingsNetwork{AllowedDomains: baselineDomains},
				Filesystem: settingsFilesystem{
					AllowRead: []string{filepath.Join(dir, "cmd.sh")}, AllowWrite: []string{ws},
				},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("settings = %s\nwant %+v with filesystem %+v and network %+v", raw, want, want.Filesystem, want.Network)
			}
			localHome := filepath.Join(ws, ".cache", "schmux-fence", "sentry-home")
			wantHome := filepath.Join(home, "inherited")
			if len(tc.presets) > 0 && runtime.GOOS == "darwin" {
				wantHome = localHome
				if info, err := os.Stat(localHome); err != nil || !info.IsDir() {
					t.Fatalf("Sentry home directory was not created: info=%v err=%v", info, err)
				}
			} else if _, err := os.Stat(localHome); !os.IsNotExist(err) {
				t.Fatalf("unexpected Sentry home without macOS preset: stat err=%v", err)
			}
			out, err := exec.Command("/bin/sh", filepath.Join(dir, "cmd.sh")).CombinedOutput()
			if err != nil {
				t.Fatalf("launch script: %v\n%s", err, out)
			}
			if want := wantHome + "\n" + home + "\n"; string(out) != want {
				t.Errorf("launch environment = %q, want %q", out, want)
			}
			script, err := os.ReadFile(filepath.Join(dir, "cmd.sh"))
			if err != nil {
				t.Fatal(err)
			}
			wantExports := 0
			if len(tc.presets) > 0 && runtime.GOOS == "darwin" {
				wantExports = 1
			}
			if got := strings.Count(string(script), "export CFFIXED_USER_HOME="); got != wantExports {
				t.Errorf("CFFIXED_USER_HOME exports = %d, want %d; script:\n%s", got, wantExports, script)
			}
		})
	}
}
