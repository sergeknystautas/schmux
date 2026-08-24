//go:build !nobuildmonitor

package buildmonitor

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/github"
)

// terminalResultTTL bounds how long a terminal (success/failure) result for a
// commit is reused without refetching; reruns reuse the SHA.
const terminalResultTTL = 5 * time.Minute

// Rate-limit backoff: doubles per rate-limited pass, capped.
const (
	initialRateLimitBackoff = 2 * time.Minute
	maxRateLimitBackoff     = 30 * time.Minute
)

type commitKey struct {
	owner string
	repo  string
	sha   string
}

type commitStatus struct {
	status    string
	url       string
	terminal  bool
	fetchedAt time.Time
}

type repoMeta struct {
	hasWorkflows bool
	lastError    string
	backoff      time.Duration
	backoffUntil time.Time
}

// Monitor owns all CI knowledge: an in-memory store of commit statuses keyed
// by (repo, SHA) plus per-repo metadata, and the durable unit state files
// (read and written via ReadState/WriteState — there is no second in-memory
// copy of those). Nothing is keyed by workspace — callers resolve a
// workspace to its (repo, head SHA) and ask Status. All methods are safe for
// concurrent use.
type Monitor struct {
	mu        sync.Mutex
	now       func() time.Time
	storePath string // durable commit-store file; "" disables persistence
	enabled   bool
	commits   map[commitKey]commitStatus
	repoMetas map[commitKey]repoMeta // keyed with sha="" (owner+repo only)
}

// NewMonitor creates a monitor whose commit store persists to storePath
// ("" keeps the store memory-only, for tests).
func NewMonitor(now func() time.Time, storePath string) *Monitor {
	return &Monitor{
		now:       now,
		storePath: storePath,
		commits:   map[commitKey]commitStatus{},
		repoMetas: map[commitKey]repoMeta{},
	}
}

func repoKey(info github.RepoInfo) commitKey {
	return commitKey{owner: info.Owner, repo: info.Repo}
}

// Status derives the CI status of one commit in one repo — the single
// derivation behind every CI indicator. ok=false → absent (no icon).
//
// Precedence: disabled → absent; unknown/empty commit → absent; repo error →
// absent; recorded commit → its status; unrecorded commit in a repo with
// active workflows → queued; otherwise absent.
func (m *Monitor) Status(repo github.RepoInfo, sha string) (string, string, bool) {
	if sha == "" {
		return "", "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled {
		return "", "", false
	}
	meta, haveMeta := m.repoMetas[repoKey(repo)]
	if haveMeta && meta.lastError != "" {
		return "", "", false
	}
	if c, ok := m.commits[commitKey{owner: repo.Owner, repo: repo.Repo, sha: sha}]; ok {
		return c.status, c.url, true
	}
	if haveMeta && meta.hasWorkflows {
		return StatusQueued, "", true
	}
	return "", "", false
}

// Hydrate seeds the commit store and repo metadata from the durable unit
// snapshots at startup so Status serves last-known results before the first
// check pass completes. The monitor owns those files; loading them at boot is
// part of that ownership, and the next pass reconciles anything that moved
// while the server was down. Called only when the feature is enabled at
// startup.
func (m *Monitor) Hydrate(snapshots map[string]*UnitState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
	for _, st := range snapshots {
		if st == nil {
			continue
		}
		owner, name, _ := strings.Cut(st.Repo, "/")
		key := commitKey{owner: owner, repo: name}
		hasWorkflows := len(st.Workflows) > 0
		m.repoMetas[key] = repoMeta{hasWorkflows: hasWorkflows}
		if st.HeadSHA == "" || !hasWorkflows {
			// No active workflows → the commit must not read as queued.
			continue
		}
		status, url, ok := AggregateRuns(unitStateRuns(st), st.HeadSHA)
		if !ok {
			status, url = StatusQueued, ""
		}
		fetchedAt := m.now()
		if t, err := time.Parse(time.RFC3339, st.CheckedAt); err == nil {
			fetchedAt = t
		}
		m.commits[commitKey{owner: owner, repo: name, sha: st.HeadSHA}] = commitStatus{
			status: status, url: url, terminal: isTerminal(status), fetchedAt: fetchedAt,
		}
	}
	// Overlay the persisted commit store (written at the end of each pass):
	// it also covers watched heads — e.g. feature-branch workspaces — whose
	// results exist in no unit snapshot.
	m.loadCommitsLocked()
}

// storedCommit is the durable form of one commit-store entry.
type storedCommit struct {
	Owner     string    `json:"owner"`
	Repo      string    `json:"repo"`
	SHA       string    `json:"sha"`
	Status    string    `json:"status"`
	URL       string    `json:"url,omitempty"`
	Terminal  bool      `json:"terminal,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

// persistCommits writes the commit store to its durable file so recorded
// results — including watched heads that appear in no unit snapshot — survive
// a restart. Best-effort: a failed write is retried by the next pass.
func (m *Monitor) persistCommits() {
	if m.storePath == "" {
		return
	}
	m.mu.Lock()
	stored := make([]storedCommit, 0, len(m.commits))
	for k, c := range m.commits {
		stored = append(stored, storedCommit{
			Owner: k.owner, Repo: k.repo, SHA: k.sha,
			Status: c.status, URL: c.url, Terminal: c.terminal, FetchedAt: c.fetchedAt,
		})
	}
	m.mu.Unlock()
	sort.Slice(stored, func(i, j int) bool {
		a, b := stored[i], stored[j]
		return a.Owner+"/"+a.Repo+"@"+a.SHA < b.Owner+"/"+b.Repo+"@"+b.SHA
	})
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return
	}
	_ = fileutil.AtomicWriteFile(m.storePath, data, 0o600)
}

// loadCommitsLocked overlays the persisted commit store onto the in-memory
// map. An unreadable or corrupt file is skipped — the next pass rewrites it.
func (m *Monitor) loadCommitsLocked() {
	if m.storePath == "" {
		return
	}
	data, err := os.ReadFile(m.storePath)
	if err != nil {
		return
	}
	var stored []storedCommit
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}
	for _, c := range stored {
		m.commits[commitKey{owner: c.Owner, repo: c.Repo, sha: c.SHA}] = commitStatus{
			status: c.Status, url: c.URL, terminal: c.Terminal, fetchedAt: c.FetchedAt,
		}
	}
}

// unitStateRuns reconstructs run records from a snapshot's per-workflow rows
// (already newest-per-workflow for the head) so hydration derives the head
// status through AggregateRuns like any other pass.
func unitStateRuns(st *UnitState) []github.WorkflowRun {
	runs := make([]github.WorkflowRun, 0, len(st.Workflows))
	for _, wf := range st.Workflows {
		if wf.RunID == 0 {
			continue
		}
		runs = append(runs, github.WorkflowRun{
			ID: wf.RunID, WorkflowID: wf.WorkflowID, RunNumber: wf.RunNumber,
			Status: wf.Status, Conclusion: wf.Conclusion, HeadSHA: wf.HeadSHA, HTMLURL: wf.HTMLURL,
		})
	}
	return runs
}

// --- pass-side mutation (called only from the serialized check pass) ---

func (m *Monitor) recordCommit(repo github.RepoInfo, sha, status, url string, terminal bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commits[commitKey{owner: repo.Owner, repo: repo.Repo, sha: sha}] = commitStatus{
		status: status, url: url, terminal: terminal, fetchedAt: m.now(),
	}
}

// commitFreshLocked reports whether a recorded terminal result is fresh
// enough to reuse without refetching.
func (m *Monitor) commitFreshLocked(repo github.RepoInfo, sha string) bool {
	c, ok := m.commits[commitKey{owner: repo.Owner, repo: repo.Repo, sha: sha}]
	return ok && c.terminal && m.now().Sub(c.fetchedAt) < terminalResultTTL
}

func (m *Monitor) setRepoMeta(repo github.RepoInfo, hasWorkflows bool, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta := m.repoMetas[repoKey(repo)]
	meta.hasWorkflows = hasWorkflows
	meta.lastError = errMsg
	if errMsg == "" {
		meta.backoff = 0
		meta.backoffUntil = time.Time{}
	}
	m.repoMetas[repoKey(repo)] = meta
}

// noteRateLimit records a rate-limit error for the repo and advances its
// exponential backoff.
func (m *Monitor) noteRateLimit(repo github.RepoInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := repoKey(repo)
	meta := m.repoMetas[key]
	meta.lastError = "rate_limit"
	meta.backoff *= 2
	if meta.backoff == 0 {
		meta.backoff = initialRateLimitBackoff
	}
	if meta.backoff > maxRateLimitBackoff {
		meta.backoff = maxRateLimitBackoff
	}
	meta.backoffUntil = m.now().Add(meta.backoff)
	m.repoMetas[key] = meta
}

// backingOffLocked reports whether the repo is inside a rate-limit backoff
// window; fetches for it are skipped so last-known data is kept.
func (m *Monitor) backingOffLocked(repo github.RepoInfo) bool {
	meta, ok := m.repoMetas[repoKey(repo)]
	return ok && m.now().Before(meta.backoffUntil)
}

// pruneExcept drops recorded commits not referenced by the current pass's
// inputs. Repo metadata persists across passes.
func (m *Monitor) pruneExcept(live map[commitKey]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.commits {
		if !live[k] {
			delete(m.commits, k)
		}
	}
}

// setDisabled transitions the monitor to the disabled state: all fetched
// knowledge is dropped as part of the transition. Reports whether anything
// was held, so the caller broadcasts once.
func (m *Monitor) setDisabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	had := m.enabled || len(m.commits) > 0 || len(m.repoMetas) > 0
	m.enabled = false
	m.commits = map[commitKey]commitStatus{}
	m.repoMetas = map[commitKey]repoMeta{}
	return had
}

func (m *Monitor) setEnabled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
}
