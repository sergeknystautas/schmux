package fence

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// The preset never redirects the Foundation home: CFFIXED_USER_HOME is not
// exported and no sentry-home dir is created, with or without the preset.
func TestWrapSentryPreset(t *testing.T) {
	t.Setenv("CFFIXED_USER_HOME", "/inherited/should/not/matter")
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
			}, "true"); err != nil {
				t.Fatalf("Wrap: %v", err)
			}
			script, err := os.ReadFile(filepath.Join(dir, "cmd.sh"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(script), "CFFIXED_USER_HOME") {
				t.Errorf("cmd.sh must not export CFFIXED_USER_HOME:\n%s", script)
			}
			if _, err := os.Stat(filepath.Join(ws, ".cache", "schmux-fence", "sentry-home")); !os.IsNotExist(err) {
				t.Errorf("sentry-home dir must not be created: stat err=%v", err)
			}
		})
	}
}

// The preset adds exactly the three Sentry API hosts, once, after the baseline.
func TestWrapSentryPresetAddsDomains(t *testing.T) {
	for _, presets := range [][]string{{"sentry"}, {"sentry", "sentry"}} {
		dir := t.TempDir()
		if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: presets, DataDir: dir}, "true"); err != nil {
			t.Fatalf("Wrap(%v): %v", presets, err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
		var s settings
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		want := append(append([]string{}, baselineDomains...), "sentry.io", "us.sentry.io", "de.sentry.io")
		if !slices.Equal(s.Network.AllowedDomains, want) {
			t.Errorf("presets %v: allowedDomains = %v, want %v", presets, s.Network.AllowedDomains, want)
		}
		for _, d := range sentryDomains {
			if strings.Contains(d, "*") {
				t.Errorf("wildcard domain %q must not be added by sentry preset", d)
			}
		}
	}
}

func TestWrapSentryPresetWritesShimAndPath(t *testing.T) {
	orig := sentryLookPathFn
	sentryLookPathFn = func() string { return "/opt/homebrew/bin/sentry" }
	defer func() { sentryLookPathFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess with 'quote'")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"sentry"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	shimDir := filepath.Join(dir, "sentry-shim")
	shimPath := filepath.Join(shimDir, "sentry")
	fi, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("sentry shim not written: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("sentry shim mode = %o, want executable", fi.Mode().Perm())
	}
	shim, _ := os.ReadFile(shimPath)
	for _, want := range []string{"/opt/homebrew/bin/sentry", "export NODE_USE_ENV_PROXY=1", "export SENTRY_CLI_NO_TELEMETRY=1"} {
		if !strings.Contains(string(shim), want) {
			t.Errorf("shim missing %q\nshim=%s", want, shim)
		}
	}
	if strings.Contains(string(shim), "NODE_OPTIONS") {
		t.Errorf("sentry shim must not touch NODE_OPTIONS:\n%s", shim)
	}
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	wantPath := "export PATH=" + shellutil.Quote(shimDir) + ":$PATH"
	if !strings.Contains(string(cmd), wantPath) {
		t.Errorf("cmd.sh missing PATH prepend %q\ncmd=%s", wantPath, cmd)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Filesystem.AllowRead, shimDir) {
		t.Errorf("allowRead = %v, want to contain %s", s.Filesystem.AllowRead, shimDir)
	}
	if slices.Contains(s.Filesystem.DenyWrite, shimDir) {
		t.Errorf("denyWrite = %v, must not contain the DataDir shim dir", s.Filesystem.DenyWrite)
	}
	if slices.Contains(s.Filesystem.AllowWrite, shimDir) {
		t.Errorf("allowWrite = %v, must not contain the shim dir", s.Filesystem.AllowWrite)
	}
}

// Run the generated shim against a stub `sentry` that echoes the env and
// args: both variables are exported and arguments pass through untouched.
func TestSentryShimSetsProxyEnv(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "sentry")
	stubScript := "#!/bin/sh\n" +
		"echo \"proxy=$NODE_USE_ENV_PROXY telemetry=$SENTRY_CLI_NO_TELEMETRY\"\n" +
		"for a in \"$@\"; do echo \"arg=$a\"; done\n"
	if err := os.WriteFile(stub, []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(t.TempDir(), "sentry")
	if err := os.WriteFile(shimPath, []byte(sentryShimScript(stub)), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(shimPath, "project", "list", "--org", "my org").CombinedOutput()
	if err != nil {
		t.Fatalf("shim run: %v\n%s", err, out)
	}
	want := "proxy=1 telemetry=1\narg=project\narg=list\narg=--org\narg=my org\n"
	if string(out) != want {
		t.Errorf("shim output = %q, want %q", out, want)
	}
}

// No host sentry: no shim, no PATH line, domains still added, launch succeeds.
func TestWrapSentryPresetNoSentrySkipsShim(t *testing.T) {
	orig := sentryLookPathFn
	sentryLookPathFn = func() string { return "" }
	defer func() { sentryLookPathFn = orig }()

	dir := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"sentry"}, DataDir: dir}, "true"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sentry-shim")); !os.IsNotExist(err) {
		t.Errorf("sentry-shim dir must not exist without host sentry: %v", err)
	}
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	if strings.Contains(string(cmd), "sentry-shim") {
		t.Errorf("cmd.sh must not reference the shim dir:\n%s", cmd)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Network.AllowedDomains, "us.sentry.io") {
		t.Errorf("domains must be added even without the shim: %v", s.Network.AllowedDomains)
	}
}
