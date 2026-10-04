package ado

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ProjectInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Projects lists every project in the organization.
func (c *Client) Projects(ctx context.Context) ([]ProjectInfo, error) {
	var resp struct {
		Value []ProjectInfo `json:"value"`
	}
	err := c.get(ctx, "/_apis/projects", url.Values{"$top": {"1000"}, "api-version": {apiVersion}}, &resp)
	return resp.Value, err
}

// RecentProjectIDs returns the projects Me visited lately, most recent
// first, from the same (undocumented) data provider the web home page uses.
func (c *Client) RecentProjectIDs(ctx context.Context) ([]string, error) {
	const provider = "ms.vss-tfs-web.project-mru-data-provider"
	body := map[string]any{
		"contributionIds":     []string{provider},
		"dataProviderContext": map[string]any{"properties": map[string]any{}},
	}
	var resp struct {
		DataProviders map[string]struct {
			Projects []struct {
				ProjectID string `json:"projectId"`
			} `json:"projects"`
		} `json:"dataProviders"`
	}
	if err := c.post(ctx, "/_apis/Contribution/HierarchyQuery", url.Values{"api-version": {"5.0-preview.1"}}, body, &resp); err != nil {
		return nil, err
	}
	var ids []string
	for _, p := range resp.DataProviders[provider].Projects {
		ids = append(ids, p.ProjectID)
	}
	return ids, nil
}

// AllActivePRs lists everyone's active pull requests across the organization.
func (c *Client) AllActivePRs(ctx context.Context) ([]PullRequest, error) {
	var resp struct {
		Value []PullRequest `json:"value"`
	}
	q := url.Values{"searchCriteria.status": {"active"}, "$top": {"1000"}, "api-version": {apiVersion}}
	err := c.get(ctx, "/_apis/git/pullrequests", q, &resp)
	return resp.Value, err
}

type Repo struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Project       Project `json:"project"`
	DefaultBranch string  `json:"defaultBranch"`
	Size          int64   `json:"size"`
	RemoteURL     string  `json:"remoteUrl"`
	SSHURL        string  `json:"sshUrl"`
	WebURL        string  `json:"webUrl"`
	IsDisabled    bool    `json:"isDisabled"`
}

func (r Repo) DefaultBranchName() string { return strings.TrimPrefix(r.DefaultBranch, "refs/heads/") }

func (r Repo) path() string {
	return fmt.Sprintf("/%s/_apis/git/repositories/%s", url.PathEscape(r.Project.ID), r.ID)
}

// Repos lists every repo in the organization.
func (c *Client) Repos(ctx context.Context) ([]Repo, error) {
	var resp struct {
		Value []Repo `json:"value"`
	}
	err := c.get(ctx, "/_apis/git/repositories", v71(), &resp)
	return resp.Value, err
}

// LastPush returns when the repo was last pushed to; zero for an empty repo.
func (c *Client) LastPush(ctx context.Context, r Repo) (time.Time, error) {
	var resp struct {
		Value []struct {
			Date time.Time `json:"date"`
		} `json:"value"`
	}
	err := c.get(ctx, r.path()+"/pushes", url.Values{"$top": {"1"}, "api-version": {apiVersion}}, &resp)
	if err != nil || len(resp.Value) == 0 {
		return time.Time{}, err
	}
	return resp.Value[0].Date, nil
}

type Branch struct {
	Name          string `json:"name"`
	AheadCount    int    `json:"aheadCount"`
	BehindCount   int    `json:"behindCount"`
	IsBaseVersion bool   `json:"isBaseVersion"`
	Commit        struct {
		ID     string `json:"commitId"`
		Author struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
		Comment string `json:"comment"`
	} `json:"commit"`
}

// Branches lists a repo's branches with ahead/behind counts against its
// default branch.
func (c *Client) Branches(ctx context.Context, r Repo) ([]Branch, error) {
	if r.DefaultBranch == "" {
		return nil, nil // empty repo
	}
	q := url.Values{
		"baseVersionDescriptor.version":     {r.DefaultBranchName()},
		"baseVersionDescriptor.versionType": {"branch"},
		"api-version":                       {apiVersion},
	}
	var resp struct {
		Value []Branch `json:"value"`
	}
	err := c.get(ctx, r.path()+"/stats/branches", q, &resp)
	return resp.Value, err
}

// Run is one execution of a pipeline.
type Run struct {
	ID           int       `json:"id"`
	BuildNumber  string    `json:"buildNumber"`
	Status       string    `json:"status"` // notStarted, inProgress, completed, cancelling, postponed
	Result       string    `json:"result"` // succeeded, partiallySucceeded, failed, canceled
	SourceBranch string    `json:"sourceBranch"`
	Reason       string    `json:"reason"`
	RequestedFor Identity  `json:"requestedFor"`
	QueueTime    time.Time `json:"queueTime"`
	StartTime    time.Time `json:"startTime"`
	FinishTime   time.Time `json:"finishTime"`
	Definition   struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"definition"`
}

// Branch is the run's source branch without "refs/heads/".
func (r Run) Branch() string { return strings.TrimPrefix(r.SourceBranch, "refs/heads/") }

type Pipeline struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	LatestBuild *Run   `json:"latestBuild"`
}

// Pipelines lists a project's build pipelines with their latest run.
func (c *Client) Pipelines(ctx context.Context, projectID string) ([]Pipeline, error) {
	var resp struct {
		Value []Pipeline `json:"value"`
	}
	q := url.Values{"includeLatestBuilds": {"true"}, "api-version": {apiVersion}}
	err := c.get(ctx, "/"+url.PathEscape(projectID)+"/_apis/build/definitions", q, &resp)
	return resp.Value, err
}

// Runs lists a pipeline's latest runs, newest first.
func (c *Client) Runs(ctx context.Context, projectID string, pipelineID int) ([]Run, error) {
	var resp struct {
		Value []Run `json:"value"`
	}
	q := url.Values{
		"definitions": {strconv.Itoa(pipelineID)},
		"$top":        {"50"},
		"queryOrder":  {"queueTimeDescending"},
		"api-version": {apiVersion},
	}
	err := c.get(ctx, "/"+url.PathEscape(projectID)+"/_apis/build/builds", q, &resp)
	return resp.Value, err
}

// Run fetches one run, for refreshing its status while it is in progress.
func (c *Client) Run(ctx context.Context, projectID string, runID int) (Run, error) {
	var r Run
	err := c.get(ctx, fmt.Sprintf("/%s/_apis/build/builds/%d", url.PathEscape(projectID), runID), v71(), &r)
	return r, err
}

// TimelineRecord is a stage, job or step of a run.
type TimelineRecord struct {
	ID         string    `json:"id"`
	ParentID   string    `json:"parentId"`
	Type       string    `json:"type"` // Stage, Phase, Job, Task, Checkpoint
	Name       string    `json:"name"`
	State      string    `json:"state"` // pending, inProgress, completed
	Result     string    `json:"result"`
	Order      int       `json:"order"`
	StartTime  time.Time `json:"startTime"`
	FinishTime time.Time `json:"finishTime"`
	Log        *struct {
		ID int `json:"id"`
	} `json:"log"`
}

func (c *Client) Timeline(ctx context.Context, projectID string, runID int) ([]TimelineRecord, error) {
	var resp struct {
		Records []TimelineRecord `json:"records"`
	}
	err := c.get(ctx, fmt.Sprintf("/%s/_apis/build/builds/%d/timeline", url.PathEscape(projectID), runID), v71(), &resp)
	return resp.Records, err
}

// LogLines returns a run log from startLine (1-based) to the end; following
// a live log asks only for lines after the ones already shown.
func (c *Client) LogLines(ctx context.Context, projectID string, runID, logID, startLine int) ([]string, error) {
	q := url.Values{"api-version": {apiVersion}}
	if startLine > 1 {
		q.Set("startLine", strconv.Itoa(startLine))
	}
	path := fmt.Sprintf("/%s/_apis/build/builds/%d/logs/%d", url.PathEscape(projectID), runID, logID)
	body, err := c.getRaw(ctx, path, q, "text/plain")
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// getRaw GETs a non-JSON body, at most maxBlobBytes of it.
func (c *Client) getRaw(ctx context.Context, path string, query url.Values, accept string) ([]byte, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	u := "https://dev.azure.com/" + url.PathEscape(c.Org) + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET %s: %s: %s", path, resp.Status, apiMessage(msg))
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBlobBytes))
}

// Web links for things the projects page can open in the browser.

func (c *Client) ProjectURL(p ProjectInfo) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s", url.PathEscape(c.Org), url.PathEscape(p.Name))
}

func (c *Client) BranchURL(r Repo, branch string) string {
	return r.WebURL + "?version=GB" + url.QueryEscape(branch)
}

func (c *Client) PipelineURL(projectName string, pipelineID int) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_build?definitionId=%d", url.PathEscape(c.Org), url.PathEscape(projectName), pipelineID)
}

func (c *Client) RunURL(projectName string, runID int) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_build/results?buildId=%d", url.PathEscape(c.Org), url.PathEscape(projectName), runID)
}
