package ado

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// An Account is one az login: az's own (Dir ""), or one lazdo keeps in a
// folder of its own, so two people's logins never share az's default.
type Account struct {
	Dir   string // AZURE_CONFIG_DIR; "" is az's own
	Email string // who is logged in; "" when nobody is
}

// Name is the account as remembered: its folder's name, "" for az's own.
func (a Account) Name() string {
	if a.Dir == "" {
		return ""
	}
	return filepath.Base(a.Dir)
}

// AccountsDir holds the az logins lazdo added, one folder each, named
// after the email that signed in.
func AccountsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lazdo", "az"), nil
}

// AccountDir is the folder of a remembered account name; "" stays az's own.
func AccountDir(name string) string {
	root, err := AccountsDir()
	if name == "" || err != nil {
		return ""
	}
	return filepath.Join(root, name)
}

// ExtraAccounts are the folders of the accounts lazdo added.
func ExtraAccounts() []string {
	root, err := AccountsDir()
	if err != nil {
		return nil
	}
	entries, _ := os.ReadDir(root)
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), "new-") {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs
}

// Accounts are az's own login, then the ones lazdo added, each with who is
// logged in.
func Accounts(ctx context.Context) []Account {
	extra := ExtraAccounts()
	accts := make([]Account, 1, 1+len(extra))
	for _, d := range extra {
		accts = append(accts, Account{Dir: d})
	}
	var wg sync.WaitGroup
	for i := range accts {
		wg.Go(func() { accts[i].Email = accountEmail(ctx, accts[i].Dir) })
	}
	wg.Wait()
	return accts
}

func accountEmail(ctx context.Context, dir string) string {
	out, err := az(ctx, dir, "account", "show", "--query", "user.name", "-o", "tsv").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// az runs the Azure CLI with the login in dir ("" for az's own).
func az(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "az", args...)
	if dir != "" {
		cmd.Env = append(os.Environ(), "AZURE_CONFIG_DIR="+dir)
	}
	return cmd
}

// LoginCommand is what to run in a shell to log the account in to tenant.
func LoginCommand(dir, tenant string) string {
	cmd := "az login --allow-no-subscriptions"
	if tenant != "" {
		cmd = "az login --tenant " + tenant + " --allow-no-subscriptions"
	}
	if dir != "" {
		cmd = "AZURE_CONFIG_DIR=" + dir + " " + cmd
	}
	return cmd
}

// azToken is an Azure DevOps token from the login in dir, for tenant when
// set (an org in another tenant takes only that tenant's tokens).
func azToken(ctx context.Context, dir, tenant string) (string, time.Time, error) {
	args := []string{"account", "get-access-token", "--resource", adoResourceID, "-o", "json"}
	if tenant != "" {
		args = append(args, "--tenant", tenant)
	}
	out, err := az(ctx, dir, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", time.Time{}, fmt.Errorf("az account get-access-token failed (run `%s`): %s", LoginCommand(dir, tenant), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", time.Time{}, fmt.Errorf("az account get-access-token: %w", err)
	}
	var tok struct {
		AccessToken string `json:"accessToken"`
		ExpiresOn   int64  `json:"expires_on"`
	}
	if err := json.Unmarshal(out, &tok); err != nil {
		return "", time.Time{}, fmt.Errorf("parse az token: %w", err)
	}
	expiry := time.Unix(tok.ExpiresOn, 0)
	if tok.ExpiresOn == 0 {
		expiry = time.Now().Add(30 * time.Minute)
	}
	return tok.AccessToken, expiry, nil
}

// noTenant is what an org backed by Microsoft accounts, not Entra, reports.
const noTenant = "00000000-0000-0000-0000-000000000000"

// OrgTenant is the Entra tenant an org belongs to, "" when unknown: asked
// without a token, Azure DevOps names it in its 401 answer.
func OrgTenant(ctx context.Context, org string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://dev.azure.com/"+org+"/_apis/connectionData", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	if t := resp.Header.Get("X-VSS-ResourceTenant"); t != noTenant {
		return t
	}
	return ""
}

// OrgRef is an org an account belongs to, and the tenant that listed it,
// whose tokens the org takes.
type OrgRef struct{ Name, Tenant string }

// AccountOrgs lists the orgs an account belongs to, across the tenants it
// is logged in to.
func AccountOrgs(ctx context.Context, dir string) ([]OrgRef, error) {
	out, err := az(ctx, dir, "account", "list", "--query", "[].tenantId", "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("not logged in (run `%s`)", LoginCommand(dir, ""))
	}
	var tenants []string
	if err := json.Unmarshal(out, &tenants); err != nil {
		return nil, err
	}
	if len(tenants) == 0 {
		return nil, fmt.Errorf("not logged in (run `%s`)", LoginCommand(dir, ""))
	}
	sort.Strings(tenants)
	tenants = compactStrings(tenants)
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		seen = map[string]bool{}
		orgs []OrgRef
		errs []error
	)
	for _, t := range tenants {
		wg.Go(func() {
			names, err := tenantOrgs(ctx, dir, t)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			}
			for _, o := range names {
				if !seen[o] {
					seen[o] = true
					orgs = append(orgs, OrgRef{o, t})
				}
			}
		})
	}
	wg.Wait()
	if len(orgs) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	sort.Slice(orgs, func(i, j int) bool { return strings.ToLower(orgs[i].Name) < strings.ToLower(orgs[j].Name) })
	return orgs, nil
}

func compactStrings(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// tenantOrgs lists the orgs of the tenant the account is a member of.
func tenantOrgs(ctx context.Context, dir, tenant string) ([]string, error) {
	token, _, err := azToken(ctx, dir, tenant)
	if err != nil {
		return nil, err
	}
	get := func(url string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("list organizations: %s", resp.Status)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var me struct {
		ID string `json:"publicAlias"`
	}
	if err := get("https://app.vssps.visualstudio.com/_apis/profile/profiles/me?api-version="+apiVersion, &me); err != nil {
		return nil, err
	}
	var accounts struct {
		Value []struct {
			Name string `json:"accountName"`
		} `json:"value"`
	}
	if err := get("https://app.vssps.visualstudio.com/_apis/accounts?memberId="+me.ID+"&api-version="+apiVersion, &accounts); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(accounts.Value))
	for _, a := range accounts.Value {
		names = append(names, a.Name)
	}
	return names, nil
}

// NewAccountLogin is az login into a fresh folder; FinishAccount names the
// folder after who signed in.
func NewAccountLogin() (*exec.Cmd, string, error) {
	root, err := AccountsDir()
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(root, fmt.Sprintf("new-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	cmd := exec.Command("az", "login", "--allow-no-subscriptions")
	cmd.Env = append(os.Environ(), "AZURE_CONFIG_DIR="+dir)
	return cmd, dir, nil
}

// FinishAccount keeps a login made by NewAccountLogin under the email that
// signed in; existed is true when that account was already there, and the
// new login is dropped.
func FinishAccount(ctx context.Context, dir string) (acct Account, existed bool, err error) {
	email := accountEmail(ctx, dir)
	if email == "" {
		_ = os.RemoveAll(dir)
		return Account{}, false, errors.New("nobody signed in")
	}
	target := filepath.Join(filepath.Dir(dir), safeName(email))
	if _, err := os.Stat(target); err == nil {
		_ = os.RemoveAll(dir)
		return Account{Dir: target, Email: email}, true, nil
	}
	if err := os.Rename(dir, target); err != nil {
		_ = os.RemoveAll(dir)
		return Account{}, false, err
	}
	return Account{Dir: target, Email: email}, false, nil
}

// safeName keeps an email usable as a folder name everywhere.
func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < ' ' {
			return '_'
		}
		return r
	}, strings.ToLower(s))
}

// RemoveAccount deletes a login lazdo added, logging that account out; az's
// own login is never touched.
func RemoveAccount(dir string) error {
	root, err := AccountsDir()
	if err != nil {
		return err
	}
	if dir == "" || filepath.Dir(dir) != root {
		return errors.New("only accounts lazdo added can be removed")
	}
	return os.RemoveAll(dir)
}
