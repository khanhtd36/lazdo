package ado

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Branch policy writes. A configuration is sent whole: its type, enabled
// and blocking flags, and settings (scope included).

func (c *Client) policyPath(projectID string) string {
	return "/" + url.PathEscape(projectID) + "/_apis/policy/configurations"
}

func (p PolicyConfig) body() map[string]any {
	return map[string]any{
		"isEnabled":  p.IsEnabled,
		"isBlocking": p.IsBlocking,
		"type":       map[string]string{"id": p.Type.ID},
		"settings":   p.Settings,
	}
}

func (c *Client) CreatePolicy(ctx context.Context, projectID string, p PolicyConfig) error {
	return c.do(ctx, http.MethodPost, c.policyPath(projectID), v71(), p.body(), nil)
}

func (c *Client) UpdatePolicy(ctx context.Context, projectID string, p PolicyConfig) error {
	return c.do(ctx, http.MethodPut, c.policyPath(projectID)+"/"+strconv.Itoa(p.ID), v71(), p.body(), nil)
}

func (c *Client) DeletePolicy(ctx context.Context, projectID string, id int) error {
	return c.do(ctx, http.MethodDelete, c.policyPath(projectID)+"/"+strconv.Itoa(id), v71(), nil, nil)
}

// Person is a user or group found by name or email.
type Person struct {
	ID, Name, Mail string
	IsGroup        bool
}

// SearchPeople finds users and groups by name or email, as the web's
// people picker does.
func (c *Client) SearchPeople(ctx context.Context, query string) ([]Person, error) {
	body := map[string]any{
		"query":           query,
		"identityTypes":   []string{"user", "group"},
		"operationScopes": []string{"ims", "source"},
		"options":         map[string]int{"MinResults": 5, "MaxResults": 20},
		"properties":      []string{"DisplayName", "Mail"},
	}
	var resp struct {
		Results []struct {
			Identities []struct {
				LocalID     string `json:"localId"`
				DisplayName string `json:"displayName"`
				Mail        string `json:"mail"`
				EntityType  string `json:"entityType"`
			} `json:"identities"`
		} `json:"results"`
	}
	if err := c.post(ctx, "/_apis/IdentityPicker/Identities", url.Values{"api-version": {"7.1-preview.1"}}, body, &resp); err != nil {
		return nil, err
	}
	var out []Person
	for _, r := range resp.Results {
		for _, id := range r.Identities {
			if id.LocalID != "" {
				out = append(out, Person{ID: id.LocalID, Name: id.DisplayName, Mail: id.Mail, IsGroup: id.EntityType == "Group"})
			}
		}
	}
	return out, nil
}
