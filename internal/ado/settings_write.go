package ado

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Repository and variable group writes for the Settings tab.

func (c *Client) repoPath(projectID string) string {
	return "/" + url.PathEscape(projectID) + "/_apis/git/repositories"
}

// UpdateRepo renames a repo and/or sets its default branch ("" leaves one
// alone).
func (c *Client) UpdateRepo(ctx context.Context, r Repo, name, defaultBranch string) error {
	body := map[string]any{}
	if name != "" {
		body["name"] = name
	}
	if defaultBranch != "" {
		body["defaultBranch"] = "refs/heads/" + defaultBranch
	}
	return c.do(ctx, http.MethodPatch, c.repoPath(r.Project.ID)+"/"+r.ID, v71(), body, nil)
}

// ProjectRepos lists a project's repos straight from the project: unlike
// the organization-wide list, it shows a repo the moment it is created.
func (c *Client) ProjectRepos(ctx context.Context, projectID string) ([]Repo, error) {
	var resp struct {
		Value []Repo `json:"value"`
	}
	err := c.get(ctx, c.repoPath(projectID), v71(), &resp)
	sort.Slice(resp.Value, func(i, j int) bool { return strings.ToLower(resp.Value[i].Name) < strings.ToLower(resp.Value[j].Name) })
	return resp.Value, err
}

func (c *Client) CreateRepo(ctx context.Context, projectID, name string) error {
	body := map[string]any{"name": name, "project": map[string]string{"id": projectID}}
	return c.do(ctx, http.MethodPost, c.repoPath(projectID), v71(), body, nil)
}

// DeleteRepo moves a repo to the project's recycle bin, where it can be
// restored for 30 days.
func (c *Client) DeleteRepo(ctx context.Context, r Repo) error {
	return c.do(ctx, http.MethodDelete, c.repoPath(r.Project.ID)+"/"+r.ID, v71(), nil, nil)
}

func (c *Client) varGroupPath(projectID string) string {
	return "/" + url.PathEscape(projectID) + "/_apis/distributedtask/variablegroups"
}

// EditVariableGroup reads group id as stored, lets edit change it, and
// writes it back whole, so fields lazdo doesn't know about are kept. A
// secret variable's value comes back empty; sent back that way, the
// stored secret stays.
func (c *Client) EditVariableGroup(ctx context.Context, projectID string, id int, edit func(g map[string]any)) error {
	var g map[string]any
	path := c.varGroupPath(projectID) + "/" + strconv.Itoa(id)
	if err := c.get(ctx, path, v71(), &g); err != nil {
		return err
	}
	edit(g)
	return c.do(ctx, http.MethodPut, path, v71(), g, nil)
}

// CreateVariableGroup makes a group with its first variable: Azure DevOps
// refuses an empty one.
func (c *Client) CreateVariableGroup(ctx context.Context, p ProjectInfo, name, description string, first Variable) error {
	body := map[string]any{
		"name": name, "description": description, "type": "Vsts",
		"variables": map[string]any{first.Name: map[string]any{"value": first.Value, "isSecret": first.Secret}},
		"variableGroupProjectReferences": []any{map[string]any{
			"name": name, "description": description,
			"projectReference": map[string]string{"id": p.ID, "name": p.Name},
		}},
	}
	return c.do(ctx, http.MethodPost, c.varGroupPath(p.ID), v71(), body, nil)
}

func (c *Client) DeleteVariableGroup(ctx context.Context, projectID string, id int) error {
	q := url.Values{"projectIds": {projectID}, "api-version": {apiVersion}}
	return c.do(ctx, http.MethodDelete, c.varGroupPath(projectID)+"/"+strconv.Itoa(id), q, nil, nil)
}

// RepoBranchExists reports whether repo r has the branch, before making it
// the default.
func (c *Client) RepoBranchExists(ctx context.Context, r Repo, branch string) (bool, error) {
	var resp struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	q := url.Values{"filter": {"heads/" + branch}, "api-version": {apiVersion}}
	if err := c.get(ctx, r.path()+"/refs", q, &resp); err != nil {
		return false, err
	}
	for _, ref := range resp.Value {
		if ref.Name == "refs/heads/"+branch {
			return true, nil
		}
	}
	return false, nil
}
