package ado

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Project settings: what the Settings tab shows, section by section.

// Overview is the project's own settings.
type Overview struct {
	Name, Description, Visibility string
	Process, VersionControl       string
	LastUpdate                    time.Time
	// Services maps Boards, Repos, Pipelines, Test Plans and Artifacts to
	// whether they are turned on for the project.
	Services map[string]bool
}

// ServiceOrder is how the web lists the project's services.
var ServiceOrder = []string{"Boards", "Repos", "Pipelines", "Test Plans", "Artifacts"}

var serviceFeatures = map[string]string{
	"ms.vss-work.agile":           "Boards",
	"ms.vss-code.version-control": "Repos",
	"ms.vss-build.pipelines":      "Pipelines",
	"ms.vss-test-web.test":        "Test Plans",
	"ms.feed.feed":                "Artifacts",
}

func (c *Client) Overview(ctx context.Context, projectID string) (Overview, error) {
	var p struct {
		Name         string    `json:"name"`
		Description  string    `json:"description"`
		Visibility   string    `json:"visibility"`
		LastUpdate   time.Time `json:"lastUpdateTime"`
		Capabilities struct {
			Process struct {
				Name string `json:"templateName"`
			} `json:"processTemplate"`
			VersionControl struct {
				Type string `json:"sourceControlType"`
			} `json:"versioncontrol"`
		} `json:"capabilities"`
	}
	q := url.Values{"includeCapabilities": {"true"}, "api-version": {apiVersion}}
	if err := c.get(ctx, "/_apis/projects/"+url.PathEscape(projectID), q, &p); err != nil {
		return Overview{}, err
	}
	o := Overview{
		Name: p.Name, Description: p.Description, Visibility: p.Visibility, LastUpdate: p.LastUpdate,
		Process: p.Capabilities.Process.Name, VersionControl: p.Capabilities.VersionControl.Type,
		Services: map[string]bool{},
	}
	ids := make([]string, 0, len(serviceFeatures))
	for id := range serviceFeatures {
		ids = append(ids, id)
	}
	body := map[string]any{"featureIds": ids, "featureDefaultStates": map[string]any{}, "scopeValues": map[string]string{"project": projectID}}
	var states struct {
		FeatureStates map[string]struct {
			State string `json:"state"`
		} `json:"featureStates"`
	}
	path := "/_apis/FeatureManagement/FeatureStatesQuery/host/project/" + url.PathEscape(projectID)
	if err := c.post(ctx, path, url.Values{"api-version": {"4.1-preview.1"}}, body, &states); err != nil {
		return o, nil // the services are a nicety; the rest still shows
	}
	for id, name := range serviceFeatures {
		o.Services[name] = states.FeatureStates[id].State != "disabled"
	}
	return o, nil
}

// Team is a project team.
type Team struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (c *Client) Teams(ctx context.Context, projectID string) ([]Team, error) {
	var resp struct {
		Value []Team `json:"value"`
	}
	err := c.get(ctx, "/_apis/projects/"+url.PathEscape(projectID)+"/teams", url.Values{"api-version": {apiVersion}, "$top": {"500"}}, &resp)
	sort.Slice(resp.Value, func(i, j int) bool { return strings.ToLower(resp.Value[i].Name) < strings.ToLower(resp.Value[j].Name) })
	return resp.Value, err
}

// TeamMembers lists a team's members by name.
func (c *Client) TeamMembers(ctx context.Context, projectID, teamID string) ([]Member, error) {
	var resp struct {
		Value []struct {
			Identity struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
				UniqueName  string `json:"uniqueName"`
			} `json:"identity"`
			IsTeamAdmin bool `json:"isTeamAdmin"`
		} `json:"value"`
	}
	path := "/_apis/projects/" + url.PathEscape(projectID) + "/teams/" + url.PathEscape(teamID) + "/members"
	if err := c.get(ctx, path, url.Values{"api-version": {apiVersion}, "$top": {"1000"}}, &resp); err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(resp.Value))
	for _, m := range resp.Value {
		out = append(out, Member{Name: m.Identity.DisplayName, Detail: m.Identity.UniqueName, Admin: m.IsTeamAdmin})
	}
	sortMembers(out)
	return out, nil
}

// Member is a user or group inside a team or security group.
type Member struct {
	Name, Detail string // Detail: an email, or "group"
	Admin        bool   // a team administrator
	IsGroup      bool
}

func sortMembers(ms []Member) {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].IsGroup != ms[j].IsGroup {
			return ms[i].IsGroup
		}
		return strings.ToLower(ms[i].Name) < strings.ToLower(ms[j].Name)
	})
}

// Group is a project security group.
type Group struct {
	Descriptor  string `json:"descriptor"` // vssgp.…
	Name        string `json:"displayName"`
	Description string `json:"description"`
}

// SID is the group's security identifier, which permissions refer to; the
// graph descriptor is "vssgp." and the SID in base64.
func (g Group) SID() string {
	enc := strings.TrimPrefix(g.Descriptor, "vssgp.")
	for _, e := range []*base64.Encoding{base64.RawURLEncoding, base64.RawStdEncoding, base64.StdEncoding, base64.URLEncoding} {
		if b, err := e.DecodeString(enc); err == nil {
			return string(b)
		}
	}
	return ""
}

func (c *Client) projectScope(ctx context.Context, projectID string) (string, error) {
	var d struct {
		Value string `json:"value"`
	}
	err := c.vssps(ctx, "GET", "/_apis/graph/descriptors/"+url.PathEscape(projectID), url.Values{"api-version": {"7.1-preview.1"}}, nil, &d)
	return d.Value, err
}

func (c *Client) Groups(ctx context.Context, projectID string) ([]Group, error) {
	scope, err := c.projectScope(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Value []Group `json:"value"`
	}
	err = c.vssps(ctx, "GET", "/_apis/graph/groups", url.Values{"scopeDescriptor": {scope}, "api-version": {"7.1-preview.1"}}, nil, &resp)
	sort.Slice(resp.Value, func(i, j int) bool { return strings.ToLower(resp.Value[i].Name) < strings.ToLower(resp.Value[j].Name) })
	return resp.Value, err
}

// GroupMembers lists a security group's direct members, users and groups.
func (c *Client) GroupMembers(ctx context.Context, group string) ([]Member, error) {
	var ms struct {
		Value []struct {
			Member string `json:"memberDescriptor"`
		} `json:"value"`
	}
	q := url.Values{"direction": {"down"}, "api-version": {"7.1-preview.1"}}
	if err := c.vssps(ctx, "GET", "/_apis/graph/Memberships/"+url.PathEscape(group), q, nil, &ms); err != nil || len(ms.Value) == 0 {
		return nil, err
	}
	keys := make([]map[string]string, 0, len(ms.Value))
	for _, m := range ms.Value {
		keys = append(keys, map[string]string{"descriptor": m.Member})
	}
	var subjects struct {
		Value map[string]struct {
			DisplayName   string `json:"displayName"`
			MailAddress   string `json:"mailAddress"`
			PrincipalName string `json:"principalName"`
			SubjectKind   string `json:"subjectKind"`
		} `json:"value"`
	}
	err := c.vssps(ctx, "POST", "/_apis/graph/subjectlookup", url.Values{"api-version": {"7.1-preview.1"}}, map[string]any{"lookupKeys": keys}, &subjects)
	out := make([]Member, 0, len(subjects.Value))
	for _, s := range subjects.Value {
		m := Member{Name: s.DisplayName, Detail: s.MailAddress}
		if s.SubjectKind == "group" {
			m.IsGroup, m.Detail = true, s.PrincipalName
		}
		out = append(out, m)
	}
	sortMembers(out)
	return out, err
}

// Permission is one project-level permission and how it stands for a group.
type Permission struct {
	Name  string
	Bit   int
	State string // "Allow", "Deny", "Not set"; Inherited when it comes from elsewhere
	// Inherited is set when the state isn't set on the group itself.
	Inherited bool
}

// ProjectNamespace is the security namespace of project-level permissions.
const ProjectNamespace = "52d39943-cb85-4d7f-8fa8-c6baac873819"

// ProjectPermissions lists the project-level permissions of each group,
// keyed by the group's SID.
func (c *Client) ProjectPermissions(ctx context.Context, projectID string) (map[string][]Permission, error) {
	var ns struct {
		Value []struct {
			Actions []struct {
				Bit         int    `json:"bit"`
				DisplayName string `json:"displayName"`
			} `json:"actions"`
		} `json:"value"`
	}
	if err := c.get(ctx, "/_apis/securitynamespaces/"+ProjectNamespace, url.Values{"api-version": {apiVersion}}, &ns); err != nil || len(ns.Value) == 0 {
		return nil, err
	}
	var acls struct {
		Value []struct {
			Aces map[string]struct {
				Allow    int `json:"allow"`
				Deny     int `json:"deny"`
				Extended struct {
					EffectiveAllow int `json:"effectiveAllow"`
					EffectiveDeny  int `json:"effectiveDeny"`
				} `json:"extendedInfo"`
			} `json:"acesDictionary"`
		} `json:"value"`
	}
	q := url.Values{"token": {"$PROJECT:vstfs:///Classification/TeamProject/" + projectID}, "includeExtendedInfo": {"true"}, "api-version": {apiVersion}}
	if err := c.get(ctx, "/_apis/accesscontrollists/"+ProjectNamespace, q, &acls); err != nil {
		return nil, err
	}
	out := map[string][]Permission{}
	for _, acl := range acls.Value {
		for desc, ace := range acl.Aces {
			_, sid, _ := strings.Cut(desc, ";")
			perms := make([]Permission, 0, len(ns.Value[0].Actions))
			for _, a := range ns.Value[0].Actions {
				p := Permission{Name: a.DisplayName, Bit: a.Bit, State: "Not set"}
				switch {
				case ace.Deny&a.Bit != 0:
					p.State = "Deny"
				case ace.Allow&a.Bit != 0:
					p.State = "Allow"
				case ace.Extended.EffectiveDeny&a.Bit != 0:
					p.State, p.Inherited = "Deny", true
				case ace.Extended.EffectiveAllow&a.Bit != 0:
					p.State, p.Inherited = "Allow", true
				}
				perms = append(perms, p)
			}
			out[sid] = perms
		}
	}
	return out, nil
}

// PolicyConfig is a branch policy as configured, not as evaluated on a PR.
type PolicyConfig struct {
	ID         int  `json:"id"`
	IsEnabled  bool `json:"isEnabled"`
	IsBlocking bool `json:"isBlocking"`
	Type       struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"type"`
	Settings map[string]any `json:"settings"`
}

// Scope is the branch (or prefix) and repo the policy covers; the repo is
// empty for all repos in the project.
func (p PolicyConfig) Scope() (branch, repoID string) {
	scopes, _ := p.Settings["scope"].([]any)
	if len(scopes) == 0 {
		return "", ""
	}
	s, _ := scopes[0].(map[string]any)
	ref, _ := s["refName"].(string)
	match, _ := s["matchKind"].(string)
	repoID, _ = s["repositoryId"].(string)
	branch = strings.TrimPrefix(ref, "refs/heads/")
	if strings.EqualFold(match, "prefix") {
		branch = strings.TrimSuffix(branch, "/") + "/*"
	}
	return branch, repoID
}

func (c *Client) PolicyConfigs(ctx context.Context, projectID string) ([]PolicyConfig, error) {
	var resp struct {
		Value []PolicyConfig `json:"value"`
	}
	err := c.get(ctx, "/"+url.PathEscape(projectID)+"/_apis/policy/configurations", url.Values{"api-version": {apiVersion}, "$top": {"1000"}}, &resp)
	return resp.Value, err
}

// IdentityNames looks up display names by identity ID (required reviewers).
func (c *Client) IdentityNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var resp struct {
		Value []struct {
			ID   string `json:"id"`
			Name string `json:"providerDisplayName"`
		} `json:"value"`
	}
	err := c.vssps(ctx, "GET", "/_apis/identities", url.Values{"identityIds": {strings.Join(ids, ",")}, "api-version": {"7.1"}}, nil, &resp)
	for _, v := range resp.Value {
		out[strings.ToLower(v.ID)] = v.Name
	}
	return out, err
}

// BuildDefinitionNames looks up pipeline names by definition ID.
func (c *Client) BuildDefinitionNames(ctx context.Context, projectID string, ids []int) (map[int]string, error) {
	out := map[int]string{}
	if len(ids) == 0 {
		return out, nil
	}
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	var resp struct {
		Value []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"value"`
	}
	err := c.get(ctx, "/"+url.PathEscape(projectID)+"/_apis/build/definitions", url.Values{"definitionIds": {strings.Join(s, ",")}, "api-version": {apiVersion}}, &resp)
	for _, v := range resp.Value {
		out[v.ID] = v.Name
	}
	return out, err
}

// AgentQueue is an agent pool as the project sees it.
type AgentQueue struct {
	Name   string
	Hosted bool
}

func (c *Client) AgentQueues(ctx context.Context, projectID string) ([]AgentQueue, error) {
	var resp struct {
		Value []struct {
			Name string `json:"name"`
			Pool struct {
				IsHosted bool `json:"isHosted"`
			} `json:"pool"`
		} `json:"value"`
	}
	err := c.get(ctx, "/"+url.PathEscape(projectID)+"/_apis/distributedtask/queues", url.Values{"api-version": {"7.1-preview.1"}}, &resp)
	out := make([]AgentQueue, 0, len(resp.Value))
	for _, q := range resp.Value {
		out = append(out, AgentQueue{Name: q.Name, Hosted: q.Pool.IsHosted})
	}
	return out, err
}

// ServiceConnection is a pipeline's connection to an outside service; its
// credentials are never read.
type ServiceConnection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	IsReady  bool   `json:"isReady"`
	IsShared bool   `json:"isShared"`
}

func (c *Client) ServiceConnections(ctx context.Context, projectID string) ([]ServiceConnection, error) {
	var resp struct {
		Value []ServiceConnection `json:"value"`
	}
	err := c.get(ctx, "/"+url.PathEscape(projectID)+"/_apis/serviceendpoint/endpoints", url.Values{"api-version": {apiVersion}}, &resp)
	return resp.Value, err
}

// VariableGroup is a set of pipeline variables; secret values never come back.
type VariableGroup struct {
	ID          int
	Name        string
	Description string
	Variables   []Variable
}

type Variable struct {
	Name, Value string
	Secret      bool
}

func (c *Client) VariableGroups(ctx context.Context, projectID string) ([]VariableGroup, error) {
	var resp struct {
		Value []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Variables   map[string]struct {
				Value    string `json:"value"`
				IsSecret bool   `json:"isSecret"`
			} `json:"variables"`
		} `json:"value"`
	}
	err := c.get(ctx, "/"+url.PathEscape(projectID)+"/_apis/distributedtask/variablegroups", url.Values{"api-version": {apiVersion}}, &resp)
	out := make([]VariableGroup, 0, len(resp.Value))
	for _, g := range resp.Value {
		vg := VariableGroup{ID: g.ID, Name: g.Name, Description: g.Description}
		for name, v := range g.Variables {
			vg.Variables = append(vg.Variables, Variable{Name: name, Value: v.Value, Secret: v.IsSecret})
		}
		sort.Slice(vg.Variables, func(i, j int) bool { return vg.Variables[i].Name < vg.Variables[j].Name })
		out = append(out, vg)
	}
	return out, err
}

// ProjectSettings is everything the Settings tab shows. Each section loads
// on its own; Errs says which failed, so one section a user may not read
// doesn't blank the others.
type ProjectSettings struct {
	Overview    Overview
	Teams       []Team
	Groups      []Group
	Permissions map[string][]Permission // by group SID
	Repos       []Repo
	Policies    []PolicyConfig
	Names       map[string]string // identity ID → name, for required reviewers
	Pipelines   map[int]string    // build definition ID → name, for build policies
	// AllPipelines are the project's pipelines, to pick from when editing
	// a build policy.
	AllPipelines []Pipeline
	Queues       []AgentQueue
	Connections  []ServiceConnection
	VarGroups    []VariableGroup
	Errs         map[string]error // by section
}

func (c *Client) ProjectSettings(ctx context.Context, p ProjectInfo, repos []Repo) *ProjectSettings {
	s := &ProjectSettings{Errs: map[string]error{}}
	for _, r := range repos {
		if r.Project.ID == p.ID {
			s.Repos = append(s.Repos, r)
		}
	}
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	run := func(section string, f func() error) {
		wg.Go(func() {
			if err := f(); err != nil {
				mu.Lock()
				s.Errs[section] = err
				mu.Unlock()
			}
		})
	}
	run("overview", func() (err error) { s.Overview, err = c.Overview(ctx, p.ID); return err })
	run("teams", func() (err error) { s.Teams, err = c.Teams(ctx, p.ID); return err })
	run("groups", func() (err error) { s.Groups, err = c.Groups(ctx, p.ID); return err })
	run("permissions", func() (err error) { s.Permissions, err = c.ProjectPermissions(ctx, p.ID); return err })
	run("policies", func() (err error) { return c.loadPolicies(ctx, p.ID, s) })
	run("queues", func() (err error) { s.Queues, err = c.AgentQueues(ctx, p.ID); return err })
	run("pipelines", func() (err error) { s.AllPipelines, err = c.Pipelines(ctx, p.ID); return err })
	run("connections", func() (err error) { s.Connections, err = c.ServiceConnections(ctx, p.ID); return err })
	run("variable groups", func() (err error) { s.VarGroups, err = c.VariableGroups(ctx, p.ID); return err })
	wg.Wait()
	return s
}

// loadPolicies reads the policies and the names they refer to by ID.
func (c *Client) loadPolicies(ctx context.Context, projectID string, s *ProjectSettings) error {
	ps, err := c.PolicyConfigs(ctx, projectID)
	if err != nil {
		return err
	}
	s.Policies = ps
	var reviewers []string
	var builds []int
	for _, p := range ps {
		if ids, ok := p.Settings["requiredReviewerIds"].([]any); ok {
			for _, id := range ids {
				if s, ok := id.(string); ok {
					reviewers = append(reviewers, s)
				}
			}
		}
		if id, ok := p.Settings["buildDefinitionId"].(float64); ok {
			builds = append(builds, int(id))
		}
	}
	names, errN := c.IdentityNames(ctx, reviewers)
	pipelines, errB := c.BuildDefinitionNames(ctx, projectID, builds)
	s.Names, s.Pipelines = names, pipelines
	return errors.Join(errN, errB)
}
