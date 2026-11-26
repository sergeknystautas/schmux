//go:build !nogithub

package github

import (
	"errors"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// Discovery manages GitHub PR discovery for configured repos.
type Discovery struct {
	mu            sync.RWMutex
	pullRequests  []contracts.PullRequest
	lastFetchedAt *time.Time
	lastError     string
	logger        *log.Logger

	// Lifecycle management
	ticker   *time.Ticker
	stopChan chan struct{}
	getRepos func() []state.Repo
	cfg      *config.Config
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery(logger *log.Logger) *Discovery {
	return &Discovery{logger: logger}
}

// Seed initializes discovery state from cached data.
func (d *Discovery) Seed(prs []contracts.PullRequest) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prs != nil {
		d.pullRequests = append([]contracts.PullRequest(nil), prs...)
	}
}

// GetPRs returns the current list of PRs and metadata.
func (d *Discovery) GetPRs() ([]contracts.PullRequest, *time.Time, string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	prs := make([]contracts.PullRequest, len(d.pullRequests))
	copy(prs, d.pullRequests)
	return prs, d.lastFetchedAt, d.lastError
}

// Refresh fetches open PRs for the GitHub repos in repos.
// Returns the fetched PRs and any rate limit retry-after value.
// The cfg parameter is used to resolve per-repo GitHub OAuth tokens.
func (d *Discovery) Refresh(repos []state.Repo, cfg *config.Config) ([]contracts.PullRequest, *int, error) {
	var allPRs []contracts.PullRequest
	fetchedRepos := 0

	for _, repo := range repos {
		if !IsGitHubURL(repo.URL) {
			continue
		}
		info, err := ParseRepoURL(repo.URL)
		if err != nil {
			if d.logger != nil {
				d.logger.Warn("skipping repo", "url", repo.URL, "err", err)
			}
			continue
		}

		// Resolve per-repo OAuth token (empty when not configured).
		var token string
		if cfg != nil {
			if login := cfg.GetGitHubLogin(repo); login != "" {
				token, _ = config.GetGitHubToken(login)
			}
		}

		prs, err := FetchOpenPRs(info, repo.Name, repo.URL, token)
		if err != nil {
			var rle *RateLimitError
			if errors.As(err, &rle) {
				retryAfter := rle.RetryAfterSec
				d.mu.Lock()
				d.lastError = err.Error()
				d.mu.Unlock()
				return nil, &retryAfter, err
			}
			if d.logger != nil {
				if IsNotFound(err) {
					d.logger.Info("skipping repo GitHub did not return", "url", repo.URL)
				} else {
					d.logger.Error("error fetching PRs", "url", repo.URL, "err", err)
				}
			}
			continue
		}
		allPRs = append(allPRs, prs...)
		fetchedRepos++
	}

	// Update state
	now := time.Now()
	d.mu.Lock()
	d.pullRequests = allPRs
	d.lastFetchedAt = &now
	d.lastError = ""
	d.mu.Unlock()

	if d.logger != nil {
		d.logger.Info("discovered PRs", "pr_count", len(allPRs), "repo_count", fetchedRepos)
	}
	return allPRs, nil, nil
}

// FindPR looks up a PR by repo URL and number.
func (d *Discovery) FindPR(repoURL string, prNumber int) (contracts.PullRequest, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, pr := range d.pullRequests {
		if pr.RepoURL == repoURL && pr.Number == prNumber {
			return pr, true
		}
	}
	return contracts.PullRequest{}, false
}

// SetTarget enables or disables PR discovery polling based on target configuration.
// Call with non-empty target and a function that returns current repos to start polling.
// Call with empty target to stop.
func (d *Discovery) SetTarget(target string, getRepos func() []state.Repo, cfg *config.Config) {
	d.mu.Lock()
	defer d.mu.Unlock()

	enabled := target != ""
	wasEnabled := d.ticker != nil

	if enabled && !wasEnabled {
		// Start polling
		d.getRepos = getRepos
		d.cfg = cfg
		d.stopChan = make(chan struct{})
		d.ticker = time.NewTicker(1 * time.Hour)
		go d.poll()
		// Trigger immediate refresh
		go d.Refresh(getRepos(), cfg)
	} else if !enabled && wasEnabled {
		// Stop polling
		d.stop()
	}
}

// Stop halts the polling goroutine and cleans up resources.
// Safe to call multiple times.
func (d *Discovery) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stop()
}

// stop is the internal version that assumes the lock is held.
func (d *Discovery) stop() {
	if d.ticker != nil {
		d.ticker.Stop()
		d.ticker = nil
	}
	if d.stopChan != nil {
		close(d.stopChan)
		d.stopChan = nil
	}
	d.getRepos = nil
	d.cfg = nil
}

// poll runs the hourly refresh loop until stopped.
func (d *Discovery) poll() {
	// Capture ticker channel under lock to avoid racing with stop() setting d.ticker = nil.
	d.mu.RLock()
	tickerC := d.ticker.C
	d.mu.RUnlock()

	for {
		select {
		case <-tickerC:
			d.mu.RLock()
			getRepos := d.getRepos
			cfg := d.cfg
			d.mu.RUnlock()
			if getRepos != nil {
				d.Refresh(getRepos(), cfg)
			}
		case <-d.stopChan:
			return
		}
	}
}

// IsAvailable reports whether the GitHub module is included in this build.
func IsAvailable() bool { return true }
