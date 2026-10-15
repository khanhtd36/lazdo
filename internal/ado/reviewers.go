package ado

import (
	"context"
	"net/http"
	"net/url"
)

// SetReviewer adds a reviewer (vote VoteNone), or makes one required or
// optional. The vote must go along as it stands: one left out is reset.
func (c *Client) SetReviewer(ctx context.Context, pr PullRequest, id string, required bool, vote int) error {
	body := map[string]any{"isRequired": required, "vote": vote}
	return c.do(ctx, http.MethodPut, pr.prPath()+"/reviewers/"+url.PathEscape(id), v71(), body, nil)
}

// RemoveReviewer takes a reviewer, and their vote, off the pull request.
func (c *Client) RemoveReviewer(ctx context.Context, pr PullRequest, id string) error {
	return c.do(ctx, http.MethodDelete, pr.prPath()+"/reviewers/"+url.PathEscape(id), v71(), nil, nil)
}
