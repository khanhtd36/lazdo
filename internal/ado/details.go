package ado

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type BuildState int

const (
	BuildNone BuildState = iota
	BuildRunning
	BuildPassed
	BuildFailed
)

// Details is the per-PR data the list endpoint lacks; each needs its own call.
type Details struct {
	CommentsTotal    int
	CommentsResolved int
	LastUpdated      time.Time
	// PushesSinceMyVote counts pushes after the user's latest vote; zero
	// when the user never voted.
	PushesSinceMyVote int
	Build             BuildState
}

type thread struct {
	Status          string    `json:"status"`
	IsDeleted       bool      `json:"isDeleted"`
	PublishedDate   time.Time `json:"publishedDate"`
	LastUpdatedDate time.Time `json:"lastUpdatedDate"`
	Properties      map[string]struct {
		Value any `json:"$value"`
	} `json:"properties"`
	Comments []struct {
		Author      Identity `json:"author"`
		CommentType string   `json:"commentType"`
	} `json:"comments"`
}

func (t thread) systemType() string {
	if p, ok := t.Properties["CodeReviewThreadType"]; ok {
		if s, ok := p.Value.(string); ok {
			return s
		}
	}
	return ""
}

func (t thread) isHumanComment() bool {
	return !t.IsDeleted && len(t.Comments) > 0 && t.Comments[0].CommentType != "system"
}

func (c *Client) Details(ctx context.Context, pr PullRequest, meID string) (Details, error) {
	d := Details{LastUpdated: pr.CreationDate}
	if err := c.fillFromThreads(ctx, pr, meID, &d); err != nil {
		return d, err
	}
	build, err := c.buildState(ctx, pr)
	if err != nil {
		return d, err
	}
	d.Build = build
	return d, nil
}

func (c *Client) fillFromThreads(ctx context.Context, pr PullRequest, meID string, d *Details) error {
	path := fmt.Sprintf("/%s/_apis/git/repositories/%s/pullRequests/%d/threads",
		url.PathEscape(pr.Repository.Project.ID), pr.Repository.ID, pr.ID)
	var resp struct {
		Value []thread `json:"value"`
	}
	if err := c.get(ctx, path, url.Values{"api-version": {apiVersion}}, &resp); err != nil {
		return err
	}

	var myLastVote time.Time
	for _, t := range resp.Value {
		if t.LastUpdatedDate.After(d.LastUpdated) {
			d.LastUpdated = t.LastUpdatedDate
		}
		if t.isHumanComment() {
			d.CommentsTotal++
			if t.Status != "active" && t.Status != "pending" {
				d.CommentsResolved++
			}
		}
		if t.systemType() == "VoteUpdate" && len(t.Comments) > 0 &&
			t.Comments[0].Author.ID == meID && t.PublishedDate.After(myLastVote) {
			myLastVote = t.PublishedDate
		}
	}
	if myLastVote.IsZero() {
		return nil
	}
	for _, t := range resp.Value {
		if t.systemType() == "RefUpdate" && t.PublishedDate.After(myLastVote) {
			d.PushesSinceMyVote++
		}
	}
	return nil
}

func (c *Client) buildState(ctx context.Context, pr PullRequest) (BuildState, error) {
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
		case "queued", "running":
			state = BuildRunning
		case "approved":
			if state == BuildNone {
				state = BuildPassed
			}
		}
	}
	return state, nil
}
