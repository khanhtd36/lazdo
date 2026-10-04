package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

type page int

const (
	pagePRs page = iota
	pageProjects
	pageCount
)

func (p page) title() string {
	if p == pageProjects {
		return "Projects"
	}
	return "Pull requests"
}

// projectsPage lists projects (recent, active, then all) and, while
// filtering, every repo in the organization.
type projectsPage struct {
	loading bool
	loaded  bool
	err     error

	projects []ado.ProjectInfo
	recent   []string
	repos    []ado.Repo
	prs      []ado.PullRequest

	list pickList
}

type projectsLoadedMsg struct {
	projects []ado.ProjectInfo
	recent   []string
	repos    []ado.Repo
	prs      []ado.PullRequest
	err      error
}

func (p *projectsPage) load(client *ado.Client) tea.Cmd {
	if p.loading {
		return nil
	}
	p.loading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var (
			msg                          projectsLoadedMsg
			wg                           sync.WaitGroup
			errP, errRecent, errR, errPR error
		)
		wg.Go(func() { msg.projects, errP = client.Projects(ctx) })
		wg.Go(func() { msg.recent, errRecent = client.RecentProjectIDs(ctx) })
		wg.Go(func() { msg.repos, errR = client.Repos(ctx) })
		wg.Go(func() { msg.prs, errPR = client.AllActivePRs(ctx) })
		wg.Wait()
		// Recent projects are a nicety from an undocumented call; the page
		// works without them.
		_ = errRecent
		msg.err = firstErr(errP, errR, errPR)
		return msg
	}
}

func (p *projectsPage) onLoaded(msg projectsLoadedMsg) {
	p.loading, p.loaded, p.err = false, true, msg.err
	if msg.err != nil && msg.projects == nil {
		return
	}
	p.projects, p.recent, p.repos, p.prs = msg.projects, msg.recent, msg.repos, msg.prs
	p.list.setItems(p.items())
}

func (p *projectsPage) prCount() map[string]int {
	n := map[string]int{}
	for _, pr := range p.prs {
		n[pr.Repository.Project.ID]++
	}
	return n
}

func (p *projectsPage) repoCount() map[string]int {
	n := map[string]int{}
	for _, r := range p.repos {
		n[r.Project.ID]++
	}
	return n
}

// items builds the rows: Recent, Active (others with open pull requests,
// most first), All projects A–Z, and every repo for / to find.
func (p *projectsPage) items() []pickItem {
	byID := map[string]ado.ProjectInfo{}
	for _, pr := range p.projects {
		byID[pr.ID] = pr
	}
	prs, repos := p.prCount(), p.repoCount()
	row := func(pr ado.ProjectInfo) pickItem {
		return pickItem{
			search: pr.Name + " " + pr.Description,
			value:  pr,
			render: func(width int) string { return projectRow(pr, prs[pr.ID], repos[pr.ID], width) },
		}
	}
	header := func(title string, n int) pickItem {
		return pickItem{header: true, render: func(int) string {
			return styleHeader.Render(fmt.Sprintf("%s (%d)", title, n))
		}}
	}

	var items []pickItem
	shown := map[string]bool{}
	var recent []ado.ProjectInfo
	for _, id := range p.recent {
		if pr, ok := byID[id]; ok {
			recent = append(recent, pr)
			shown[id] = true
		}
	}
	if len(recent) > 0 {
		items = append(items, header("Recent", len(recent)))
		for _, pr := range recent {
			items = append(items, row(pr))
		}
	}

	var active []ado.ProjectInfo
	for _, pr := range p.projects {
		if prs[pr.ID] > 0 && !shown[pr.ID] {
			active = append(active, pr)
		}
	}
	sort.Slice(active, func(i, j int) bool { return prs[active[i].ID] > prs[active[j].ID] })
	if len(active) > 0 {
		items = append(items, header("Active", len(active)))
		for _, pr := range active {
			items = append(items, row(pr))
		}
	}

	all := append([]ado.ProjectInfo(nil), p.projects...)
	sort.Slice(all, func(i, j int) bool { return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name) })
	items = append(items, header("All projects", len(all)))
	for _, pr := range all {
		if shown[pr.ID] || prs[pr.ID] > 0 {
			// Listed above already; keep it out of the search twice.
			it := row(pr)
			it.search = ""
			items = append(items, it)
			continue
		}
		items = append(items, row(pr))
	}

	for _, r := range p.repos {
		items = append(items, pickItem{
			searchOnly: true,
			search:     r.Name + " " + r.Project.Name,
			value:      r,
			render:     func(width int) string { return repoSearchRow(r, width) },
		})
	}
	return items
}

func projectRow(p ado.ProjectInfo, prs, repos, width int) string {
	badges := ""
	if prs > 0 {
		badges = styleCyan.Render(fmt.Sprintf("[%d %s]", prs, plural(prs, "PR", "PRs")))
	}
	repoText := ""
	if repos > 0 {
		repoText = styleDim.Render(fmt.Sprintf("%d %s", repos, plural(repos, "repo", "repos")))
	}
	desc := strings.Join(strings.Fields(p.Description), " ")
	return truncate(joinCols(styleTitle.Render(p.Name), badges, repoText, styleDim.Render(desc)), width)
}

func repoSearchRow(r ado.Repo, width int) string {
	return truncate(joinCols(styleCyan.Render("⎇ ")+styleTitle.Render(r.Name),
		styleDim.Render("repo in "+r.Project.Name), styleDim.Render(r.DefaultBranchName())), width)
}

// key handles the Projects page; open asks the root to open a project,
// optionally straight at one of its repos.
func (p *projectsPage) key(msg tea.KeyMsg, height int, client *ado.Client) (cmd tea.Cmd, open *projectModel) {
	handled, activate := p.list.key(msg, height)
	if activate {
		open, cmd = p.openSelected(client)
		return cmd, open
	}
	if handled {
		return nil, nil
	}
	it, ok := p.list.selected()
	switch msg.String() {
	case "r":
		return p.load(client), nil
	case "o":
		if ok {
			if r, isRepo := it.value.(ado.Repo); isRepo {
				return openURL(r.WebURL, "opened "+r.Name), nil
			}
			pr := it.value.(ado.ProjectInfo)
			return openURL(client.ProjectURL(pr), "opened "+pr.Name), nil
		}
	case "y":
		if !ok {
			return nil, nil
		}
		if r, isRepo := it.value.(ado.Repo); isRepo {
			return copyRepo(r), nil
		}
		return copyProject(client, it.value.(ado.ProjectInfo)), nil
	}
	return nil, nil
}

// openSelected opens the selected project, or a repo's project at that
// repo's branches; the command loads what the project needs first.
func (p *projectsPage) openSelected(client *ado.Client) (*projectModel, tea.Cmd) {
	it, ok := p.list.selected()
	if !ok {
		return nil, nil
	}
	switch v := it.value.(type) {
	case ado.ProjectInfo:
		m := p.newProject(client, v)
		return m, m.init()
	case ado.Repo:
		for _, pr := range p.projects {
			if pr.ID == v.Project.ID {
				m := p.newProject(client, pr)
				return m, tea.Batch(m.init(), m.openRepo(v))
			}
		}
	}
	return nil, nil
}

func (p *projectsPage) newProject(client *ado.Client, pr ado.ProjectInfo) *projectModel {
	var prs []ado.PullRequest
	for _, x := range p.prs {
		if x.Repository.Project.ID == pr.ID {
			prs = append(prs, x)
		}
	}
	var repos []ado.Repo
	for _, r := range p.repos {
		if r.Project.ID == pr.ID {
			repos = append(repos, r)
		}
	}
	return newProject(client, pr, prs, repos)
}

func (p *projectsPage) view(width, height int) []string {
	switch {
	case p.err != nil && !p.loaded:
		return []string{styleRed.Render(truncate("error: "+p.err.Error(), width))}
	case !p.loaded:
		return []string{styleDim.Render("  loading projects…")}
	}
	return p.list.view(width, height)
}
