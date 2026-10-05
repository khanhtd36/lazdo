package ado

import (
	"context"
	"fmt"
	"net/url"
)

// Policy types the dashboard and the detail view tell apart.
const (
	PolicyTypeBuild  = "0609b952-1397-4640-95ec-e00a01b2c241"
	PolicyTypeStatus = "cbdc66da-9728-4af8-aada-9a5a32e4a226" // an external service's status check
)

type BuildState int

const (
	BuildNone BuildState = iota
	BuildRunning
	BuildPassed
	BuildFailed
)

// Build summarizes the PR's checks: its enabled build policies and its
// required external status checks. There is no batch endpoint for policy
// evaluations, so this is one call per PR.
func (c *Client) Build(ctx context.Context, pr PullRequest) (BuildState, error) {
	path := "/" + url.PathEscape(pr.Repository.Project.ID) + "/_apis/policy/evaluations"
	q := url.Values{
		"artifactId":  {fmt.Sprintf("vstfs:///CodeReview/CodeReviewId/%s/%d", pr.Repository.Project.ID, pr.ID)},
		"api-version": {"7.1-preview.1"},
	}
	var resp struct {
		Value []struct {
			Status        string `json:"status"`
			Configuration struct {
				IsEnabled  bool `json:"isEnabled"`
				IsBlocking bool `json:"isBlocking"`
				Type       struct {
					ID string `json:"id"`
				} `json:"type"`
			} `json:"configuration"`
		} `json:"value"`
	}
	if err := c.get(ctx, path, q, &resp); err != nil {
		return BuildNone, err
	}
	state := BuildNone
	for _, e := range resp.Value {
		cfg := e.Configuration
		build := cfg.Type.ID == PolicyTypeBuild
		external := cfg.Type.ID == PolicyTypeStatus && cfg.IsBlocking
		if !cfg.IsEnabled || (!build && !external) {
			continue
		}
		switch e.Status {
		case "rejected", "broken":
			return BuildFailed, nil
		case "running":
			state = BuildRunning
		case "approved":
			if state == BuildNone {
				state = BuildPassed
			}
		}
	}
	return state, nil
}
