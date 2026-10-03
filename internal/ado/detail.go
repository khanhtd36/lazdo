package ado

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Commit struct {
	ID      string `json:"commitId"`
	Comment string `json:"comment"`
	Author  struct {
		Name string    `json:"name"`
		Date time.Time `json:"date"`
	} `json:"author"`
}

// PRDetail is the full pull request with everything the detail view shows.
type PRDetail struct {
	PullRequest
	Description       string    `json:"description"`
	Status            string    `json:"status"`
	AutoCompleteSetBy *Identity `json:"autoCompleteSetBy"`
	LastMergeSource   Commit    `json:"lastMergeSourceCommit"`
	LastMergeCommit   *Commit   `json:"lastMergeCommit"`
	Labels            []Label   `json:"labels"`
	MergeFailure      string    `json:"mergeFailureMessage"`

	Policies  []Policy
	Threads   []Thread
	Commits   []Commit
	Pushes    []Push
	Changes   []Change
	Conflicts []Conflict
	WorkItems []WorkItem
}

type Label struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type Policy struct {
	Status        string `json:"status"`
	Configuration struct {
		IsBlocking bool `json:"isBlocking"`
		IsEnabled  bool `json:"isEnabled"`
		Type       struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"type"`
		Settings map[string]any `json:"settings"`
	} `json:"configuration"`
	Context map[string]any `json:"context"`
	// BuildName is the pipeline name for build policies.
	BuildName string `json:"-"`
}

type Push struct {
	ID              int       `json:"id"`
	Author          Identity  `json:"author"`
	CreatedDate     time.Time `json:"createdDate"`
	SourceRefCommit struct {
		CommitID string `json:"commitId"`
	} `json:"sourceRefCommit"`
}

type Change struct {
	ChangeType   string `json:"changeType"`
	OriginalPath string `json:"originalPath"`
	Item         struct {
		Path string `json:"path"`
	} `json:"item"`
}

type Conflict struct {
	Path string `json:"conflictPath"`
	Type string `json:"conflictType"`
}

type WorkItem struct {
	ID     int `json:"id"`
	Fields struct {
		Title string `json:"System.Title"`
		Type  string `json:"System.WorkItemType"`
		State string `json:"System.State"`
	} `json:"fields"`
}

type Comment struct {
	ID              int       `json:"id"`
	ParentCommentID int       `json:"parentCommentId"`
	Author          Identity  `json:"author"`
	Content         string    `json:"content"`
	CommentType     string    `json:"commentType"`
	IsDeleted       bool      `json:"isDeleted"`
	PublishedDate   time.Time `json:"publishedDate"`
	LastContentDate time.Time `json:"lastContentUpdatedDate"`
}

type Thread struct {
	ID            int                 `json:"id"`
	Status        string              `json:"status"`
	IsDeleted     bool                `json:"isDeleted"`
	PublishedDate time.Time           `json:"publishedDate"`
	Comments      []Comment           `json:"comments"`
	Identities    map[string]Identity `json:"identities"`
	Properties    map[string]struct {
		Value any `json:"$value"`
	} `json:"properties"`
	ThreadContext *struct {
		FilePath string `json:"filePath"`
	} `json:"threadContext"`
}

// Kind is the CodeReviewThreadType of a system thread, "" for a human one.
func (t Thread) Kind() string { return t.Prop("CodeReviewThreadType") }

// Prop returns a thread property as a string.
func (t Thread) Prop(key string) string {
	p, ok := t.Properties[key]
	if !ok || p.Value == nil {
		return ""
	}
	if s, ok := p.Value.(string); ok {
		return s
	}
	return fmt.Sprint(p.Value)
}

// PropIdentity resolves a property that holds a key into Identities.
func (t Thread) PropIdentity(key string) Identity {
	return t.Identities[t.Prop(key)]
}

func (t Thread) IsHuman() bool {
	return len(t.Comments) > 0 && t.Comments[0].CommentType != "system"
}

// IsResolved reports whether a human thread is in any closed state.
func (t Thread) IsResolved() bool {
	return t.Status != "active" && t.Status != "pending" && t.Status != ""
}

// LiveComments are the comments that are not deleted.
func (t Thread) LiveComments() []Comment {
	var out []Comment
	for _, c := range t.Comments {
		if !c.IsDeleted {
			out = append(out, c)
		}
	}
	return out
}

func (pr PullRequest) repoPath() string {
	return fmt.Sprintf("/%s/_apis/git/repositories/%s", url.PathEscape(pr.Repository.Project.ID), pr.Repository.ID)
}

func (pr PullRequest) prPath() string {
	return fmt.Sprintf("%s/pullRequests/%d", pr.repoPath(), pr.ID)
}

func v71() url.Values { return url.Values{"api-version": {apiVersion}} }

// Detail fetches everything the detail view shows, in parallel.
func (c *Client) Detail(ctx context.Context, pr PullRequest) (*PRDetail, error) {
	d := &PRDetail{}
	if err := c.get(ctx, pr.prPath(), v71(), d); err != nil {
		return nil, err
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []string
	)
	run := func(name string, f func() error) {
		wg.Go(func() {
			if err := f(); err != nil {
				mu.Lock()
				errs = append(errs, name+": "+err.Error())
				mu.Unlock()
			}
		})
	}
	pr = d.PullRequest
	run("policies", func() error {
		var err error
		d.Policies, err = c.policies(ctx, pr)
		return err
	})
	run("threads", func() error {
		var err error
		d.Threads, err = c.threads(ctx, pr)
		return err
	})
	run("commits", func() error {
		var err error
		d.Commits, err = c.commits(ctx, pr)
		return err
	})
	run("files", func() error {
		var err error
		d.Pushes, d.Changes, err = c.pushesAndChanges(ctx, pr)
		return err
	})
	run("work items", func() error {
		var err error
		d.WorkItems, err = c.workItems(ctx, pr)
		return err
	})
	if d.MergeStatus == "conflicts" {
		run("conflicts", func() error {
			var err error
			d.Conflicts, err = c.conflicts(ctx, pr)
			return err
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		return d, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return d, nil
}

func (c *Client) policies(ctx context.Context, pr PullRequest) ([]Policy, error) {
	q := url.Values{
		"artifactId":  {fmt.Sprintf("vstfs:///CodeReview/CodeReviewId/%s/%d", pr.Repository.Project.ID, pr.ID)},
		"api-version": {"7.1-preview.1"},
	}
	var resp struct {
		Value []Policy `json:"value"`
	}
	err := c.get(ctx, "/"+url.PathEscape(pr.Repository.Project.ID)+"/_apis/policy/evaluations", q, &resp)
	var out []Policy
	names := map[int]string{}
	for _, p := range resp.Value {
		if !p.Configuration.IsEnabled || p.Status == "notApplicable" {
			continue
		}
		if id, ok := p.Configuration.Settings["buildDefinitionId"].(float64); ok {
			p.BuildName = c.buildDefinitionName(ctx, pr, int(id), names)
		}
		out = append(out, p)
	}
	return out, err
}

// buildDefinitionName looks up a pipeline name; a queued build policy carries
// only the definition ID. Failures fall back to an empty name.
func (c *Client) buildDefinitionName(ctx context.Context, pr PullRequest, id int, cache map[int]string) string {
	if name, ok := cache[id]; ok {
		return name
	}
	var def struct {
		Name string `json:"name"`
	}
	path := fmt.Sprintf("/%s/_apis/build/definitions/%d", url.PathEscape(pr.Repository.Project.ID), id)
	_ = c.get(ctx, path, v71(), &def)
	cache[id] = def.Name
	return def.Name
}

func (c *Client) threads(ctx context.Context, pr PullRequest) ([]Thread, error) {
	var resp struct {
		Value []Thread `json:"value"`
	}
	err := c.get(ctx, pr.prPath()+"/threads", v71(), &resp)
	var out []Thread
	for _, t := range resp.Value {
		if !t.IsDeleted && len(t.LiveComments()) > 0 {
			out = append(out, t)
		}
	}
	return out, err
}

func (c *Client) commits(ctx context.Context, pr PullRequest) ([]Commit, error) {
	var resp struct {
		Value []Commit `json:"value"`
	}
	err := c.get(ctx, pr.prPath()+"/commits", url.Values{"api-version": {apiVersion}, "$top": {"500"}}, &resp)
	return resp.Value, err
}

func (c *Client) pushesAndChanges(ctx context.Context, pr PullRequest) ([]Push, []Change, error) {
	var its struct {
		Value []Push `json:"value"`
	}
	if err := c.get(ctx, pr.prPath()+"/iterations", v71(), &its); err != nil || len(its.Value) == 0 {
		return its.Value, nil, err
	}
	last := its.Value[len(its.Value)-1].ID
	var changes []Change
	for skip := 0; ; {
		q := url.Values{"api-version": {apiVersion}, "$compareTo": {"0"}, "$top": {"100"}, "$skip": {strconv.Itoa(skip)}}
		var resp struct {
			ChangeEntries []Change `json:"changeEntries"`
			NextSkip      int      `json:"nextSkip"`
		}
		if err := c.get(ctx, fmt.Sprintf("%s/iterations/%d/changes", pr.prPath(), last), q, &resp); err != nil {
			return its.Value, changes, err
		}
		changes = append(changes, resp.ChangeEntries...)
		if resp.NextSkip == 0 {
			break
		}
		skip = resp.NextSkip
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Item.Path < changes[j].Item.Path })
	return its.Value, changes, nil
}

func (c *Client) conflicts(ctx context.Context, pr PullRequest) ([]Conflict, error) {
	var resp struct {
		Value []Conflict `json:"value"`
	}
	err := c.get(ctx, pr.prPath()+"/conflicts", v71(), &resp)
	return resp.Value, err
}

func (c *Client) workItems(ctx context.Context, pr PullRequest) ([]WorkItem, error) {
	var refs struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if err := c.get(ctx, pr.prPath()+"/workitems", v71(), &refs); err != nil || len(refs.Value) == 0 {
		return nil, err
	}
	ids := make([]string, len(refs.Value))
	for i, r := range refs.Value {
		ids[i] = r.ID
	}
	q := url.Values{
		"ids":         {strings.Join(ids, ",")},
		"fields":      {"System.Title,System.WorkItemType,System.State"},
		"api-version": {apiVersion},
	}
	var resp struct {
		Value []WorkItem `json:"value"`
	}
	err := c.get(ctx, "/_apis/wit/workitems", q, &resp)
	return resp.Value, err
}

// RecordVisit marks the PR visited by Me now, the same as opening it in the
// browser, and returns the visit before this one (zero if none).
func (c *Client) RecordVisit(ctx context.Context, pr PullRequest) (time.Time, error) {
	body := map[string]string{"artifactId": pr.visitArtifactID()}
	var resp struct {
		PreviousLastVisitedDate *time.Time `json:"previousLastVisitedDate"`
	}
	err := c.do(ctx, "PUT", "/_apis/visits/artifactVisits", url.Values{"api-version": {"7.1-preview.1"}}, body, &resp)
	if err != nil || resp.PreviousLastVisitedDate == nil {
		return time.Time{}, err
	}
	return *resp.PreviousLastVisitedDate, nil
}

// visitArtifactID matches the casing the web UI uses when it records a visit.
func (pr PullRequest) visitArtifactID() string {
	return strings.ReplaceAll(pr.statsArtifactID(), "%2F", "%2f")
}
