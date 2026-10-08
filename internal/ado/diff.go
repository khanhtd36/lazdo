package ado

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// LineBlock is one run of lines in a file diff, as Azure DevOps aligns them.
// Line numbers are 1-based; a count of 0 means the side has no lines here.
type LineBlock struct {
	ChangeType    string `json:"changeType"` // none, add, delete, edit
	OriginalStart int    `json:"originalLineNumberStart"`
	OriginalCount int    `json:"originalLinesCount"`
	ModifiedStart int    `json:"modifiedLineNumberStart"`
	ModifiedCount int    `json:"modifiedLinesCount"`
}

// FileDiff returns the line alignment of one file between two commits.
func (c *Client) FileDiff(ctx context.Context, pr PullRequest, baseCommit, targetCommit, path, originalPath string) ([]LineBlock, error) {
	body := map[string]any{
		"baseVersionCommit":   baseCommit,
		"targetVersionCommit": targetCommit,
		"fileDiffParams":      []map[string]string{{"path": path, "originalPath": originalPath}},
	}
	var resp struct {
		Value []struct {
			LineDiffBlocks []LineBlock `json:"lineDiffBlocks"`
		} `json:"value"`
	}
	if err := c.post(ctx, pr.repoPath()+"/FileDiffs", v71(), body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Value) == 0 {
		return nil, nil
	}
	return resp.Value[0].LineDiffBlocks, nil
}

// maxBlobBytes bounds what the diff view downloads for one file version.
const maxBlobBytes = 8 << 20

// Blob downloads one file version by its git object ID.
// IsMissingObject reports Azure DevOps' "the object does not exist"
// (TF401035): asked for an ID that isn't in the repo, such as a
// submodule's commit, which lives in the submodule's own repo.
func IsMissingObject(err error) bool {
	return err != nil && strings.Contains(err.Error(), "TF401035")
}

func (c *Client) Blob(ctx context.Context, pr PullRequest, objectID string) ([]byte, error) {
	q := url.Values{"api-version": {apiVersion}, "$format": {"octetstream"}}
	return c.getRaw(ctx, pr.repoPath()+"/blobs/"+objectID, q, "application/octet-stream")
}

// CommitParent returns the first parent of a commit.
func (c *Client) CommitParent(ctx context.Context, pr PullRequest, commitID string) (string, error) {
	var resp struct {
		Parents []string `json:"parents"`
	}
	if err := c.get(ctx, pr.repoPath()+"/commits/"+commitID, v71(), &resp); err != nil {
		return "", err
	}
	if len(resp.Parents) == 0 {
		return "", fmt.Errorf("commit %s has no parent", commitID)
	}
	return resp.Parents[0], nil
}

// ParentCounts returns how many parents each commit has, 2 or more for a
// merge. Commit lists leave parents out, so it asks for each commit, a few
// at a time; what it got before an error is still returned.
func (c *Client) ParentCounts(ctx context.Context, pr PullRequest, ids []string) (map[string]int, error) {
	out := make(map[string]int, len(ids))
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	slots := make(chan struct{}, 8)
	for _, id := range ids {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			var resp struct {
				Parents []string `json:"parents"`
			}
			err := c.get(ctx, pr.repoPath()+"/commits/"+id, v71(), &resp)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				firstErr = cmp.Or(firstErr, err)
				return
			}
			out[id] = len(resp.Parents)
		})
	}
	wg.Wait()
	return out, firstErr
}

// CommitChanges lists the files one commit changed against its parent.
func (c *Client) CommitChanges(ctx context.Context, pr PullRequest, commitID string) ([]Change, error) {
	var out []Change
	for skip := 0; ; skip += 100 {
		q := url.Values{"api-version": {apiVersion}, "top": {"100"}, "skip": {fmt.Sprint(skip)}}
		var resp struct {
			Changes []Change `json:"changes"`
		}
		if err := c.get(ctx, pr.repoPath()+"/commits/"+commitID+"/changes", q, &resp); err != nil {
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

// Side is which file of a diff a line comment is on.
type Side int

const (
	SideRight Side = iota // the new file
	SideLeft              // the original file
)

// LineComment anchors a new thread to lines of one file in a comparison.
type LineComment struct {
	Path             string
	Side             Side
	StartLine        int
	EndLine          int
	EndLineLength    int // characters on EndLine, so the comment spans it whole
	ChangeTrackingID int
	FirstIteration   int
	SecondIteration  int
}

func (c *Client) NewLineThread(ctx context.Context, pr PullRequest, lc LineComment, content string) error {
	start := map[string]int{"line": lc.StartLine, "offset": 1}
	end := map[string]int{"line": lc.EndLine, "offset": lc.EndLineLength + 1}
	tc := map[string]any{"filePath": lc.Path}
	if lc.Side == SideLeft {
		tc["leftFileStart"], tc["leftFileEnd"] = start, end
	} else {
		tc["rightFileStart"], tc["rightFileEnd"] = start, end
	}
	body := map[string]any{
		"comments":      []map[string]any{{"parentCommentId": 0, "content": content, "commentType": "text"}},
		"status":        "active",
		"threadContext": tc,
		"pullRequestThreadContext": map[string]any{
			"changeTrackingId": lc.ChangeTrackingID,
			"iterationContext": map[string]int{
				"firstComparingIteration":  lc.FirstIteration,
				"secondComparingIteration": lc.SecondIteration,
			},
		},
	}
	return c.do(ctx, http.MethodPost, pr.prPath()+"/threads", v71(), body, nil)
}

// IsBinary guesses binary content the way git does: a NUL in the first 8 KB.
func IsBinary(b []byte) bool {
	return strings.IndexByte(string(b[:min(len(b), 8000)]), 0) >= 0
}
