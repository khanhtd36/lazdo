// Package prefs keeps what lazdo remembers between runs: the orgs used
// and the account used with each, most recent first.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Pair is an org and the account used with it: the account's folder name
// ("" for az's own login), and the org's tenant when known.
type Pair struct {
	Org     string `json:"org"`
	Account string `json:"account,omitempty"`
	Tenant  string `json:"tenant,omitempty"`
}

// Prefs is the saved file.
type Prefs struct {
	Recent []Pair `json:"recent"` // most recent first; Recent[0] is the last one used
}

const maxRecent = 20

// file is where the prefs live; tests point it elsewhere.
var file = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lazdo", "config.json"), nil
}

// Load reads the prefs; a missing or broken file is empty prefs.
func Load() Prefs {
	var p Prefs
	if f, err := file(); err == nil {
		if b, err := os.ReadFile(f); err == nil {
			_ = json.Unmarshal(b, &p)
		}
	}
	return p
}

// Use puts pair first among the recent ones, replacing its older entry.
func (p Prefs) Use(pair Pair) Prefs {
	recent := []Pair{pair}
	for _, r := range p.Recent {
		if (r.Org != pair.Org || r.Account != pair.Account) && len(recent) < maxRecent {
			recent = append(recent, r)
		}
	}
	p.Recent = recent
	return p
}

// Forget drops the pairs of an account.
func (p Prefs) Forget(account string) Prefs {
	var keep []Pair
	for _, r := range p.Recent {
		if r.Account != account {
			keep = append(keep, r)
		}
	}
	p.Recent = keep
	return p
}

// For is the most recent pair of org, if it was used.
func (p Prefs) For(org string) (Pair, bool) {
	for _, r := range p.Recent {
		if r.Org == org {
			return r, true
		}
	}
	return Pair{}, false
}

// Save writes the prefs.
func (p Prefs) Save() error {
	f, err := file()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, b, 0o644)
}

// UseFile points the prefs at path, for tests; the returned func undoes it.
func UseFile(path string) (restore func()) {
	saved := file
	file = func() (string, error) { return path, nil }
	return func() { file = saved }
}
