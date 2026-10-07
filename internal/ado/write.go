package ado

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// MergeType is how Complete merges the source branch, in the order Azure
// DevOps lists them.
type MergeType int

const (
	MergeNoFastForward MergeType = iota
	MergeSquash
	MergeRebase
	MergeRebaseMerge
)

var AllMergeTypes = []MergeType{MergeNoFastForward, MergeSquash, MergeRebase, MergeRebaseMerge}

func (m MergeType) Title() string {
	switch m {
	case MergeNoFastForward:
		return "Merge (no fast-forward)"
	case MergeSquash:
		return "Squash commit"
	case MergeRebase:
		return "Rebase and fast-forward"
	case MergeRebaseMerge:
		return "Semi-linear merge"
	default:
		return "?"
	}
}

func (m MergeType) apiValue() string {
	switch m {
	case MergeNoFastForward:
		return "noFastForward"
	case MergeSquash:
		return "squash"
	case MergeRebase:
		return "rebase"
	case MergeRebaseMerge:
		return "rebaseMerge"
	default:
		return ""
	}
}

// settingKey is the merge-type policy setting that allows this type.
func (m MergeType) settingKey() string {
	switch m {
	case MergeNoFastForward:
		return "allowNoFastForward"
	case MergeSquash:
		return "allowSquash"
	case MergeRebase:
		return "allowRebase"
	case MergeRebaseMerge:
		return "allowRebaseMerge"
	default:
		return ""
	}
}

const policyTypeMergeStrategy = "fa4e907d-c16b-4a4c-9dfa-4916e5d171ab"

// AllowedMergeTypes are the merge types the target branch's policy allows;
// all of them when it has no merge-type policy.
func (d *PRDetail) AllowedMergeTypes() []MergeType {
	for _, p := range d.Policies {
		if p.Configuration.Type.ID != policyTypeMergeStrategy {
			continue
		}
		var out []MergeType
		for _, m := range AllMergeTypes {
			if allowed, _ := p.Configuration.Settings[m.settingKey()].(bool); allowed {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return AllMergeTypes
}

type CompletionOptions struct {
	MergeType           MergeType
	DeleteSourceBranch  bool
	TransitionWorkItems bool
	// MergeCommitMessage, when empty, gets the server's "Merge pull request
	// N from <branch> into <target>", not the web UI's "Merged PR N: title".
	MergeCommitMessage string
	BypassReason       string // non-empty overrides blocking policies
}

// MaxCompletionOptionsLength caps the completion options as Azure DevOps
// encodes them, merge commit message included. Its count runs a little
// above json.Marshal's (4069 for options this measures at 4050), so
// callers keep a margin.
const MaxCompletionOptionsLength = 4000

// EncodedLength estimates the server's count of these options.
func (o CompletionOptions) EncodedLength() int {
	b, _ := json.Marshal(o.body())
	return len(b)
}

func (o CompletionOptions) body() map[string]any {
	b := map[string]any{
		"mergeStrategy":       o.MergeType.apiValue(),
		"deleteSourceBranch":  o.DeleteSourceBranch,
		"transitionWorkItems": o.TransitionWorkItems,
	}
	if o.MergeCommitMessage != "" {
		b["mergeCommitMessage"] = o.MergeCommitMessage
	}
	if o.BypassReason != "" {
		b["bypassPolicy"] = true
		b["bypassReason"] = o.BypassReason
	}
	return b
}

func (c *Client) patchPR(ctx context.Context, pr PullRequest, body map[string]any) error {
	return c.do(ctx, http.MethodPatch, pr.prPath(), v71(), body, nil)
}

// Vote sets Me's vote, adding Me as an optional reviewer if needed.
func (c *Client) Vote(ctx context.Context, pr PullRequest, meID string, vote int) error {
	body := map[string]any{"vote": vote}
	if me, ok := pr.ReviewerFor(meID); ok {
		body["isRequired"] = me.IsRequired
	}
	return c.do(ctx, http.MethodPut, pr.prPath()+"/reviewers/"+meID, v71(), body, nil)
}

func (c *Client) SetAutoComplete(ctx context.Context, pr PullRequest, meID string, o CompletionOptions) error {
	return c.patchPR(ctx, pr, map[string]any{
		"autoCompleteSetBy": map[string]string{"id": meID},
		"completionOptions": o.body(),
	})
}

func (c *Client) CancelAutoComplete(ctx context.Context, pr PullRequest) error {
	return c.patchPR(ctx, pr, map[string]any{
		"autoCompleteSetBy": map[string]string{"id": "00000000-0000-0000-0000-000000000000"},
	})
}

func (c *Client) Complete(ctx context.Context, d *PRDetail, o CompletionOptions) error {
	return c.patchPR(ctx, d.PullRequest, map[string]any{
		"status":                "completed",
		"lastMergeSourceCommit": map[string]string{"commitId": d.LastMergeSource.ID},
		"completionOptions":     o.body(),
	})
}

// Pull request text limits. A longer description is refused; a longer
// title is silently cut to 400 characters, so callers check first.
const (
	MaxTitleLength       = 400
	MaxDescriptionLength = 4000
)

// EditPR changes the title and/or the description; a nil one is left alone.
func (c *Client) EditPR(ctx context.Context, pr PullRequest, title, description *string) error {
	body := map[string]any{}
	if title != nil {
		body["title"] = *title
	}
	if description != nil {
		body["description"] = *description
	}
	return c.patchPR(ctx, pr, body)
}

func (c *Client) SetDraft(ctx context.Context, pr PullRequest, draft bool) error {
	return c.patchPR(ctx, pr, map[string]any{"isDraft": draft})
}

func (c *Client) Abandon(ctx context.Context, pr PullRequest) error {
	return c.patchPR(ctx, pr, map[string]any{"status": "abandoned"})
}

// ThreadStatuses are the states a human thread can be set to.
var ThreadStatuses = []string{"active", "pending", "fixed", "wontFix", "closed", "byDesign"}

func ThreadStatusTitle(s string) string {
	switch s {
	case "active":
		return "Active"
	case "pending":
		return "Pending"
	case "fixed":
		return "Resolved"
	case "wontFix":
		return "Won't fix"
	case "closed":
		return "Closed"
	case "byDesign":
		return "By design"
	default:
		return s
	}
}

func (c *Client) NewThread(ctx context.Context, pr PullRequest, content string) error {
	body := map[string]any{
		"comments": []map[string]any{{"parentCommentId": 0, "content": content, "commentType": "text"}},
		"status":   "active",
	}
	return c.do(ctx, http.MethodPost, pr.prPath()+"/threads", v71(), body, nil)
}

func (c *Client) Reply(ctx context.Context, pr PullRequest, t Thread, content string) error {
	parent := 0
	if live := t.LiveComments(); len(live) > 0 {
		parent = live[0].ID
	}
	body := map[string]any{"parentCommentId": parent, "content": content, "commentType": "text"}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("%s/threads/%d/comments", pr.prPath(), t.ID), v71(), body, nil)
}

func (c *Client) SetThreadStatus(ctx context.Context, pr PullRequest, t Thread, status string) error {
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("%s/threads/%d", pr.prPath(), t.ID), v71(),
		map[string]any{"status": status}, nil)
}

func (c *Client) EditComment(ctx context.Context, pr PullRequest, t Thread, cm Comment, content string) error {
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("%s/threads/%d/comments/%d", pr.prPath(), t.ID, cm.ID), v71(),
		map[string]any{"content": content}, nil)
}

func (c *Client) DeleteComment(ctx context.Context, pr PullRequest, t Thread, cm Comment) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("%s/threads/%d/comments/%d", pr.prPath(), t.ID, cm.ID), v71(), nil, nil)
}
