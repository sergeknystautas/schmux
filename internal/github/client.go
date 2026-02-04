package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

const (
	userAgent = "schmux"
	maxPRs    = 5
)

// apiBaseURL is the GitHub API base URL. Var for testing.
var apiBaseURL = "https://api.github.com"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// RateLimitError is returned when the GitHub API rate limit is exceeded.
type RateLimitError struct {
	RetryAfterSec int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub API rate limit exceeded, retry after %d seconds", e.RetryAfterSec)
}

// CheckVisibility checks whether a GitHub repo is public.
// Returns true if the repo is public, false if private or not found.
func CheckVisibility(info RepoInfo) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s", apiBaseURL, info.APIPath())
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check repo visibility: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		retryAfter := parseRetryAfter(resp)
		return false, &RateLimitError{RetryAfterSec: retryAfter}
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("unexpected status %d checking repo visibility: %s", resp.StatusCode, string(body))
	}

	var repoData struct {
		Private bool `json:"private"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoData); err != nil {
		return false, fmt.Errorf("failed to decode repo response: %w", err)
	}
	return !repoData.Private, nil
}

// FetchOpenPRs fetches open pull requests for a public GitHub repo.
func FetchOpenPRs(info RepoInfo, repoName, repoURL string) ([]contracts.PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls?state=open&per_page=%d", apiBaseURL, info.APIPath(), maxPRs)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PRs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		retryAfter := parseRetryAfter(resp)
		return nil, &RateLimitError{RetryAfterSec: retryAfter}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d fetching PRs: %s", resp.StatusCode, string(body))
	}

	var ghPRs []ghPullRequest
	if err := json.NewDecoder(resp.Body).Decode(&ghPRs); err != nil {
		return nil, fmt.Errorf("failed to decode PRs response: %w", err)
	}

	prs := make([]contracts.PullRequest, 0, len(ghPRs))
	for _, gh := range ghPRs {
		pr := contracts.PullRequest{
			Number:       gh.Number,
			Title:        gh.Title,
			Body:         gh.Body,
			State:        gh.State,
			RepoName:     repoName,
			RepoURL:      repoURL,
			SourceBranch: gh.Head.Ref,
			TargetBranch: gh.Base.Ref,
			Author:       gh.User.Login,
			CreatedAt:    gh.CreatedAt,
			HTMLURL:      gh.HTMLURL,
		}
		if gh.Head.Repo.Fork {
			pr.IsFork = true
			pr.ForkOwner = gh.Head.Repo.Owner.Login
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// ghPullRequest is the GitHub API pull request response shape.
type ghPullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref  string `json:"ref"`
		Repo struct {
			Fork  bool `json:"fork"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func parseRetryAfter(resp *http.Response) int {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil {
			return sec
		}
	}
	return 60
}
