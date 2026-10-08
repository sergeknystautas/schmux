//go:build !nobuildmonitor

package buildmonitor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sergeknystautas/schmux/internal/github"
)

// UnitInput is one monitored repo, resolved by the caller.
type UnitInput struct {
	Slug      string
	RepoName  string
	Branch    string
	Token     string
	Info      github.RepoInfo
	HeadSHA   string // default-branch head, from ls-remote; "" if unresolvable
	StatePath string // durable unit file
	// SessionIDs are the sessions that exist now. A row linked to any other
	// session (disposed, by any path) is unlinked.
	SessionIDs map[string]bool
}

// HeadInput is one commit to watch, resolved by the caller. The caller
// resolves workspaces to (CI repo, branch, head SHA) — including fork
// resolution — so no workspace identity crosses the monitor boundary.
type HeadInput struct {
	Info   github.RepoInfo // CI repo the commit belongs to
	Branch string          // fetch metadata: runs are listed per branch
	SHA    string          // head commit ("" → skipped)
	Token  string
}

// PassResult reports what changed and what needs remediation.
type PassResult struct {
	Changed    bool
	Events     map[string][]TransitionEvent // slug → events (dashboard builds directives)
	UnitStates map[string]*UnitState        // slug → post-transition state (persisted when Changed=true)
	StaleRuns  []StaleRun                   // fetched runs ignored as older than the recorded run (dashboard logs them)
}

// StaleRun is a fetch that returned no run, or a run created before the run
// its workflow row already records (FetchedRunID 0 means no run). GitHub has
// been observed serving old snapshots and omitting a workflow's latest run;
// the row keeps its run.
type StaleRun struct {
	Slug             string
	WorkflowID       int64
	Workflow         string
	FetchedRunID     int64
	FetchedCreatedAt string
	KeptRunID        int64
	KeptCreatedAt    string
}

// CheckPass executes one full branch-head pass: per-unit CI fetch, watched
// head resolution, durable unit state with transitions, and the status-store
// updates that Status reads.
//
// enabled is observed world state, not a control signal: the scheduler ticks
// unconditionally and the monitor transitions on the value like any other
// input. A disabled pass drops all fetched knowledge and reports Changed once.
//
// Remaining inputs are pre-resolved by the caller (eligibility, head SHAs via
// ls-remote, tokens, fork CI repos). The monitor owns the commit store;
// nothing is keyed by workspace.
func (m *Monitor) CheckPass(ctx context.Context, actions Actions, enabled bool, units []UnitInput, heads []HeadInput) PassResult {
	result := PassResult{
		Events:     map[string][]TransitionEvent{},
		UnitStates: map[string]*UnitState{},
	}
	if !enabled {
		if result.Changed = m.setDisabled(); result.Changed {
			m.persistCommits() // the drop is part of the transition
		}
		return result
	}
	m.setEnabled()

	// The watch set is this pass's inputs: the commits Status may be asked
	// about until the next pass. Fetched or not, they stay referenced.
	live := map[commitKey]bool{}
	for _, u := range units {
		if u.HeadSHA != "" {
			live[commitKey{owner: u.Info.Owner, repo: u.Info.Repo, branch: u.Branch, sha: u.HeadSHA}] = true
		}
	}
	for _, h := range heads {
		if h.SHA != "" {
			live[commitKey{owner: h.Info.Owner, repo: h.Info.Repo, branch: h.Branch, sha: h.SHA}] = true
		}
	}

	jobsCache := map[int64]jobFetch{}

	for _, unit := range units {
		prev, readErr := ReadState(unit.StatePath)
		unlinked := unlinkGoneSessions(prev, unit.SessionIDs)

		m.mu.Lock()
		backingOff := m.backingOffLocked(unit.Info)
		m.mu.Unlock()
		if backingOff {
			if prev != nil {
				result.UnitStates[unit.Slug] = prev
			}
			continue
		}

		// Always learn whether the repo has active workflows — Status needs
		// this for the unrecorded-commit queued derivation.
		workflows, repoErr := actions.ListWorkflows(ctx, unit.Token, unit.Info)
		if repoErr != nil {
			m.noteFetchError(unit.Info, repoErr)
			if prev != nil {
				result.UnitStates[unit.Slug] = prev
			}
			continue
		}
		active := make([]github.Workflow, 0, len(workflows))
		for _, wf := range workflows {
			if wf.State == "active" {
				active = append(active, wf)
			}
		}
		m.setRepoMeta(unit.Info, len(active) > 0, "")

		if unit.HeadSHA == "" {
			// Can't rebaseline — keep prior state, don't fetch unit runs.
			if prev != nil {
				result.UnitStates[unit.Slug] = prev
			}
			continue
		}

		state, stale, unitChanged, passErr := m.checkUnit(ctx, actions, unit, prev, active, jobsCache)
		if passErr != nil {
			if prev != nil {
				result.UnitStates[unit.Slug] = prev
			}
			continue
		}
		result.StaleRuns = append(result.StaleRuns, stale...)
		if readErr != nil {
			// Prior state was unreadable: surface it rather than silently
			// rebaselining — remediation history was lost.
			state.LastError = fmt.Sprintf("prior state unreadable, rebaselined: %v", readErr)
		}

		if err := WriteState(unit.StatePath, state); err != nil {
			if prev != nil {
				result.UnitStates[unit.Slug] = prev
			}
			continue
		}

		if unitChanged || unlinked {
			result.Changed = true
		}

		events, _ := ApplyTransitions(prev, state)
		if len(events) > 0 {
			result.Events[unit.Slug] = events
		}
		result.UnitStates[unit.Slug] = state
	}

	m.checkHeads(ctx, actions, heads, jobsCache)
	m.pruneExcept(live)
	m.persistCommits()
	return result
}

type fetchKey struct {
	owner, repo, branch string
}

type jobFetch struct {
	jobs []github.WorkflowJob
	err  error
}

// noteFetchError records a fetch failure for the repo: rate limits advance
// the exponential backoff, everything else is classified and kept until the
// next successful fetch.
func (m *Monitor) noteFetchError(info github.RepoInfo, err error) {
	var rle *github.RateLimitError
	if errors.As(err, &rle) {
		m.noteRateLimit(info)
		return
	}
	m.mu.Lock()
	hasWorkflows := m.repoMetas[repoKey(info)].hasWorkflows
	m.mu.Unlock()
	m.setRepoMeta(info, hasWorkflows, classify(err))
}

// checkUnit builds a fresh unit snapshot for one repo's default-branch head.
func (m *Monitor) checkUnit(ctx context.Context, actions Actions, unit UnitInput, prev *UnitState, active []github.Workflow, jobsCache map[int64]jobFetch) (*UnitState, []StaleRun, bool, error) {
	// One request per workflow for its newest run on the branch. Workflows
	// are independent (CI runs on push, Performance on a cron), and the
	// repo-wide branch listing mixed them into one 100-run window that GitHub
	// intermittently serves stale or incomplete, breaking every row at once.
	var runs []github.WorkflowRun
	for _, wf := range active {
		run, err := actions.LatestWorkflowRun(ctx, unit.Token, unit.Info, wf.ID, unit.Branch)
		if err != nil {
			m.noteFetchError(unit.Info, err)
			return nil, nil, false, err
		}
		if run != nil {
			runs = append(runs, *run)
		}
	}
	// GitHub can leave a run queued after its jobs start; reconcile each
	// workflow's run against its jobs. The head commit's status below reads
	// the same runs, filtered to the head SHA.
	runs = runsWithEffectiveStatus(ctx, actions, unit.Token, unit.Info, runs, "", jobsCache)

	state := &UnitState{
		RepoName: unit.RepoName,
		Repo:     unit.Info.Owner + "/" + unit.Info.Repo,
		Branch:   unit.Branch,
		HeadSHA:  unit.HeadSHA,
	}

	prevByID := prevWorkflowsByID(prev)
	var stale []StaleRun
	for _, wf := range active {
		ws := WorkflowState{Name: wf.Name, Path: wf.Path, WorkflowID: wf.ID}
		newest := newestRun(runs, wf.ID)
		pw := prevByID[wf.ID]
		recorded := pw != nil && pw.RunID != 0
		if newest == nil && !recorded {
			// The workflow has never run on this branch — row exists, no run info.
			state.Workflows = append(state.Workflows, ws)
			continue
		}
		if recorded && (newest == nil || createdBefore(newest.CreatedAt, pw.RunCreatedAt)) {
			// GitHub returned no run, or an older run than the one recorded:
			// no newer evidence. Keep the recorded run; transitions see no
			// change.
			s := StaleRun{
				Slug: unit.Slug, WorkflowID: wf.ID, Workflow: wf.Name,
				KeptRunID: pw.RunID, KeptCreatedAt: pw.RunCreatedAt,
			}
			if newest != nil {
				s.FetchedRunID, s.FetchedCreatedAt = newest.ID, newest.CreatedAt
			}
			stale = append(stale, s)
			ws.RunID, ws.RunNumber, ws.Status, ws.Conclusion = pw.RunID, pw.RunNumber, pw.Status, pw.Conclusion
			ws.HTMLURL, ws.HeadSHA, ws.RunCreatedAt, ws.FailedJobs = pw.HTMLURL, pw.HeadSHA, pw.RunCreatedAt, pw.FailedJobs
			state.Workflows = append(state.Workflows, ws)
			continue
		}
		ws.RunID = newest.ID
		ws.RunNumber = newest.RunNumber
		ws.Status = newest.Status
		ws.HTMLURL = newest.HTMLURL
		ws.HeadSHA = newest.HeadSHA
		ws.RunCreatedAt = newest.CreatedAt
		if newest.Status == "completed" {
			ws.Conclusion = newest.Conclusion
			if newest.Conclusion == "failure" {
				jobs, err := listRunJobsCached(ctx, actions, unit.Token, unit.Info, newest.ID, jobsCache)
				if err != nil {
					state.LastError = classify(err)
				} else {
					for _, j := range jobs {
						if j.Conclusion == "failure" {
							ws.FailedJobs = append(ws.FailedJobs, FailedJob{ID: j.ID, Name: j.Name, HTMLURL: j.HTMLURL})
						}
					}
				}
			}
		}
		state.Workflows = append(state.Workflows, ws)
	}

	// Record the head commit's status for Status.
	status, url, ok := AggregateRuns(runs, unit.HeadSHA)
	if !ok {
		status, url = StatusQueued, ""
	}
	m.recordCommit(unit.Info, unit.Branch, unit.HeadSHA, status, url, isTerminal(status))

	state.CheckedAt = m.now().UTC().Format(time.RFC3339)

	_, unitChanged := ApplyTransitions(prev, state)
	return state, stale, unitChanged, nil
}

// checkHeads resolves CI status for each watched head commit, deduping run
// fetches per (repo, branch). A recorded terminal result fresh within
// terminalResultTTL is kept without refetching.
func (m *Monitor) checkHeads(ctx context.Context, actions Actions, heads []HeadInput, jobsCache map[int64]jobFetch) {
	fetched := map[fetchKey]bool{}
	runsCache := map[fetchKey][]github.WorkflowRun{}

	for _, h := range heads {
		if h.SHA == "" {
			continue
		}
		key := fetchKey{owner: h.Info.Owner, repo: h.Info.Repo, branch: h.Branch}

		m.mu.Lock()
		fresh := m.commitFreshLocked(h.Info, h.Branch, h.SHA)
		backingOff := m.backingOffLocked(h.Info)
		m.mu.Unlock()
		if fresh {
			continue // terminal result still fresh; reruns reuse the SHA
		}

		runs, ok := runsCache[key]
		if !ok {
			if fetched[key] || backingOff {
				continue // fetch already failed this pass, or backing off: keep last known
			}
			fetched[key] = true // one attempt per (repo, branch) per pass
			var err error
			runs, err = actions.ListRepoRuns(ctx, h.Token, h.Info, h.Branch)
			if err != nil {
				m.noteFetchError(h.Info, err)
				continue
			}
			// A reachable Actions API means CI may be active; precise
			// workflow knowledge only exists for unit repos.
			m.setRepoMeta(h.Info, true, "")
			runsCache[key] = runs
		}

		effectiveRuns := runsWithEffectiveStatus(ctx, actions, h.Token, h.Info, runs, h.SHA, jobsCache)
		status, url, aggregated := AggregateRuns(effectiveRuns, h.SHA)
		if !aggregated {
			status, url = StatusQueued, ""
		}
		m.recordCommit(h.Info, h.Branch, h.SHA, status, url, isTerminal(status))
	}
}

// runsWithEffectiveStatus corrects a GitHub Actions inconsistency: the runs
// endpoint can keep a workflow run at "queued" after the jobs endpoint shows
// one or more jobs in progress. Only the newest run per workflow can affect
// the caller, so only those runs need job lookups. headSHA scopes "newest" to
// one commit (AggregateRuns); "" means the newest run on the branch, whatever
// commit it built (unit rows).
func runsWithEffectiveStatus(ctx context.Context, actions Actions, token string, info github.RepoInfo, runs []github.WorkflowRun, headSHA string, jobsCache map[int64]jobFetch) []github.WorkflowRun {
	effective := append([]github.WorkflowRun(nil), runs...)
	seen := map[int64]bool{}
	for i := range effective {
		run := &effective[i]
		if (headSHA != "" && run.HeadSHA != headSHA) || seen[run.WorkflowID] {
			continue
		}
		seen[run.WorkflowID] = true
		if run.Status == "completed" || run.Status == "in_progress" {
			continue
		}
		jobs, err := listRunJobsCached(ctx, actions, token, info, run.ID, jobsCache)
		if err != nil {
			continue
		}
		for _, job := range jobs {
			if job.Status == "in_progress" {
				run.Status = "in_progress"
				break
			}
		}
	}
	return effective
}

func listRunJobsCached(ctx context.Context, actions Actions, token string, info github.RepoInfo, runID int64, cache map[int64]jobFetch) ([]github.WorkflowJob, error) {
	if fetched, ok := cache[runID]; ok {
		return fetched.jobs, fetched.err
	}
	jobs, err := actions.ListRunJobs(ctx, token, info, runID)
	cache[runID] = jobFetch{jobs: jobs, err: err}
	return jobs, err
}

func isTerminal(status string) bool {
	return status == StatusSuccess || status == StatusFailure
}

// newestRun returns the newest run for workflowID (GitHub lists runs newest
// first), whatever commit it built.
func newestRun(runs []github.WorkflowRun, workflowID int64) *github.WorkflowRun {
	for i := range runs {
		if runs[i].WorkflowID == workflowID {
			return &runs[i]
		}
	}
	return nil
}

// createdBefore reports whether GitHub created_at timestamp a is earlier than
// b. A row recorded before created_at was stored has b == "" and accepts any
// run, as does a run with an unparseable timestamp.
// unlinkGoneSessions clears row links to sessions not in live, before
// transitions carry the links forward. The remediation ledger keeps its
// record, so the run is not relaunched automatically.
func unlinkGoneSessions(s *UnitState, live map[string]bool) bool {
	if s == nil {
		return false
	}
	changed := false
	for i := range s.Workflows {
		if w := &s.Workflows[i]; w.SessionID != "" && !live[w.SessionID] {
			w.SessionID = ""
			changed = true
		}
	}
	return changed
}

func createdBefore(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	return errA == nil && errB == nil && ta.Before(tb)
}
