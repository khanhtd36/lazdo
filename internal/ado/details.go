package ado

import (
	"context"
	"fmt"
	"net/url"
)

type BuildState int

const (
	BuildNone BuildState = iota
	BuildRunning
	BuildPassed
	BuildFailed
)

// Build summarizes the PR's enabled build policies. There is no batch
// endpoint for policy evaluations, so this is one call per PR.
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
				IsEnabled bool `json:"isEnabled"`
				Type      struct {
					DisplayName string `json:"displayName"`
				} `json:"type"`
			} `json:"configuration"`
		} `json:"value"`
	}
	if err := c.get(ctx, path, q, &resp); err != nil {
		return BuildNone, err
	}
	state := BuildNone
	for _, e := range resp.Value {
		if !e.Configuration.IsEnabled || e.Configuration.Type.DisplayName != "Build" {
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
