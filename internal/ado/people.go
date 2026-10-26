package ado

import (
	"context"
	"net/url"
	"strings"
)

// OrgUsers lists the organization's users, with the identity IDs mentions
// need, from its user entitlements. Listing needs a permission not
// everyone has; SearchPeople is the fallback.
func (c *Client) OrgUsers(ctx context.Context) ([]Person, error) {
	var out []Person
	token := ""
	for range 50 { // 100 a page; a stop for a runaway token
		q := url.Values{"api-version": {"7.1-preview.3"}, "$top": {"100"}}
		if token != "" {
			q.Set("continuationToken", token)
		}
		var resp struct {
			Members []struct {
				ID   string `json:"id"`
				User struct {
					DisplayName string `json:"displayName"`
					MailAddress string `json:"mailAddress"`
				} `json:"user"`
			} `json:"members"`
			ContinuationToken string `json:"continuationToken"`
		}
		if err := c.doAt(ctx, "vsaex.dev.azure.com", "GET", "/_apis/userentitlements", q, nil, &resp); err != nil {
			return nil, err
		}
		for _, m := range resp.Members {
			if m.ID != "" {
				out = append(out, Person{ID: m.ID, Name: m.User.DisplayName, Mail: m.User.MailAddress})
			}
		}
		if resp.ContinuationToken == "" || len(resp.Members) == 0 || strings.EqualFold(resp.ContinuationToken, token) {
			return out, nil
		}
		token = resp.ContinuationToken
	}
	return out, nil
}
