// Package session: reaper.go terminates a fenced session's process tree.
//
// Fenced sessions run tmux -> fence -> sh -> agent, and the agent lives in a
// different process group than the tmux pane, so tmux's SIGHUP can leave it
// running (codex spins at 100% CPU on the revoked pty and holds its
// thread-writer lock). The reaper enumerates the tree while the pane is
// alive, waits out the grace period after tmux dies, then escalates
// SIGTERM -> SIGKILL against start-time-validated targets only.
package session

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
)

type procIdent struct {
	PID   int
	PGID  int
	Start string // opaque `ps -o lstart=` value; compared for equality only
}

type reaper struct {
	logger *log.Logger
	signal func(pid int, sig syscall.Signal) error
	poll   time.Duration
}

func newReaper(logger *log.Logger) *reaper {
	return &reaper{
		logger: logger,
		signal: func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) },
		poll:   200 * time.Millisecond,
	}
}

// enumerate walks the descendants of rootPID (inclusive) and records each
// live process's PID, PGID, and start time. Processes that exit during the
// walk are skipped. An empty result (root already dead) is an error: the
// caller must fall back to legacy disposal.
func (r *reaper) enumerate(ctx context.Context, rootPID int) ([]procIdent, error) {
	pids := []int{rootPID}
	frontier := []int{rootPID}
	for depth := 0; len(frontier) > 0 && depth < 32; depth++ {
		var next []int
		for _, p := range frontier {
			next = append(next, childPIDs(ctx, p)...)
		}
		pids = append(pids, next...)
		frontier = next
	}
	var procs []procIdent
	for _, pid := range pids {
		pgid, err := currentPGID(ctx, pid)
		if err != nil {
			continue // exited during the walk
		}
		start, err := procStartTime(ctx, pid)
		if err != nil {
			continue
		}
		procs = append(procs, procIdent{PID: pid, PGID: pgid, Start: start})
	}
	if len(procs) == 0 {
		return nil, fmt.Errorf("no live processes under pane pid %d", rootPID)
	}
	return procs, nil
}

// aliveValidated reports whether the process is alive AND still the process
// enumerated at dispose time. A start-time mismatch means the PID was reused
// by an unrelated process; it must never be signaled.
func (p procIdent) aliveValidated(ctx context.Context) bool {
	if syscall.Kill(p.PID, 0) != nil {
		return false
	}
	start, err := procStartTime(ctx, p.PID)
	return err == nil && start == p.Start
}

func childPIDs(ctx context.Context, pid int) []int {
	out, err := exec.CommandContext(ctx, "pgrep", "-P", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil // pgrep exits 1 when there are no children
	}
	return parsePIDLines(string(out))
}

func groupMemberPIDs(ctx context.Context, pgid int) []int {
	out, err := exec.CommandContext(ctx, "pgrep", "-g", strconv.Itoa(pgid)).Output()
	if err != nil {
		return nil
	}
	return parsePIDLines(string(out))
}

func currentPGID(ctx context.Context, pid int) (int, error) {
	return psIntField(ctx, pid, "pgid=")
}

func currentPPID(ctx context.Context, pid int) (int, error) {
	return psIntField(ctx, pid, "ppid=")
}

func procStartTime(ctx context.Context, pid int) (string, error) {
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("no start time for pid %d", pid)
	}
	return s, nil
}

func psIntField(ctx context.Context, pid int, field string) (int, error) {
	out, err := exec.CommandContext(ctx, "ps", "-o", field, "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

func parsePIDLines(out string) []int {
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

type reapReport struct {
	Enumerated    []procIdent
	GracefulExit  bool
	GraceElapsed  time.Duration
	SigtermGroups []int
	SigtermPIDs   []int
	SigkillGroups []int
	SigkillPIDs   []int
	Survivors     []procIdent
}

// reap waits up to grace for the enumerated tree to exit, then escalates
// SIGTERM -> 2s -> SIGKILL -> 2s and verifies. Call it AFTER tmux
// kill-session; the pty hangup is the graceful signal, reap never sends one.
func (r *reaper) reap(ctx context.Context, procs []procIdent, grace time.Duration) (reapReport, error) {
	report := reapReport{Enumerated: procs}
	start := time.Now()

	if r.waitClear(ctx, procs, grace) {
		report.GracefulExit = true
		report.GraceElapsed = time.Since(start)
		return report, nil
	}
	report.GraceElapsed = time.Since(start)

	report.SigtermGroups, report.SigtermPIDs = r.signalPhase(ctx, procs, syscall.SIGTERM)
	if r.waitClear(ctx, procs, 2*time.Second) {
		return report, nil
	}

	report.SigkillGroups, report.SigkillPIDs = r.signalPhase(ctx, procs, syscall.SIGKILL)
	r.waitClear(ctx, procs, 2*time.Second)

	report.Survivors = r.liveTreePIDs(ctx, procs)
	if len(report.Survivors) > 0 {
		var pids []string
		for _, s := range report.Survivors {
			pids = append(pids, strconv.Itoa(s.PID))
		}
		return report, fmt.Errorf("fenced session cleanup failed: surviving pids %s", strings.Join(pids, ", "))
	}
	return report, nil
}

// waitClear polls until the tree is empty, the timeout elapses, or ctx is
// cancelled.
func (r *reaper) waitClear(ctx context.Context, procs []procIdent, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if len(r.liveTreePIDs(ctx, procs)) == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(r.poll):
		}
	}
}

// liveTreePIDs returns the tree's still-live members: start-time-validated
// enumerated PIDs, plus current members of each recorded process group whose
// parent is PID 1 or an enumerated PID. The parent test admits processes
// forked after enumeration (their parent is in the tree, or they were
// orphaned to 1 when it died) and filters most members of an unrelated group
// that recycled the PGID after the whole original group exited. Residual
// accepted case: a recycled group whose occupant daemonized (parent 1)
// within the seconds-long reap window would be admitted — and at worst
// signaled, since a PGID cannot be recycled while any original member lives.
func (r *reaper) liveTreePIDs(ctx context.Context, procs []procIdent) []procIdent {
	inTree := make(map[int]bool, len(procs))
	for _, p := range procs {
		inTree[p.PID] = true
	}
	var live []procIdent
	seen := map[int]bool{}
	for _, p := range procs {
		if p.aliveValidated(ctx) && !seen[p.PID] {
			live = append(live, p)
			seen[p.PID] = true
		}
	}
	groups := map[int]bool{}
	for _, p := range procs {
		groups[p.PGID] = true
	}
	for pgid := range groups {
		for _, pid := range groupMemberPIDs(ctx, pgid) {
			if seen[pid] {
				continue
			}
			ppid, err := currentPPID(ctx, pid)
			if err != nil || (ppid != 1 && !inTree[ppid]) {
				continue
			}
			live = append(live, procIdent{PID: pid, PGID: pgid})
			seen[pid] = true
		}
	}
	return live
}

// signalPhase signals each recorded process group that still contains a live
// tree member in it, and individually signals live members that left their
// recorded group. Targets derive only from liveTreePIDs — never from names.
func (r *reaper) signalPhase(ctx context.Context, procs []procIdent, sig syscall.Signal) (groups, strays []int) {
	recorded := map[int]bool{}
	for _, p := range procs {
		recorded[p.PGID] = true
	}
	groupLive := map[int]bool{}
	for _, p := range r.liveTreePIDs(ctx, procs) {
		cur, err := currentPGID(ctx, p.PID)
		if err != nil {
			continue
		}
		if recorded[cur] {
			groupLive[cur] = true
		} else {
			strays = append(strays, p.PID)
		}
	}
	for pgid := range groupLive {
		if pgid <= 1 {
			continue // kill(-1) signals every process the user owns; never
		}
		if err := r.signal(-pgid, sig); err == nil {
			groups = append(groups, pgid)
		}
	}
	for _, pid := range strays {
		if pid <= 1 {
			continue
		}
		r.signal(pid, sig)
	}
	return groups, strays
}
