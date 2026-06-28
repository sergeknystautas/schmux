//go:build !nogithub

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ErrUnauthorized is returned when the GitHub API responds with 401.
var ErrUnauthorized = errors.New("github: unauthorized")

// ErrNotFound is returned when the GitHub API responds with 404.
var ErrNotFound = errors.New("github: not found")

// ErrForbidden is returned when the GitHub API responds with 403 for a reason
// other than rate limiting — a missing scope, a private repo, or an org
// OAuth-app/SSO access restriction. Reporting these as rate limits sends
// operators chasing a throttling problem that isn't there.
var ErrForbidden = errors.New("github: forbidden")

// IsUnauthorized reports whether the error is a GitHub 401.
func IsUnauthorized(err error) bool { return errors.Is(err, ErrUnauthorized) }

// IsNotFound reports whether the error is a GitHub 404.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// IsForbidden reports whether the error is a non-rate-limit GitHub 403.
func IsForbidden(err error) bool { return errors.Is(err, ErrForbidden) }

// forbiddenError classifies a 403/429 response. GitHub overloads 403 for both
// rate limits and genuine permission failures, so the status alone can't tell
// them apart. A real rate limit zeroes X-RateLimit-Remaining (primary) or
// sends Retry-After / a "rate limit" body (secondary); everything else is a
// permission error. A 429 is always a rate limit. The body is consumed.
func forbiddenError(resp *http.Response) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{RetryAfterSec: parseRetryAfter(resp)}
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.Header.Get("X-RateLimit-Remaining") == "0" ||
		resp.Header.Get("Retry-After") != "" ||
		bytes.Contains(bytes.ToLower(body), []byte("rate limit")) {
		return &RateLimitError{RetryAfterSec: parseRetryAfter(resp)}
	}
	return ErrForbidden
}

// Workflow represents a GitHub Actions workflow definition.
type Workflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"` // "active", "disabled_manually", ...
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID         int64  `json:"id"`
	WorkflowID int64  `json:"workflow_id"`
	RunNumber  int    `json:"run_number"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"head_sha"`
	HTMLURL    string `json:"html_url"`
	CreatedAt  string `json:"created_at"`
}

// WorkflowJob represents a GitHub Actions workflow job.
type WorkflowJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

func doActionsGET(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(resp.Body).Decode(out)
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden, http.StatusTooManyRequests:
		return forbiddenError(resp)
	case http.StatusNotFound:
		return ErrNotFound
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github actions: unexpected status %d: %s", resp.StatusCode, string(body))
	}
}

// ListWorkflows fetches the workflows defined in a repository.
func ListWorkflows(ctx context.Context, token string, info RepoInfo) ([]Workflow, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows?per_page=100", info.Owner, info.Repo)
	var env struct {
		Workflows []Workflow `json:"workflows"`
	}
	if err := doActionsGET(ctx, token, path, &env); err != nil {
		return nil, err
	}
	return env.Workflows, nil
}

// ListRepoRuns fetches the most recent runs across all workflows for a branch,
// newest first.
func ListRepoRuns(ctx context.Context, token string, info RepoInfo, branch string) ([]WorkflowRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?branch=%s&per_page=100",
		info.Owner, info.Repo, url.QueryEscape(branch))
	var env struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if err := doActionsGET(ctx, token, path, &env); err != nil {
		return nil, err
	}
	return env.WorkflowRuns, nil
}

// ListRunJobs fetches the jobs for a specific workflow run.
func ListRunJobs(ctx context.Context, token string, info RepoInfo, runID int64) ([]WorkflowJob, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", info.Owner, info.Repo, runID)
	var env struct {
		Jobs []WorkflowJob `json:"jobs"`
	}
	if err := doActionsGET(ctx, token, path, &env); err != nil {
		return nil, err
	}
	return env.Jobs, nil
}

// maxJobLogBytes caps a downloaded job log, keeping the tail — failures
// live at the end of CI logs.
const maxJobLogBytes = 2 << 20 // 2 MB

// DownloadJobLogs fetches the full plain-text log of a workflow job.
// GitHub answers with a 302 to a signed URL; the default client follows it
// and drops the Authorization header on the cross-host hop. Oversized logs
// are truncated keeping the tail.
func DownloadJobLogs(ctx context.Context, token string, info RepoInfo, jobID int64) ([]byte, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", info.Owner, info.Repo, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if len(data) > maxJobLogBytes {
			data = data[len(data)-maxJobLogBytes:]
		}
		return data, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, forbiddenError(resp)
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github actions: unexpected status %d: %s", resp.StatusCode, string(body))
	}
}
