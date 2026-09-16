package fence

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// Exercise Foundation itself, including when the test runner is already fenced
// and cannot nest Seatbelt. This fixture writes a cache probe, not a Sentry event.
func TestSentryFoundationHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("CFFIXED_USER_HOME", filepath.Join(root, "inherited"))
	ws := filepath.Join(root, "workspace")
	dir := filepath.Join(root, "session")
	command := sentryFoundationFixture(t, root)
	if _, err := Wrap(context.Background(), Config{
		FenceCommand: "fence", WorkspacePath: ws, DataDir: dir, Presets: []string{"sentry"},
	}, command); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/bin/sh", filepath.Join(dir, "cmd.sh")).CombinedOutput()
	if err != nil {
		t.Fatalf("Foundation fixture: %v\n%s", err, out)
	}
	assertSentryFoundationHome(t, ws, out)
}

// A separate live gate proves the write boundary. A temporary HOME outside the
// system temp directory avoids both touching user caches and the code template's
// blanket temp-directory write allowance making the denial checks vacuous.
func TestSentryHomeLiveSandbox(t *testing.T) {
	if os.Getenv("FENCE_SANDBOX") != "" {
		t.Skip("already inside a fence; Seatbelt cannot nest")
	}
	fenceBin, err := exec.LookPath("fence")
	if err != nil {
		t.Skip("fence not installed")
	}
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(filepath.Join(realHome, ".schmux"), "sentry-home-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	ws := filepath.Join(root, "workspace")
	for _, name := range []string{"io.sentry", "unrelated-cache"} {
		if err := os.MkdirAll(filepath.Join(home, "Library", "Caches", name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("CFFIXED_USER_HOME", "")
	command := sentryFoundationFixture(t, root)
	// Explicit host paths must fail even though Foundation's redirected cache
	// is writable. Abort on an unexpected successful write, preserving stderr.
	command += `
for cache in io.sentry unrelated-cache; do
  if (printf probe > "$HOME/Library/Caches/$cache/probe"); then
    echo "unexpected host-cache write: $cache" >&2
    exit 1
  fi
done
`
	wrapped, err := Wrap(context.Background(), Config{
		FenceCommand: fenceBin, WorkspacePath: ws,
		DataDir: filepath.Join(root, "session"), Presets: []string{"sentry"},
	}, command)
	if err != nil {
		t.Fatal(err)
	}
	run := exec.Command("/bin/sh", "-c", wrapped)
	run.Dir = ws
	out, err := run.Output()
	if err != nil {
		var stderr []byte
		if exit, ok := err.(*exec.ExitError); ok {
			stderr = exit.Stderr
		}
		t.Fatalf("fenced Foundation fixture: %v\nstdout: %s\nstderr: %s", err, out, stderr)
	}
	assertSentryFoundationHome(t, ws, out)
	for _, name := range []string{"io.sentry", "unrelated-cache"} {
		if _, err := os.Stat(filepath.Join(home, "Library", "Caches", name, "probe")); !os.IsNotExist(err) {
			t.Errorf("host cache %s: denied write landed, stat err=%v", name, err)
		}
	}
}

func sentryFoundationFixture(t *testing.T, root string) string {
	t.Helper()
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		t.Skip("swiftc not installed")
	}
	source := filepath.Join(root, "foundation.swift")
	if err := os.WriteFile(source, []byte(`import Foundation
let fm = FileManager.default
let cache = fm.urls(for: .cachesDirectory, in: .userDomainMask)[0]
let support = fm.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
let queue = cache.appendingPathComponent("io.sentry/test-sha")
try fm.createDirectory(at: queue, withIntermediateDirectories: true)
try Data("cache probe".utf8).write(to: queue.appendingPathComponent("probe"))
let result = ["cache": cache.path, "support": support.path]
print(String(data: try JSONSerialization.data(withJSONObject: result), encoding: .utf8)!)
`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "foundation-probe")
	compile := exec.Command(swiftc, "-module-cache-path", filepath.Join(root, "modules"), source, "-o", bin)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile Foundation fixture: %v\n%s", err, out)
	}
	return shellutil.Quote(bin)
}

func assertSentryFoundationHome(t *testing.T, ws string, out []byte) {
	t.Helper()
	var got map[string]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("Foundation paths: %v\n%s", err, out)
	}
	home := filepath.Join(ws, ".cache", "schmux-fence", "sentry-home")
	for key, suffix := range map[string]string{"cache": "Caches", "support": "Application Support"} {
		if want := filepath.Join(home, "Library", suffix); got[key] != want {
			t.Errorf("Foundation %s = %q, want %q", key, got[key], want)
		}
	}
	path := filepath.Join(home, "Library", "Caches", "io.sentry", "test-sha", "probe")
	if data, err := os.ReadFile(path); err != nil || string(data) != "cache probe" {
		t.Errorf("cache probe %s: data=%q err=%v", path, data, err)
	}
}
