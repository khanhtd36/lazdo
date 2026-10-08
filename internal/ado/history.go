package ado

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RepoCommit is a commit in a repo's history.
type RepoCommit struct {
	ID      string `json:"commitId"`
	Comment string `json:"comment"`
	Author  struct {
		Name string    `json:"name"`
		Date time.Time `json:"date"`
	} `json:"author"`
	ChangeCounts map[string]int `json:"changeCounts"`
}

// Commits pages through a branch's history, newest first.
func (c *Client) Commits(ctx context.Context, r Repo, branch string, skip, top int) ([]RepoCommit, error) {
	q := url.Values{
		"searchCriteria.itemVersion.version":     {branch},
		"searchCriteria.itemVersion.versionType": {"branch"},
		"searchCriteria.$skip":                   {strconv.Itoa(skip)},
		"searchCriteria.$top":                    {strconv.Itoa(top)},
		"api-version":                            {apiVersion},
	}
	var resp struct {
		Value []RepoCommit `json:"value"`
	}
	err := c.get(ctx, r.path()+"/commits", q, &resp)
	return resp.Value, err
}

// CommitsBetween lists the commits newer has that older lacks: a tag's
// release changes. (The batch API reads itemVersion as the older side.)
func (c *Client) CommitsBetween(ctx context.Context, r Repo, older, newer string) ([]RepoCommit, error) {
	return c.commitsBetween(ctx, r, older, newer, "tag")
}

// CommitRange lists the commits newer has that older lacks, by commit ID:
// what moving a submodule from older to newer brings in.
func (c *Client) CommitRange(ctx context.Context, r Repo, older, newer string) ([]RepoCommit, error) {
	return c.commitsBetween(ctx, r, older, newer, "commit")
}

func (c *Client) commitsBetween(ctx context.Context, r Repo, older, newer, versionType string) ([]RepoCommit, error) {
	body := map[string]any{
		"itemVersion":    map[string]string{"version": older, "versionType": versionType},
		"compareVersion": map[string]string{"version": newer, "versionType": versionType},
	}
	var resp struct {
		Value []RepoCommit `json:"value"`
	}
	err := c.post(ctx, r.path()+"/commitsbatch", url.Values{"$top": {"1000"}, "api-version": {apiVersion}}, body, &resp)
	return resp.Value, err
}

// RangeChanges lists the files that differ between two commits.
func (c *Client) RangeChanges(ctx context.Context, r Repo, base, target string) ([]Change, error) {
	var out []Change
	for skip := 0; ; skip += 100 {
		q := url.Values{
			"baseVersion": {base}, "baseVersionType": {"commit"},
			"targetVersion": {target}, "targetVersionType": {"commit"},
			"$top": {"100"}, "$skip": {strconv.Itoa(skip)}, "api-version": {apiVersion},
		}
		var resp struct {
			Changes []Change `json:"changes"`
		}
		if err := c.get(ctx, r.path()+"/diffs/commits", q, &resp); err != nil {
			return out, err
		}
		for _, ch := range resp.Changes {
			if !ch.Item.IsFolder {
				out = append(out, ch.withPath())
			}
		}
		if len(resp.Changes) < 100 {
			return out, nil
		}
	}
}

// Tag is a tag ref; CommitID is the commit it points at, ObjectID the tag
// object for annotated tags (equal to CommitID for lightweight ones).
type Tag struct {
	Name     string
	ObjectID string
	CommitID string
}

// Annotated reports whether the tag has its own tag object.
func (t Tag) Annotated() bool { return t.ObjectID != t.CommitID }

// Tags lists every tag in a repo.
func (c *Client) Tags(ctx context.Context, r Repo) ([]Tag, error) {
	var resp struct {
		Value []struct {
			Name           string `json:"name"`
			ObjectID       string `json:"objectId"`
			PeeledObjectID string `json:"peeledObjectId"`
		} `json:"value"`
	}
	q := url.Values{"filter": {"tags/"}, "peelTags": {"true"}, "$top": {"10000"}, "api-version": {apiVersion}}
	if err := c.get(ctx, r.path()+"/refs", q, &resp); err != nil {
		return nil, err
	}
	tags := make([]Tag, 0, len(resp.Value))
	for _, v := range resp.Value {
		t := Tag{Name: strings.TrimPrefix(v.Name, "refs/tags/"), ObjectID: v.ObjectID, CommitID: v.PeeledObjectID}
		if t.CommitID == "" {
			t.CommitID = v.ObjectID // lightweight: the ref points at the commit
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// TagInfo is an annotated tag's tagger, date and message.
type TagInfo struct {
	Message string
	Tagger  string
	Date    time.Time
}

func (c *Client) TagInfo(ctx context.Context, r Repo, t Tag) (TagInfo, error) {
	var resp struct {
		Message  string `json:"message"`
		TaggedBy struct {
			Name string `json:"name"`
			Date string `json:"date"` // may lack a time zone: "2026-10-02T06:38:59"
		} `json:"taggedBy"`
	}
	err := c.get(ctx, r.path()+"/annotatedtags/"+t.ObjectID, url.Values{"api-version": {"7.1-preview.1"}}, &resp)
	return TagInfo{Message: strings.TrimSpace(resp.Message), Tagger: resp.TaggedBy.Name, Date: parseLooseTime(resp.TaggedBy.Date)}, err
}

// parseLooseTime reads RFC 3339, or the same without a zone (taken as UTC).
func parseLooseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// CreateTag tags a commit: annotated when message is set, lightweight
// otherwise.
func (c *Client) CreateTag(ctx context.Context, r Repo, name, commitID, message string) error {
	if message != "" {
		body := map[string]any{
			"name":         name,
			"taggedObject": map[string]string{"objectId": commitID},
			"message":      message,
		}
		return c.post(ctx, r.path()+"/annotatedtags", url.Values{"api-version": {"7.1-preview.1"}}, body, nil)
	}
	return c.updateRef(ctx, r, "refs/tags/"+name, zeroID, commitID)
}

// DeleteTag removes a tag; objectID is what the tag ref points at now.
func (c *Client) DeleteTag(ctx context.Context, r Repo, t Tag) error {
	return c.updateRef(ctx, r, "refs/tags/"+t.Name, t.ObjectID, zeroID)
}

// DeleteBranch removes a branch at the commit it is known to point at, so
// a push since then makes the delete fail instead of losing work.
func (c *Client) DeleteBranch(ctx context.Context, r Repo, name, commitID string) error {
	return c.updateRef(ctx, r, "refs/heads/"+name, commitID, zeroID)
}

const zeroID = "0000000000000000000000000000000000000000"

// updateRef creates (old zero), moves, or deletes (new zero) one ref.
func (c *Client) updateRef(ctx context.Context, r Repo, name, oldID, newID string) error {
	body := []map[string]string{{"name": name, "oldObjectId": oldID, "newObjectId": newID}}
	var resp struct {
		Value []struct {
			Success       bool   `json:"success"`
			UpdateStatus  string `json:"updateStatus"`
			CustomMessage string `json:"customMessage"`
		} `json:"value"`
	}
	if err := c.do(ctx, http.MethodPost, r.path()+"/refs", v71(), body, &resp); err != nil {
		return err
	}
	if len(resp.Value) == 0 {
		return errors.New("no result from the ref update")
	}
	if v := resp.Value[0]; !v.Success {
		if v.CustomMessage != "" {
			return errors.New(v.CustomMessage)
		}
		return fmt.Errorf("ref update failed: %s", v.UpdateStatus)
	}
	return nil
}

func (c *Client) CommitURL(r Repo, id string) string { return r.WebURL + "/commit/" + id }

func (c *Client) TagURL(r Repo, name string) string {
	return r.WebURL + "?version=GT" + url.QueryEscape(name)
}
