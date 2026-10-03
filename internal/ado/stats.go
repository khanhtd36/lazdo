package ado

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Stats are the per-user counters behind the "My pull requests" page:
// comment counts and what changed since the user last opened the PR.
type Stats struct {
	LastUpdated    time.Time
	Comments       int // human comment threads
	ActiveComments int // unresolved human comment threads
	// Visited is false when the user never opened the PR; the New* counts
	// are zero then.
	Visited     bool
	NewComments int
	NewPushes   int
	NewVotes    int
}

type statsCounts struct {
	Text      int `json:"Text"`
	Iteration int `json:"Iteration"`
	Vote      int `json:"Vote"`
}

// Artifact IDs embed "/" as "%2F"; the format comes from the web UI's own
// pull request list.
func (pr PullRequest) statsArtifactID() string {
	return fmt.Sprintf("vstfs:///Git/PullRequestId/%s%%2F%s%%2F%d", pr.Repository.Project.ID, pr.Repository.ID, pr.ID)
}

func (pr PullRequest) discussionArtifactID() string {
	if pr.SupportsIterations {
		return fmt.Sprintf("vstfs:///CodeReview/ReviewId/%s%%2F%d", pr.Repository.Project.ID, pr.CodeReviewID)
	}
	return fmt.Sprintf("vstfs:///CodeReview/CodeReviewId/%s%%2F%d", pr.Repository.Project.ID, pr.ID)
}

// Stats fetches stats for many PRs in one request, keyed by PR ID.
func (c *Client) Stats(ctx context.Context, prs []PullRequest) (map[int]Stats, error) {
	type artifact struct {
		ArtifactID           string `json:"artifactId"`
		DiscussionArtifactID string `json:"discussionArtifactId"`
	}
	body := make([]artifact, len(prs))
	byArtifact := make(map[string]int, len(prs))
	for i, pr := range prs {
		body[i] = artifact{pr.statsArtifactID(), pr.discussionArtifactID()}
		byArtifact[body[i].ArtifactID] = pr.ID
	}

	var resp struct {
		Value []struct {
			ArtifactID          string       `json:"artifactId"`
			LastUpdatedDate     time.Time    `json:"lastUpdatedDate"`
			CommentsCount       statsCounts  `json:"commentsCount"`
			ActiveCommentsCount statsCounts  `json:"activeCommentsCount"`
			NewCommentsCount    *statsCounts `json:"newCommentsCount"`
		} `json:"value"`
	}
	q := url.Values{"includeUpdatesSinceLastVisit": {"true"}, "api-version": {"7.1-preview.1"}}
	if err := c.post(ctx, "/_apis/visits/artifactStatsBatch", q, body, &resp); err != nil {
		return nil, err
	}

	out := make(map[int]Stats, len(resp.Value))
	for _, v := range resp.Value {
		id, ok := byArtifact[v.ArtifactID]
		if !ok {
			continue
		}
		s := Stats{
			LastUpdated:    v.LastUpdatedDate,
			Comments:       v.CommentsCount.Text,
			ActiveComments: v.ActiveCommentsCount.Text,
			Visited:        v.NewCommentsCount != nil,
		}
		if n := v.NewCommentsCount; n != nil {
			s.NewComments, s.NewPushes, s.NewVotes = n.Text, n.Iteration, n.Vote
		}
		out[id] = s
	}
	return out, nil
}
