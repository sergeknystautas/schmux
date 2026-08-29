package fence

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpinePresetLiveSandbox runs the real fence binary against the settings
// the spine preset generates and proves the grant's exact edges behaviorally:
// Spine's state-dir workload (create, append, replace-by-rename, chmod of files
// and the dir itself, UUID-style root files) succeeds, while a sibling
// Application Support dir, an unrelated home file, the Spine.app bundle, and a
// symlink escaping the granted dir all stay denied. HOME is pointed at a temp
// dir so the test never touches the user's real Spine state; fence expands the
// code template's ~-relative rules against the same HOME it inherits.
//
// Skipped off-macOS, when fence is not installed, and when the test itself
// runs inside a fence (Seatbelt cannot nest — sandbox_apply is denied).
func TestSpinePresetLiveSandbox(t *testing.T) {
	fenceBin, err := exec.LookPath("fence")
	if err != nil {
		t.Skip("fence not installed")
	}
	if os.Getenv("FENCE_SANDBOX") != "" {
		t.Skip("already inside a fence; Seatbelt cannot nest")
	}

	// The fake home must NOT live under TempDir: fence's baseline allows the
	// system temp dir, so denial probes placed there pass vacuously (observed:
	// sibling/home/symlink writes all landed when the fake home was under
	// /var/folders). Use a scratch dir under the real home instead — with HOME
	// overridden to the fake home, the code template's ~-relative allows
	// expand against the fake home, leaving this absolute path default-denied.
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(filepath.Join(realHome, ".schmux"), "fence-live-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	ws := filepath.Join(root, "ws")
	dataDir := filepath.Join(root, "sess")
	appSupport := filepath.Join(home, "Library", "Application Support")
	spineDir := filepath.Join(appSupport, "Spine")
	siblingDir := filepath.Join(appSupport, "OtherApp")
	// Pre-create what exists in reality before a fenced session starts: the
	// state dirs themselves. The preset does not grant creating Spine's dir —
	// Spine has always run at least once (unfenced) before an export.
	for _, d := range []string{ws, spineDir, siblingDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home) // os.UserConfigDir() -> home/Library/Application Support

	escapeTarget := filepath.Join(home, "escape-target.txt")
	script := `
run() { name=$1; shift; if "$@" >/dev/null 2>&1; then echo "OK:$name"; else echo "NO:$name"; fi; }
wr() { name=$1; path=$2; if sh -c ': > "$1"' _ "$path" 2>/dev/null; then echo "OK:$name"; else echo "NO:$name"; fi; }
S="$HOME/Library/Application Support/Spine"
wr create "$S/spine.log"
run append sh -c 'echo line >> "$1"' _ "$S/spine.log"
run mkdir mkdir -p "$S/settings"
wr settings-create "$S/settings/start-1.json"
run replace mv "$S/settings/start-1.json" "$S/settings/start-1.json.bak"
wr uuid-root "$S/a0c05e25-b7b5-43f2-8161-0e3740e6d240"
run chmod-file chmod 600 "$S/spine.log"
run chmod-dir chmod 700 "$S"
wr sibling "$HOME/Library/Application Support/OtherApp/f.txt"
wr home-file "$HOME/unrelated.txt"
run make-symlink ln -s "$HOME/escape-target.txt" "$S/escape-link"
wr symlink-escape "$S/escape-link"
`
	cfg := Config{
		FenceCommand:  fenceBin,
		WorkspacePath: ws,
		Presets:       []string{"spine"},
		DataDir:       dataDir,
	}
	cmdStr, err := Wrap(context.Background(), cfg, script)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	run := exec.Command("/bin/sh", "-c", cmdStr)
	run.Dir = ws
	run.Env = append(os.Environ(), "HOME="+home)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("fenced script failed: %v\n%s", err, out)
	}

	want := map[string]bool{
		// Spine's real launch workload inside its own state dir: allowed.
		"create": true, "append": true, "mkdir": true, "settings-create": true,
		"replace": true, "uuid-root": true, "chmod-file": true, "chmod-dir": true,
		// Creating a symlink inside the granted dir is a write there: allowed.
		"make-symlink": true,
		// Everything outside the granted dir: denied.
		"sibling": false, "home-file": false,
		// fence resolves the target, so a symlink inside the granted dir must
		// not open a write path to a file outside it.
		"symlink-escape": false,
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if name, ok := strings.CutPrefix(line, "OK:"); ok {
			got[name] = true
		} else if name, ok := strings.CutPrefix(line, "NO:"); ok {
			got[name] = false
		}
	}
	for name, wantOK := range want {
		gotOK, ran := got[name]
		if !ran {
			t.Errorf("case %q produced no marker; output:\n%s", name, out)
			continue
		}
		if gotOK != wantOK {
			t.Errorf("case %q = allowed:%v, want allowed:%v; output:\n%s", name, gotOK, wantOK, out)
		}
	}
	// The denials must have stuck on disk too, not just in exit codes.
	for _, p := range []string{
		filepath.Join(siblingDir, "f.txt"),
		filepath.Join(home, "unrelated.txt"),
		escapeTarget,
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s exists — a denied write landed (stat err = %v)", p, err)
		}
	}
}

// TestSpinePresetLiveSpineAppDenied proves writes inside the Spine.app bundle
// stay denied for a spine-preset session. Separate from the main live test
// because it touches the real /Applications and only runs where Spine is
// installed. Self-cleaning if fence ever regressed and allowed the write.
func TestSpinePresetLiveSpineAppDenied(t *testing.T) {
	fenceBin, err := exec.LookPath("fence")
	if err != nil {
		t.Skip("fence not installed")
	}
	if os.Getenv("FENCE_SANDBOX") != "" {
		t.Skip("already inside a fence; Seatbelt cannot nest")
	}
	shared := "/Applications/Spine.app/Contents/MacOS/shared"
	if _, err := os.Stat(shared); err != nil {
		t.Skip("Spine.app not installed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(shared, "schmux-fence-test-probe")
	t.Cleanup(func() { os.Remove(probe) })
	cmdStr, err := Wrap(context.Background(), Config{
		FenceCommand:  fenceBin,
		WorkspacePath: ws,
		Presets:       []string{"spine"},
		DataDir:       filepath.Join(root, "sess"),
	}, `sh -c ': > "$1"' _ `+"'"+probe+"'"+` && echo WROTE || echo DENIED`)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	run := exec.Command("/bin/sh", "-c", cmdStr)
	run.Dir = ws
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("fenced probe failed to run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "DENIED") || strings.Contains(string(out), "WROTE") {
		t.Errorf("write inside Spine.app was not denied:\n%s", out)
	}
	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Errorf("probe file exists inside Spine.app (stat err = %v)", err)
	}
}
