package actions

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// PlanKind is what checking out a branch at a path will do.
type PlanKind int

const (
	PlanSwitch PlanKind = iota // the path is a clone of the repo: fetch and switch
	PlanClone                  // the path is missing or an empty folder: clone into it
	PlanRefuse                 // anything else
)

type Plan struct {
	Kind   PlanKind
	Path   string // absolute
	Reason string // why it is refused
}

// ExpandPath resolves ~ and makes the path absolute.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// PlanCheckout decides what checking out into path means for the repo
// identified by wantKey (see RepoKey).
func PlanCheckout(path, wantKey string) Plan {
	path = ExpandPath(path)
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return Plan{Kind: PlanClone, Path: path}
	case err != nil:
		return Plan{Kind: PlanRefuse, Path: path, Reason: err.Error()}
	case !info.IsDir():
		return Plan{Kind: PlanRefuse, Path: path, Reason: "a file, not a folder"}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Plan{Kind: PlanRefuse, Path: path, Reason: err.Error()}
	}
	if len(entries) == 0 {
		return Plan{Kind: PlanClone, Path: path}
	}
	key, ok := RepoKeyAt(path)
	switch {
	case !ok:
		return Plan{Kind: PlanRefuse, Path: path, Reason: "folder is not empty and is not a clone of this repo"}
	case key != wantKey:
		return Plan{Kind: PlanRefuse, Path: path, Reason: "folder is a clone of another repo (" + key + ")"}
	}
	return Plan{Kind: PlanSwitch, Path: path}
}

// RepoKeyAt returns the RepoKey of the origin remote of the clone at path.
func RepoKeyAt(path string) (string, bool) {
	out, err := exec.Command("git", "-C", path, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", false
	}
	return RemoteRepoKey(string(out))
}

// CheckoutIn fetches the branch from origin and switches to it in the clone
// at path. It never forces: uncommitted changes that block the switch fail.
func CheckoutIn(path, branch string) error {
	if err := git("-C", path, "fetch", "origin", branch); err != nil {
		return err
	}
	return git("-C", path, "switch", branch)
}

// CheckoutDetached puts the clone at path on target without a branch
// (detached HEAD): a tag when fetchBranch is empty, else a commit that
// fetching fetchBranch brings in.
func CheckoutDetached(path, fetchBranch, target string) error {
	fetch := []string{"-C", path, "fetch", "origin", "tag", target}
	if fetchBranch != "" {
		fetch = []string{"-C", path, "fetch", "origin", fetchBranch}
	}
	if err := git(fetch...); err != nil {
		return err
	}
	return git("-C", path, "switch", "--detach", target)
}

// CloneDetached clones the repo into path and detaches at target: a tag
// (cloned directly) or a commit on fetchBranch.
func CloneDetached(cloneURL, path, fetchBranch, target string) error {
	if fetchBranch == "" {
		return git("clone", "--branch", target, cloneURL, path)
	}
	if err := git("clone", "--branch", fetchBranch, cloneURL, path); err != nil {
		return err
	}
	return git("-C", path, "switch", "--detach", target)
}

// Clone clones the repo into path with the branch checked out.
func Clone(cloneURL, path, branch string) error {
	return git("clone", "--branch", branch, cloneURL, path)
}

// CloneURL is the https clone URL of an Azure DevOps repo.
func CloneURL(org, project, repo string) string {
	return fmt.Sprintf("https://%s@dev.azure.com/%s/%s/_git/%s",
		url.PathEscape(org), url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo))
}

// CompleteDir completes the last path element to the longest prefix shared
// by the matching folders, like a shell's tab completion.
func CompleteDir(input string) string {
	expanded := ExpandPath(input)
	dir, prefix := filepath.Split(expanded)
	if strings.HasSuffix(input, "/") || strings.HasSuffix(input, `\`) {
		dir, prefix = expanded, ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return input
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(prefix)) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return input
	}
	sort.Strings(names)
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(strings.ToLower(n), strings.ToLower(common)) {
			common = common[:len(common)-1]
		}
	}
	out := filepath.Join(dir, common)
	if len(names) == 1 {
		out += string(filepath.Separator)
	}
	return out
}
