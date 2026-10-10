package ado

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Team, security group and membership writes. A team is backed by a
// security group, so both take members the same way, through the graph.

// JSONPatch is a list of JSON patch operations; requests carrying one are
// sent as application/json-patch+json.
type JSONPatch []map[string]any

func (c *Client) teamsPath(projectID string) string {
	return "/_apis/projects/" + url.PathEscape(projectID) + "/teams"
}

func (c *Client) CreateTeam(ctx context.Context, projectID, name, description string) error {
	return c.do(ctx, http.MethodPost, c.teamsPath(projectID), v71(), map[string]string{"name": name, "description": description}, nil)
}

func (c *Client) UpdateTeam(ctx context.Context, projectID, teamID, name, description string) error {
	return c.do(ctx, http.MethodPatch, c.teamsPath(projectID)+"/"+url.PathEscape(teamID), v71(), map[string]string{"name": name, "description": description}, nil)
}

func (c *Client) DeleteTeam(ctx context.Context, projectID, teamID string) error {
	return c.do(ctx, http.MethodDelete, c.teamsPath(projectID)+"/"+url.PathEscape(teamID), v71(), nil, nil)
}

func graphVersion() url.Values { return url.Values{"api-version": {"7.1-preview.1"}} }

// Descriptor is the graph descriptor of a user, group or team by its ID:
// "aad.…" for a user, "vssgp.…" for a group or team.
func (c *Client) Descriptor(ctx context.Context, id string) (string, error) {
	var d struct {
		Value string `json:"value"`
	}
	err := c.vssps(ctx, http.MethodGet, "/_apis/graph/descriptors/"+url.PathEscape(id), graphVersion(), nil, &d)
	return d.Value, err
}

func (c *Client) CreateGroup(ctx context.Context, projectID, name, description string) error {
	scope, err := c.projectScope(ctx, projectID)
	if err != nil {
		return err
	}
	q := url.Values{"scopeDescriptor": {scope}, "api-version": {"7.1-preview.1"}}
	return c.vssps(ctx, http.MethodPost, "/_apis/graph/groups", q, map[string]string{"displayName": name, "description": description}, nil)
}

func (c *Client) UpdateGroup(ctx context.Context, descriptor, name, description string) error {
	patch := JSONPatch{
		{"op": "replace", "path": "/displayName", "value": name},
		{"op": "replace", "path": "/description", "value": description},
	}
	return c.vssps(ctx, http.MethodPatch, "/_apis/graph/groups/"+url.PathEscape(descriptor), graphVersion(), patch, nil)
}

func (c *Client) DeleteGroup(ctx context.Context, descriptor string) error {
	return c.vssps(ctx, http.MethodDelete, "/_apis/graph/groups/"+url.PathEscape(descriptor), graphVersion(), nil, nil)
}

// SetProjectPermission sets one project-level permission (bit) of a group
// (by security ID) to "Allow", "Deny" or "Not set", leaving the group's
// other permissions alone, as az devops security permission update and
// reset do. Changing one bit at a time matters: the access list can read
// back stale for a moment after a write, and replacing the whole entry from
// a stale read would undo changes just made.
func (c *Client) SetProjectPermission(ctx context.Context, projectID, sid string, bit int, state string) error {
	token := "$PROJECT:vstfs:///Classification/TeamProject/" + projectID
	descriptor := "Microsoft.TeamFoundation.Identity;" + sid
	if state == "Not set" {
		q := url.Values{"descriptor": {descriptor}, "token": {token}, "api-version": {apiVersion}}
		return c.do(ctx, http.MethodDelete, "/_apis/permissions/"+ProjectNamespace+"/"+strconv.Itoa(bit), q, nil, nil)
	}
	allow, deny := bit, 0
	if state == "Deny" {
		allow, deny = 0, bit
	}
	body := map[string]any{
		"token": token,
		"merge": true, // add to the group's entry; the bit leaves the other side
		"accessControlEntries": []any{map[string]any{
			"descriptor": descriptor, "allow": allow, "deny": deny,
		}},
	}
	return c.do(ctx, http.MethodPost, "/_apis/accesscontrolentries/"+ProjectNamespace, v71(), body, nil)
}

// AddMember puts a user or group (by graph descriptor) into a group or team.
func (c *Client) AddMember(ctx context.Context, member, container string) error {
	return c.vssps(ctx, http.MethodPut, "/_apis/graph/memberships/"+url.PathEscape(member)+"/"+url.PathEscape(container), graphVersion(), nil, nil)
}

func (c *Client) RemoveMember(ctx context.Context, member, container string) error {
	return c.vssps(ctx, http.MethodDelete, "/_apis/graph/memberships/"+url.PathEscape(member)+"/"+url.PathEscape(container), graphVersion(), nil, nil)
}
