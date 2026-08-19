//go:build !nogithub

package github

import (
	"context"
	"fmt"
	"net/url"
)

// BranchPR identifies the open pull request for a branch head.
type BranchPR struct {
	Number  int
	HTMLURL string
}

// FetchOpenPRForBranch returns the open PR in the base repo whose head is
// `head` ("owner:branch"), or nil when none exists. Unlike FetchOpenPRs this
// is authenticated, so it works for private repos.
func FetchOpenPRForBranch(ctx context.Context, token string, base RepoInfo, head string) (*BranchPR, error) {
	path := fmt.Sprintf("/repos/%s/pulls?state=open&per_page=1&head=%s", base.APIPath(), url.QueryEscape(head))
	var prs []struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := doActionsGET(ctx, token, path, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &BranchPR{Number: prs[0].Number, HTMLURL: prs[0].HTMLURL}, nil
}
