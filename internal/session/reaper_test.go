package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
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
	awaitZombie(t, root)
	start, err := procStartTime(context.Background(), root)
	if err != nil {
		t.Fatalf("procStartTime: %v", err)
	}
	p := procIdent{PID: root, PGID: root, Start: start}
	if p.aliveValidated(context.Background()) {
		t.Fatal("zombie must count as exited")
	}
}

// awaitZombie waits until pid is an un-waited zombie. It probes ps because
// cmd.Wait would reap the zombie.
func awaitZombie(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := procState(context.Background(), pid)
		if err == nil && strings.HasPrefix(state, "Z") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d never became a zombie: state=%q err=%v", pid, state, err)
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

// --- orphan scan -----------------------------------------------------------

// TestOrphanChild is the body of the process the orphan tests spawn: the
// test binary started again, which prints its PID and then waits to be killed.
func TestOrphanChild(t *testing.T) {
	if os.Getenv("SCHMUX_TEST_ORPHAN") != "1" {
		return
	}
	if os.Getenv("SCHMUX_TEST_ORPHAN_SETPGID") == "1" {
		if err := syscall.Setpgid(0, 0); err != nil {
			fmt.Fprintf(os.Stderr, "setpgid: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println(os.Getpid()) // tells the parent it is running
	time.Sleep(time.Minute)  // lifetime backstop; the test kills it
}

// orphanOpts configures the process the orphan tests spawn.
type orphanOpts struct {
	sessionID string // SCHMUX_SESSION_ID in the environment
	arg       string // extra command-line argument
	ownGroup  bool   // the child starts its own process group
	zombie    bool   // leave the killed shell unreaped, so it stays a zombie
}

// spawnOrphan starts a shell that starts the test binary, kills only the
// shell, and returns the orphan's PID.
func spawnOrphan(t *testing.T, o orphanOpts) int {
	t.Helper()
	pid, _ := spawnOrphanGroup(t, o)
	return pid
}

// spawnOrphanGroup also returns the killed shell's PID, which is the leader
// of the process group the orphan was left in.
func spawnOrphanGroup(t *testing.T, o orphanOpts) (pid, shell int) {
	t.Helper()

	args := []string{"-test.run=^TestOrphanChild$", "--"}
	if o.arg != "" {
		args = append(args, o.arg)
	}
	cmdArgs := append([]string{"-c", `"$@" & wait`, "sh", os.Args[0]}, args...)
	cmd := exec.Command("/bin/sh", cmdArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "SCHMUX_TEST_ORPHAN=1", "SCHMUX_SESSION_ID="+o.sessionID)
	if o.ownGroup {
		cmd.Env = append(cmd.Env, "SCHMUX_TEST_ORPHAN_SETPGID=1")
	}

	// A StdoutPipe would be closed by Wait, before the PID could be read.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	cmd.Stdout = w

	if err := cmd.Start(); err != nil {
		w.Close()
		t.Fatalf("spawn orphan: %v", err)
	}
	w.Close()
	shell = cmd.Process.Pid
	shellReaped := false

	t.Cleanup(func() {
		if pid > 0 {
			syscall.Kill(pid, syscall.SIGKILL)
		}
		if shellReaped {
			return
		}
		// The shell still holds its PID: take its whole process group with
		// it. Once the PID is freed it could be reused, and kill(-pgid)
		// would then hit an unrelated group.
		syscall.Kill(-shell, syscall.SIGKILL)
		cmd.Wait()
	})

	if err := r.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatalf("read orphan pid: %v", err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse orphan pid %q: %v", line, err)
	}

	// Kill only the shell: the orphan stays alive in the shell's now-dead
	// process group.
	syscall.Kill(shell, syscall.SIGKILL)
	if !o.zombie {
		// Reap the shell now so it leaves no zombie behind. The group kill
		// in the cleanup then has nothing left to take.
		cmd.Wait()
		shellReaped = true
	}
	return pid, shell
}

// identOf reads a live process's identity, failing the test if it cannot.
func identOf(t *testing.T, pid int) procIdent {
	t.Helper()
	ident, ok := identify(context.Background(), pid)
	if !ok {
		t.Fatalf("identify(%d) failed", pid)
	}
	return ident
}

func TestOrphanPIDs(t *testing.T) {
	const sessionID = "ws-1"
	tests := []struct {
		name    string
		plain   string
		withEnv string
		want    []int
	}{
		{
			name:    "dead leader",
			plain:   "20 10 R node x.js",
			withEnv: "20 10 R node x.js A=1 SCHMUX_SESSION_ID=ws-1 B=2",
			want:    []int{20},
		},
		{
			name:    "ID is last",
			plain:   "20 10 R node x.js",
			withEnv: "20 10 R node x.js SCHMUX_SESSION_ID=ws-1",
			want:    []int{20},
		},
		{
			name:    "spaces in an earlier value",
			plain:   "20 10 R node x.js",
			withEnv: "20 10 R node x.js NOTE=a b c SCHMUX_SESSION_ID=ws-1",
			want:    []int{20},
		},
		{
			name:    "padded columns, different STAT",
			plain:   "   20   10 R+   node x.js",
			withEnv: "   20   10 S      node x.js SCHMUX_SESSION_ID=ws-1",
			want:    []int{20},
		},
		{
			name:    "zombie leader",
			plain:   "10 10 Z <defunct>\n20 10 R node x.js",
			withEnv: "10 10 Z <defunct>\n20 10 R node x.js SCHMUX_SESSION_ID=ws-1",
			want:    []int{20},
		},
		{
			name:    "living leader",
			plain:   "10 10 S sh\n20 10 R node x.js",
			withEnv: "10 10 S sh\n20 10 R node x.js SCHMUX_SESSION_ID=ws-1",
			want:    nil,
		},
		{
			name:    "leader alive in one listing only",
			plain:   "10 10 S sh\n20 10 R node x.js",
			withEnv: "20 10 R node x.js SCHMUX_SESSION_ID=ws-1",
			want:    nil,
		},
		{
			name:    "leads its own group",
			plain:   "20 20 S node x.js",
			withEnv: "20 20 S node x.js SCHMUX_SESSION_ID=ws-1",
			want:    nil,
		},
		{
			name:    "orphan is a zombie",
			plain:   "20 10 Z node x.js",
			withEnv: "20 10 Z node x.js SCHMUX_SESSION_ID=ws-1",
			want:    nil,
		},
		{
			name:    "different ID",
			plain:   "20 10 R node x.js",
			withEnv: "20 10 R node x.js SCHMUX_SESSION_ID=ws-2",
			want:    nil,
		},
		{
			name:    "longer ID",
			plain:   "20 10 R node x.js",
			withEnv: "20 10 R node x.js SCHMUX_SESSION_ID=ws-1-2",
			want:    nil,
		},
		{
			name:    "ID in command only",
			plain:   "20 10 R node SCHMUX_SESSION_ID=ws-1",
			withEnv: "20 10 R node SCHMUX_SESSION_ID=ws-1 A=1",
			want:    nil,
		},
		{
			name:    "command changed",
			plain:   "20 10 R sh -c x",
			withEnv: "20 10 R node x.js SCHMUX_SESSION_ID=ws-1",
			want:    nil,
		},
		{
			name:    "group changed",
			plain:   "20 11 R node x.js",
			withEnv: "20 10 R node x.js SCHMUX_SESSION_ID=ws-1",
			want:    nil,
		},
		{
			name:    "in withEnv only",
			plain:   "",
			withEnv: "20 10 R node x.js SCHMUX_SESSION_ID=ws-1",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orphanPIDs(tt.plain, tt.withEnv, sessionID)
			if !slices.Equal(got, tt.want) {
				t.Errorf("orphanPIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrphans_FoundAndReaped(t *testing.T) {
	requireProcTools(t)
	sessionID := fmt.Sprintf("orphan-%d", time.Now().UnixNano())
	orphanPID, shellPID := spawnOrphanGroup(t, orphanOpts{sessionID: sessionID})

	r := testReaper()
	procs, err := r.orphans(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("orphans: %v", err)
	}
	if len(procs) != 1 || procs[0].PID != orphanPID {
		t.Fatalf("orphans = %+v, want one process with pid %d", procs, orphanPID)
	}
	if procs[0].Start == "" {
		t.Error("orphan start time is empty")
	}
	if procs[0].PGID != shellPID {
		t.Errorf("orphan pgid = %d, want the killed shell's pid %d", procs[0].PGID, shellPID)
	}

	if _, err := r.reap(context.Background(), procs, 100*time.Millisecond); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if procs[0].aliveValidated(context.Background()) {
		t.Error("orphan still alive after reap")
	}
}

func TestOrphans_ZombieLeader(t *testing.T) {
	requireProcTools(t)
	if runtime.GOOS != "linux" {
		t.Skip("Linux retains an un-waited child as a visible zombie")
	}
	sessionID := fmt.Sprintf("zombie-%d", time.Now().UnixNano())
	orphanPID, shellPID := spawnOrphanGroup(t, orphanOpts{sessionID: sessionID, zombie: true})
	awaitZombie(t, shellPID)

	r := testReaper()
	procs, err := r.orphans(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("orphans: %v", err)
	}
	if len(procs) != 1 || procs[0].PID != orphanPID {
		t.Fatalf("orphans = %+v, want one process with pid %d", procs, orphanPID)
	}

	if _, err := r.reap(context.Background(), procs, 100*time.Millisecond); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if procs[0].aliveValidated(context.Background()) {
		t.Error("orphan still alive after reap")
	}
}

func TestOrphans_Ignores(t *testing.T) {
	requireProcTools(t)
	sessionID := fmt.Sprintf("ignore-%d", time.Now().UnixNano())
	otherID := fmt.Sprintf("other-%d", time.Now().UnixNano())

	control := spawnOrphan(t, orphanOpts{sessionID: sessionID})
	spawnOrphan(t, orphanOpts{sessionID: sessionID, ownGroup: true})
	spawnOrphan(t, orphanOpts{sessionID: otherID})
	spawnOrphan(t, orphanOpts{sessionID: sessionID + "-2"})
	spawnOrphan(t, orphanOpts{sessionID: otherID, arg: "SCHMUX_SESSION_ID=" + sessionID})

	procs, err := testReaper().orphans(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("orphans: %v", err)
	}
	if len(procs) != 1 || procs[0].PID != control {
		t.Fatalf("orphans = %+v, want only the control's pid %d", procs, control)
	}
}
