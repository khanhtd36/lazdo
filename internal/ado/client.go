// Package ado talks to the Azure DevOps REST API using the token of the
// logged-in Azure CLI (`az login`), so no PAT is needed.
package ado

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// adoResourceID is the well-known Entra ID resource for Azure DevOps.
const adoResourceID = "499b84ac-1321-427f-aa17-267ca6975798"

type Client struct {
	Org  string // organization name, e.g. "arbinSW"
	http *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

func NewClient(org string) *Client {
	return &Client{Org: org, http: &http.Client{Timeout: 30 * time.Second}}
}

// DefaultOrg reads the organization configured by `az devops configure`.
func DefaultOrg() (string, error) {
	out, err := exec.Command("az", "devops", "configure", "--list").Output()
	if err != nil {
		return "", fmt.Errorf("az devops configure --list: %w", err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "organization" {
			return OrgName(strings.TrimSpace(v)), nil
		}
	}
	return "", errors.New("no default organization; pass --org or run `az devops configure -d organization=https://dev.azure.com/<org>`")
}

// OrgName accepts either a bare org name or an org URL.
func OrgName(s string) string {
	s = strings.TrimSuffix(strings.TrimSpace(s), "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.tokenExpiry) > 5*time.Minute {
		return c.token, nil
	}
	out, err := exec.CommandContext(ctx, "az", "account", "get-access-token",
		"--resource", adoResourceID, "-o", "json").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("az account get-access-token failed (run `az login`): %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("az account get-access-token: %w", err)
	}
	var tok struct {
		AccessToken string `json:"accessToken"`
		ExpiresOn   int64  `json:"expires_on"`
	}
	if err := json.Unmarshal(out, &tok); err != nil {
		return "", fmt.Errorf("parse az token: %w", err)
	}
	c.token = tok.AccessToken
	c.tokenExpiry = time.Unix(tok.ExpiresOn, 0)
	if tok.ExpiresOn == 0 {
		c.tokenExpiry = time.Now().Add(30 * time.Minute)
	}
	return c.token, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

func (c *Client) post(ctx context.Context, path string, query url.Values, in, out any) error {
	return c.do(ctx, http.MethodPost, path, query, in, out)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	return c.doAt(ctx, "dev.azure.com", method, path, query, in, out)
}

// vssps calls the identity host, where security groups and their members
// live; the rest of the API is on dev.azure.com.
func (c *Client) vssps(ctx context.Context, method, path string, query url.Values, in, out any) error {
	return c.doAt(ctx, "vssps.dev.azure.com", method, path, query, in, out)
}

func (c *Client) doAt(ctx context.Context, host, method, path string, query url.Values, in, out any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	u := "https://" + host + "/" + url.PathEscape(c.Org) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	switch in.(type) {
	case nil:
	case JSONPatch:
		req.Header.Set("Content-Type", "application/json-patch+json")
	default:
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		// The reason first: a status line cuts from the right.
		return fmt.Errorf("%s (%s, %s %s)", apiMessage(msg), resp.Status, method, path)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// apiMessage pulls the human-readable message out of an Azure DevOps error body.
func apiMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	text := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		text = e.Message
	}
	// One line: messages like "Invalid argument value.\nParameter name: …"
	// end up in a status line, where a line break would push the screen.
	return strings.Join(strings.Fields(text), " ")
}
