package github

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
)

// Discovery manages GitHub PR discovery for configured repos.
type Discovery struct {
	mu            sync.RWMutex
	pullRequests  []contracts.PullRequest
	publicRepos   []string // repo URLs that are confirmed public
	lastFetchedAt *time.Time
	lastError     string

	// Lifecycle management
	ticker   *time.Ticker
	stopChan chan struct{}
	getRepos func() []config.Repo
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery() *Discovery {
	return &Discovery{}
}

// Seed initializes discovery state from cached data.
func (d *Discovery) Seed(prs []contracts.PullRequest, publicRepos []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prs != nil {
		d.pullRequests = append([]contracts.PullRequest(nil), prs...)
	}
	if publicRepos != nil {
		d.publicRepos = append([]string(nil), publicRepos...)
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

// GetPublicRepos returns the list of public repo URLs.
func (d *Discovery) GetPublicRepos() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]string, len(d.publicRepos))
	copy(result, d.publicRepos)
	return result
}

// Refresh discovers GitHub repos and fetches PRs.
// Returns the fetched PRs and any rate limit retry-after value.
func (d *Discovery) Refresh(repos []config.Repo) ([]contracts.PullRequest, *int, error) {
	// Step 1: Find GitHub repos and check visibility
	var publicRepos []string
	repoMap := make(map[string]config.Repo) // repoURL -> repo config

	for _, repo := range repos {
		if !IsGitHubURL(repo.URL) {
			continue
		}
		info, err := ParseRepoURL(repo.URL)
		if err != nil {
			fmt.Printf("[github] skipping %s: %v\n", repo.URL, err)
			continue
		}

		isPublic, err := CheckVisibility(info)
		if err != nil {
			var rle *RateLimitError
			if errors.As(err, &rle) {
				retryAfter := rle.RetryAfterSec
				d.mu.Lock()
				d.lastError = err.Error()
				d.mu.Unlock()
				return nil, &retryAfter, err
			}
			fmt.Printf("[github] error checking visibility for %s: %v\n", repo.URL, err)
			continue
		}
		if !isPublic {
			fmt.Printf("[github] skipping private/missing repo: %s\n", repo.URL)
			continue
		}

		publicRepos = append(publicRepos, repo.URL)
		repoMap[repo.URL] = repo
	}

	// Step 2: Fetch PRs for public repos
	var allPRs []contracts.PullRequest
	for _, repoURL := range publicRepos {
		repo := repoMap[repoURL]
		info, _ := ParseRepoURL(repoURL) // already validated above

		prs, err := FetchOpenPRs(info, repo.Name, repoURL)
		if err != nil {
			var rle *RateLimitError
			if errors.As(err, &rle) {
				retryAfter := rle.RetryAfterSec
				d.mu.Lock()
				d.lastError = err.Error()
				d.mu.Unlock()
				return nil, &retryAfter, err
			}
			fmt.Printf("[github] error fetching PRs for %s: %v\n", repoURL, err)
			continue
		}
		allPRs = append(allPRs, prs...)
	}

	// Step 3: Update state
	now := time.Now()
	d.mu.Lock()
	d.pullRequests = allPRs
	d.publicRepos = publicRepos
	d.lastFetchedAt = &now
	d.lastError = ""
	d.mu.Unlock()

	fmt.Printf("[github] discovered %d PRs across %d public repos\n", len(allPRs), len(publicRepos))
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
func (d *Discovery) SetTarget(target string, getRepos func() []config.Repo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	enabled := target != ""
	wasEnabled := d.ticker != nil

	if enabled && !wasEnabled {
		// Start polling
		d.getRepos = getRepos
		d.stopChan = make(chan struct{})
		d.ticker = time.NewTicker(1 * time.Hour)
		go d.poll()
		// Trigger immediate refresh
		go d.Refresh(getRepos())
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
}

// poll runs the hourly refresh loop until stopped.
func (d *Discovery) poll() {
	for {
		select {
		case <-d.ticker.C:
			d.mu.RLock()
			getRepos := d.getRepos
			d.mu.RUnlock()
			if getRepos != nil {
				d.Refresh(getRepos())
			}
		case <-d.stopChan:
			return
		}
	}
}
