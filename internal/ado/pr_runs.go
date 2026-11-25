package ado

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
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

// BranchRuns lists recent runs on the pull request's source branch.
func (c *Client) BranchRuns(ctx context.Context, pr PullRequest) ([]Run, error) {
	if pr.SourceRefName == "" || pr.Repository.Project.ID == "" {
		return nil, nil
	}
	var resp struct {
		Value []Run `json:"value"`
	}
	q := url.Values{
		"branchName":  {pr.SourceRefName},
		"$top":        {"30"},
		"queryOrder":  {"queueTimeDescending"},
		"api-version": {apiVersion},
	}
	if pr.Repository.ID != "" {
		q.Set("repositoryId", pr.Repository.ID)
		q.Set("repositoryType", "TfsGit")
	}
	err := c.get(ctx, "/"+url.PathEscape(pr.Repository.Project.ID)+"/_apis/build/builds", q, &resp)
	return resp.Value, err
}

// PRAllRuns fetches PR merge-ref runs and source-branch runs concurrently.
func (c *Client) PRAllRuns(ctx context.Context, pr PullRequest) (prRuns, branchRuns []Run, err error) {
	var (
		wg        sync.WaitGroup
		prErr     error
		branchErr error
	)
	wg.Go(func() {
		prRuns, prErr = c.PRRuns(ctx, pr)
	})
	wg.Go(func() {
		branchRuns, branchErr = c.BranchRuns(ctx, pr)
	})
	wg.Wait()
	switch {
	case prErr != nil && branchErr != nil:
		return nil, nil, prErr
	case prErr != nil:
		return nil, branchRuns, prErr
	case branchErr != nil:
		return prRuns, nil, branchErr
	}
	return prRuns, branchRuns, nil
}

func buildPath(projectID string, runID int) string {
	return "/" + url.PathEscape(projectID) + "/_apis/build/builds/" + strconv.Itoa(runID)
}

// CancelRun requests cancellation of an active or queued build run.
func (c *Client) CancelRun(ctx context.Context, projectID string, runID int) error {
	return c.do(ctx, http.MethodPatch, buildPath(projectID, runID), v71(), map[string]any{"status": "cancelling"}, nil)
}

// RetryRun retries failed jobs of an existing build run.
func (c *Client) RetryRun(ctx context.Context, projectID string, runID int) error {
	q := v71()
	q.Set("retry", "true")
	return c.do(ctx, http.MethodPatch, buildPath(projectID, runID), q, map[string]any{}, nil)
}

// QueueRun queues a new run of definitionID on sourceBranch with default parameters.
func (c *Client) QueueRun(ctx context.Context, projectID string, definitionID int, sourceBranch string) (Run, error) {
	body := map[string]any{
		"definition": map[string]int{"id": definitionID},
	}
	if sourceBranch != "" {
		body["sourceBranch"] = sourceBranch
	}
	var run Run
	err := c.post(ctx, "/"+url.PathEscape(projectID)+"/_apis/build/builds", v71(), body, &run)
	return run, err
}
