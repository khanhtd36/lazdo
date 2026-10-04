package ado

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const apiVersion = "7.1"

type Identity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	UniqueName  string `json:"uniqueName"`
}

type Reviewer struct {
	Identity
	Vote        int  `json:"vote"`
	IsRequired  bool `json:"isRequired"`
	IsContainer bool `json:"isContainer"`
	HasDeclined bool `json:"hasDeclined"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Repository struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Project Project `json:"project"`
}

type PullRequest struct {
	ID                 int        `json:"pullRequestId"`
	CodeReviewID       int        `json:"codeReviewId"`
	SupportsIterations bool       `json:"supportsIterations"`
	Title              string     `json:"title"`
	IsDraft            bool       `json:"isDraft"`
	CreatedBy          Identity   `json:"createdBy"`
	CreationDate       time.Time  `json:"creationDate"`
	SourceRefName      string     `json:"sourceRefName"`
	TargetRefName      string     `json:"targetRefName"`
	MergeStatus        string     `json:"mergeStatus"`
	Repository         Repository `json:"repository"`
	Reviewers          []Reviewer `json:"reviewers"`
}

func (pr PullRequest) SourceBranch() string {
	return strings.TrimPrefix(pr.SourceRefName, "refs/heads/")
}

func (pr PullRequest) TargetBranch() string {
	return strings.TrimPrefix(pr.TargetRefName, "refs/heads/")
}

// AsRepo is the PR's repository as a Repo, for repo-level calls.
func (pr PullRequest) AsRepo() Repo {
	return Repo{ID: pr.Repository.ID, Name: pr.Repository.Name, Project: pr.Repository.Project}
}

// WebURL is the browser link to the PR.
func (pr PullRequest) WebURL(org string) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s/pullrequest/%d",
		url.PathEscape(org), url.PathEscape(pr.Repository.Project.Name),
		url.PathEscape(pr.Repository.Name), pr.ID)
}

// ReviewerFor returns the reviewer entry for the given identity, if any.
func (pr PullRequest) ReviewerFor(id string) (Reviewer, bool) {
	for _, r := range pr.Reviewers {
		if r.ID == id {
			return r, true
		}
	}
	return Reviewer{}, false
}

// Me returns the identity ID of the logged-in user. It must come from
// connectionData: the profile API returns a different GUID that the PR
// search does not recognize.
func (c *Client) Me(ctx context.Context) (Identity, error) {
	var resp struct {
		AuthenticatedUser struct {
			ID                  string `json:"id"`
			ProviderDisplayName string `json:"providerDisplayName"`
		} `json:"authenticatedUser"`
	}
	if err := c.get(ctx, "/_apis/connectionData", nil, &resp); err != nil {
		return Identity{}, err
	}
	return Identity{ID: resp.AuthenticatedUser.ID, DisplayName: resp.AuthenticatedUser.ProviderDisplayName}, nil
}

// ActivePRs searches active PRs across the whole organization. Pass
// role "reviewerId" or "creatorId".
func (c *Client) ActivePRs(ctx context.Context, role, identityID string) ([]PullRequest, error) {
	q := url.Values{
		"searchCriteria.status":  {"active"},
		"searchCriteria." + role: {identityID},
		"$top":                   {"1000"},
		"api-version":            {apiVersion},
	}
	var resp struct {
		Value []PullRequest `json:"value"`
	}
	if err := c.get(ctx, "/_apis/git/pullrequests", q, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}
