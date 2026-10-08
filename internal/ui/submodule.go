package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// submoduleChange is a submodule's pointer moving. Azure DevOps lists it
// like a file, but its two "file" IDs are commits in another repo, so
// instead of a diff lazdo shows the move and the commits it brings in.
type submoduleChange struct {
	from, to string    // commit IDs; from is empty when added, to when removed
	repo     *ado.Repo // the submodule's repo, when lazdo found it
	commits  []ado.RepoCommit
	back     bool   // the pointer moved to an older commit
	note     string // why the commits aren't listed, if they aren't
}

func loadSubmodule(ctx context.Context, client *ado.Client, pr ado.PullRequest, ch ado.Change, base, target string) *submoduleChange {
	s := &submoduleChange{from: strings.ToLower(ch.Item.OriginalObjectID), to: strings.ToLower(ch.Item.ObjectID)}
	switch {
	case strings.Contains(ch.ChangeType, "add"):
		s.from = ""
	case strings.Contains(ch.ChangeType, "delete"):
		s.to = ""
	}
	path := strings.TrimPrefix(ch.Item.Path, "/")
	var url string
	for _, commit := range []string{target, base} { // a removed submodule is only in the base's .gitmodules
		if b, err := client.FileAt(ctx, pr.AsRepo(), commit, "/.gitmodules"); err == nil {
			if url = parseGitmodules(string(b))[path]; url != "" {
				break
			}
		}
	}
	if url == "" {
		s.note = "no .gitmodules entry for " + path
		return s
	}
	project, name := submoduleRepo(url)
	repos, err := client.Repos(ctx)
	if err != nil {
		s.note = "couldn't list repos: " + err.Error()
		return s
	}
	s.repo = findRepo(repos, project, name, pr.Repository.Project.Name)
	if s.repo == nil {
		s.note = "its repo (" + url + ") isn't in this organization"
		return s
	}
	if s.from == "" || s.to == "" {
		return s
	}
	if s.commits, err = client.CommitRange(ctx, *s.repo, s.from, s.to); err == nil && len(s.commits) == 0 {
		s.commits, err = client.CommitRange(ctx, *s.repo, s.to, s.from) // moved back?
		s.back = len(s.commits) > 0
	}
	if err != nil {
		s.note = "couldn't list its commits: " + err.Error()
	}
	return s
}

// parseGitmodules maps each submodule's path to its URL.
func parseGitmodules(text string) map[string]string {
	out := map[string]string{}
	var path, url string
	flush := func() {
		if path != "" && url != "" {
			out[path] = url
		}
		path, url = "", ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			flush()
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "path":
			path = strings.Trim(strings.TrimSpace(value), "/")
		case "url":
			url = strings.TrimSpace(value)
		}
	}
	flush()
	return out
}

// submoduleRepo reads the project (when the URL says) and repo name from a
// submodule URL: https://dev.azure.com/org/project/_git/repo,
// git@ssh.dev.azure.com:v3/org/project/repo, or a relative ../repo.
func submoduleRepo(url string) (project, name string) {
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	if i := strings.Index(url, "/_git/"); i >= 0 {
		before := strings.Split(url[:i], "/")
		return before[len(before)-1], url[i+len("/_git/"):]
	}
	if _, rest, ok := strings.Cut(url, ":v3/"); ok {
		if parts := strings.Split(rest, "/"); len(parts) == 3 {
			return parts[1], parts[2]
		}
	}
	parts := strings.Split(url, "/")
	return "", parts[len(parts)-1]
}

// findRepo picks the repo a submodule names: in its project when the URL
// says, else preferring the pull request's own project.
func findRepo(repos []ado.Repo, project, name, prProject string) *ado.Repo {
	var fallback *ado.Repo
	for i := range repos {
		r := &repos[i]
		if !strings.EqualFold(r.Name, name) {
			continue
		}
		switch {
		case project != "" && strings.EqualFold(r.Project.Name, project):
			return r
		case project == "" && strings.EqualFold(r.Project.Name, prProject):
			return r
		case fallback == nil && project == "":
			fallback = r
		}
	}
	return fallback
}

// submoduleLines renders a submodule's move for the diff pane.
func submoduleLines(s *submoduleChange, path string, height, width int) []string {
	name := path[strings.LastIndex(path, "/")+1:]
	if s.repo != nil {
		name = s.repo.Project.Name + "/" + s.repo.Name
	}
	move := shortSHA(s.from) + " → " + shortSHA(s.to)
	switch {
	case s.from == "":
		move = "added at " + shortSHA(s.to)
	case s.to == "":
		move = "removed (was at " + shortSHA(s.from) + ")"
	}
	lines := []string{styleSection.Render("Submodule "+name) + "  " + styleCyan.Render(move)}
	switch {
	case s.note != "":
		lines = append(lines, styleDim.Render("  "+s.note))
	case len(s.commits) > 0:
		what := fmt.Sprintf("%d new %s", len(s.commits), plural(len(s.commits), "commit", "commits"))
		if s.back {
			what = fmt.Sprintf("moved back, dropping %d %s", len(s.commits), plural(len(s.commits), "commit", "commits"))
		}
		lines = append(lines, styleDim.Render("  "+what), "")
		for i, c := range s.commits {
			if len(lines) == height-1 && i < len(s.commits)-1 {
				lines = append(lines, styleDim.Render(fmt.Sprintf("  …and %d more", len(s.commits)-i)))
				break
			}
			lines = append(lines, "  "+styleDim.Render(shortSHA(c.ID))+" "+firstLine(c.Comment))
		}
	}
	for i := range lines {
		lines[i] = truncate(lines[i], width)
	}
	return lines
}
