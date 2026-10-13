package ado

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// BuildID is the run a build policy last queued; 0 before it queued one.
func (p Policy) BuildID() int {
	id, _ := p.Context["buildId"].(float64)
	return int(id)
}

// BuildDefinitionID is the pipeline a build policy runs.
func (p Policy) BuildDefinitionID() int {
	id, _ := p.Configuration.Settings["buildDefinitionId"].(float64)
	return int(id)
}

func evaluationPath(pr PullRequest, evaluationID string) string {
	return "/" + url.PathEscape(pr.Repository.Project.ID) + "/_apis/policy/evaluations/" + url.PathEscape(evaluationID)
}

func policyVersion() url.Values { return url.Values{"api-version": {"7.1-preview.1"}} }

// RequeuePolicy runs a policy again, as the web's Queue button does; a build
// policy queues a new run.
func (c *Client) RequeuePolicy(ctx context.Context, pr PullRequest, evaluationID string) error {
	return c.do(ctx, http.MethodPatch, evaluationPath(pr, evaluationID), policyVersion(), nil, nil)
}

// PolicyEvaluation fetches one policy's evaluation, to watch for the run a
// re-queue starts.
func (c *Client) PolicyEvaluation(ctx context.Context, pr PullRequest, evaluationID string) (Policy, error) {
	var p Policy
	err := c.get(ctx, evaluationPath(pr, evaluationID), policyVersion(), &p)
	return p, err
}

// PRRuns lists the runs on the pull request's merge ref, newest first: its
// build policies' and any pipeline a pull request trigger started.
func (c *Client) PRRuns(ctx context.Context, pr PullRequest) ([]Run, error) {
	var resp struct {
		Value []Run `json:"value"`
	}
	q := url.Values{
		"branchName":  {fmt.Sprintf("refs/pull/%d/merge", pr.ID)},
		"$top":        {"50"},
		"queryOrder":  {"queueTimeDescending"},
		"api-version": {apiVersion},
	}
	err := c.get(ctx, "/"+url.PathEscape(pr.Repository.Project.ID)+"/_apis/build/builds", q, &resp)
	return resp.Value, err
}
