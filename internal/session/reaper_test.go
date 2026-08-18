package session

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

// spawnTree starts script under /bin/sh in its own process group and
// guarantees cleanup. Returns the sh PID (the tree root).
func spawnTree(t *testing.T, script string) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawnTree: %v", err)
	}
	t.Cleanup(func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
	})
	return cmd.Process.Pid
}

// requireProcTools skips the test when ps/pgrep cannot run. schmux runs
// coding agents inside the fence sandbox, which denies process listing
// (ps exec fails, or pgrep reports "Cannot get process list"), so these
// tests would fail falsely for every fenced agent or developer. CI and
// unsandboxed dev machines run them normally.
func requireProcTools(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("ps", "-o", "pid=", "-p", "1").CombinedOutput(); err != nil {
		t.Skipf("ps unavailable (sandboxed environment): %v (%s)", err, strings.TrimSpace(string(out)))
	}
	// PID 1 always has children, so any pgrep failure here means the tool
	// cannot list processes, not that there were no matches.
	if out, err := exec.Command("pgrep", "-P", "1").CombinedOutput(); err != nil {
		t.Skipf("pgrep unavailable (sandboxed environment): %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

func testReaper() *reaper {
	r := newReaper(log.NewWithOptions(io.Discard, log.Options{}))
	r.poll = 20 * time.Millisecond
	return r
}

// awaitEnumerate retries enumerate until it returns at least min procs or the
// deadline passes — children take a few ms to spawn after cmd.Start.
func awaitEnumerate(t *testing.T, r *reaper, root, min int) []procIdent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		procs, err := r.enumerate(context.Background(), root)
		if err == nil && len(procs) >= min {
			return procs
		}
		if time.Now().After(deadline) {
			t.Fatalf("enumerate(%d) never reached %d procs: %v (%v)", root, min, procs, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestEnumerate_FindsDescendants(t *testing.T) {
	requireProcTools(t)
	root := spawnTree(t, "sleep 60 & sleep 60 & wait")

	procs := awaitEnumerate(t, testReaper(), root, 3) // sh + two sleeps
	for _, p := range procs {
		if p.PGID != root {
			t.Errorf("pid %d: pgid %d, want %d (Setpgid root)", p.PID, p.PGID, root)
		}
		if p.Start == "" {
			t.Errorf("pid %d: empty start time", p.PID)
		}
	}
}

func TestEnumerate_DeadRootErrors(t *testing.T) {
	requireProcTools(t)
	root := spawnTree(t, "true") // exits immediately
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := testReaper().enumerate(context.Background(), root); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected error enumerating a dead root")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAliveValidated_StartTimeMismatch(t *testing.T) {
	requireProcTools(t)
	// Our own live PID with a bogus start time must read as NOT alive:
	// this is the PID-reuse guard.
	p := procIdent{PID: os.Getpid(), PGID: os.Getpid(), Start: "bogus"}
	if p.aliveValidated(context.Background()) {
		t.Fatal("mismatched start time must invalidate the PID")
	}
	// And with the real start time it reads alive.
	start, err := procStartTime(context.Background(), os.Getpid())
	if err != nil {
		t.Fatalf("procStartTime: %v", err)
	}
	p.Start = start
	if !p.aliveValidated(context.Background()) {
		t.Fatal("matching start time must validate a live PID")
	}
}

func TestAliveValidated_ZombieIsExited(t *testing.T) {
	requireProcTools(t)
	if runtime.GOOS != "linux" {
		t.Skip("Linux retains an un-waited child as a visible zombie")
	}
	root := spawnTree(t, "true")
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := procState(context.Background(), root)
		if err == nil && strings.HasPrefix(state, "Z") {
			start, err := procStartTime(context.Background(), root)
			if err != nil {
				t.Fatalf("procStartTime: %v", err)
			}
			p := procIdent{PID: root, PGID: root, Start: start}
			if p.aliveValidated(context.Background()) {
				t.Fatal("zombie must count as exited")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d never became a zombie: state=%q err=%v", root, state, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func enumerateAfter(t *testing.T, r *reaper, root int) []procIdent {
	t.Helper()
	return awaitEnumerate(t, r, root, 1)
}

func TestReap_GracefulExit(t *testing.T) {
	requireProcTools(t)
	r := testReaper()
	root := spawnTree(t, "sleep 0.4")
	procs := enumerateAfter(t, r, root)

	report, err := r.reap(context.Background(), procs, 5*time.Second)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if !report.GracefulExit {
		t.Error("expected graceful exit")
	}
	if len(report.SigtermGroups)+len(report.SigtermPIDs)+len(report.SigkillGroups)+len(report.SigkillPIDs) != 0 {
		t.Errorf("no signals expected, got %+v", report)
	}
}

func TestReap_SigtermEscalation(t *testing.T) {
	requireProcTools(t)
	r := testReaper()
	root := spawnTree(t, "sleep 60") // outlives grace; dies on default SIGTERM
	procs := enumerateAfter(t, r, root)

	report, err := r.reap(context.Background(), procs, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if report.GracefulExit {
		t.Error("expected escalation, not graceful exit")
	}
	if len(report.SigtermGroups) == 0 {
		t.Error("expected a SIGTERM group signal")
	}
	if len(report.SigkillGroups)+len(report.SigkillPIDs) != 0 {
		t.Errorf("SIGKILL should not have been needed: %+v", report)
	}
}

func TestReap_SigkillEscalation_AndIsolation(t *testing.T) {
	requireProcTools(t)
	r := testReaper()
	// Parent and grandchild both ignore TERM and busy-loop (the codex shape).
	root := spawnTree(t, `trap '' TERM; /bin/sh -c 'trap "" TERM; while :; do :; done' & while :; do :; done`)
	procs := awaitEnumerate(t, r, root, 2) // parent + grandchild
	// Unrelated process in its own group must survive.
	bystander := spawnTree(t, "sleep 60")

	report, err := r.reap(context.Background(), procs, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if len(report.SigkillGroups)+len(report.SigkillPIDs) == 0 {
		t.Errorf("expected SIGKILL escalation: %+v", report)
	}
	for _, p := range procs {
		if p.aliveValidated(context.Background()) {
			t.Errorf("pid %d still alive after reap", p.PID)
		}
	}
	if syscall.Kill(bystander, 0) != nil {
		t.Error("unrelated process was killed")
	}
}

func TestReap_PIDReuseGuardSendsNoSignals(t *testing.T) {
	requireProcTools(t)
	r := testReaper()
	var signaled []int
	r.signal = func(pid int, sig syscall.Signal) error {
		signaled = append(signaled, pid)
		return nil
	}
	// A live PID with a mismatched start time: reuse. Give it a dedicated
	// process group so unrelated orphaned members of the test runner's group
	// cannot be mistaken for members of this synthetic tree.
	bystander := spawnTree(t, "exec sleep 60")
	procs := []procIdent{{PID: bystander, PGID: bystander, Start: "bogus"}}

	report, err := r.reap(context.Background(), procs, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if len(signaled) != 0 {
		t.Errorf("reused PID was signaled: %v", signaled)
	}
	if !report.GracefulExit {
		t.Error("invalidated tree should count as already exited")
	}
}

func TestReap_UnkillableSurvivorErrors(t *testing.T) {
	requireProcTools(t)
	r := testReaper()
	r.signal = func(pid int, sig syscall.Signal) error { return nil } // signals do nothing
	root := spawnTree(t, "sleep 60")
	procs := enumerateAfter(t, r, root)

	report, err := r.reap(context.Background(), procs, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for surviving process")
	}
	if len(report.Survivors) == 0 {
		t.Error("expected survivors in report")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(root)) {
		t.Errorf("error should name surviving pid %d: %v", root, err)
	}
}
