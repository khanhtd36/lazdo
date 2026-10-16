package ui

import (
	"fmt"
	"net/url"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// commandGroups are the palette's groups for where the user is, nearest
// first, ending with the organization's.
func (m Model) commandGroups() []commandGroup {
	var gs []commandGroup
	switch {
	case m.detail != nil:
		gs = m.detail.commandGroups()
		if !m.detail.standalone {
			gs = append(gs, m.ancestorCommands(m.detail.pr.Repository)...)
		}
	case m.project != nil:
		// The project's own commands come with the next release.
	case m.page == pageProjects:
		gs = m.projectsPageCommands()
	default:
		gs = m.dashboardCommands()
	}
	return append(gs, m.orgCommands())
}

// --- Dashboard ---

func (m Model) dashboardCommands() []commandGroup {
	rows := m.rows()
	r, ok := m.selected(rows)
	page := commandGroup{title: "Pull requests page", commands: []command{
		{id: "Filter", label: "Filter pull requests", key: "/", run: replayKey("/")},
		{id: "Refresh", label: "Refresh", key: "r", run: replayKey("r")},
	}}
	switch {
	case !ok:
		return []commandGroup{page}
	case r.pr == nil:
		kind := m.sections[r.section].Kind
		return []commandGroup{{title: "Section " + kind.Title(), commands: []command{{
			id: "Fold", label: pick2(m.collapsed[kind], "Expand the section", "Collapse the section"), key: "enter", run: replayKey("enter"),
		}}}, page}
	}
	pr := *r.pr
	gs := m.prCommandsFor(pr)
	return append(append(gs, m.ancestorCommands(pr.Repository)...), page)
}

// prCommandsFor are a pull request's commands from outside it: the same as
// inside, but those needing its details open it first and run once it
// has loaded.
func (m Model) prCommandsFor(pr ado.PullRequest) []commandGroup {
	d := newDetail(m.client, m.me, pr, m.width, m.height)
	d.data = &ado.PRDetail{PullRequest: pr}
	first := []command{
		{id: "Open", label: "Open", key: "enter", run: replayKey("enter")},
		{id: "Open in browser", label: "Open in browser", key: "o", run: replayKey("o")},
	}
	gs := make([]commandGroup, 0, 2)
	for i, g := range []commandGroup{d.prCommands(), d.reviewerCommands()} {
		var cmds []command
		if i == 0 {
			cmds = first
		}
		for _, c := range g.commands {
			if c.id == "Open in browser" {
				continue // the dashboard's own, above
			}
			if !c.local {
				c.run, c.disabled = openThen(pr, c.id), ""
			}
			cmds = append(cmds, c)
		}
		gs = append(gs, commandGroup{title: g.title, commands: cmds})
	}
	return gs
}

// openThen opens a pull request and runs one of its commands once it has
// loaded.
func openThen(pr ado.PullRequest, id string) func(*Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		next, cmd := m.openDetail(&pr)
		*m = next.(Model)
		m.detail.pending = id
		return cmd
	}
}

// --- Projects page ---

func (m Model) projectsPageCommands() []commandGroup {
	page := commandGroup{title: "Projects page", commands: []command{
		{id: "Search", label: "Search projects and repos", key: "/", run: replayKey("/")},
		{id: "Refresh", label: "Refresh", key: "r", run: replayKey("r")},
	}}
	it, ok := m.projects.list.selected()
	if !ok {
		return []commandGroup{page}
	}
	switch v := it.value.(type) {
	case ado.ProjectInfo:
		g := m.projectCommands(ado.Project{ID: v.ID, Name: v.Name})
		g.commands = append([]command{{id: "Open", label: "Open", key: "enter", run: replayKey("enter")}}, g.commands...)
		return []commandGroup{g, page}
	case ado.Repo:
		return append(m.ancestorCommands(ado.Repository{ID: v.ID, Name: v.Name, Project: v.Project}), page)
	}
	return []commandGroup{page}
}

// --- Ancestors ---

// ancestorCommands are a repo's and its project's.
func (m Model) ancestorCommands(r ado.Repository) []commandGroup {
	repo := m.fullRepo(r)
	cmds := append([]command{
		{id: "Open repo", label: "Open the repo", run: m.jumpTo(jump{project: r.Project, repo: &repo})},
		{id: "Open repo in browser", label: "Open the repo in the browser", run: func(*Model) tea.Cmd { return openURL(repo.WebURL, "opened "+repo.Name) }},
	}, copyCommands(copyRepo(repo), "")...)
	return []commandGroup{{title: "Repo " + r.Name, commands: cmds}, m.projectCommands(r.Project)}
}

func (m Model) projectCommands(p ado.Project) commandGroup {
	info := ado.ProjectInfo{ID: p.ID, Name: p.Name}
	return commandGroup{title: "Project " + p.Name, commands: []command{
		{id: "Repos", label: "Open the project's repos", run: m.jumpTo(jump{project: p, tab: projTabRepos})},
		{id: "Pull requests", label: "Open the project's pull requests", run: m.jumpTo(jump{project: p, tab: projTabPRs})},
		{id: "Pipelines", label: "Open the project's pipelines", run: m.jumpTo(jump{project: p, tab: projTabPipelines})},
		{id: "Settings", label: "Open the project's settings", run: m.jumpTo(jump{project: p, tab: projTabSettings})},
		{id: "Open in browser", label: "Open the project in the browser", run: func(*Model) tea.Cmd {
			return openURL(m.client.ProjectURL(info), "opened "+p.Name)
		}},
	}}
}

func (m Model) orgCommands() commandGroup {
	cmds := []command{}
	if m.detail == nil && m.project == nil {
		cmds = append(cmds,
			command{id: "Pull requests page", label: "Go to the Pull requests page", key: "1", run: replayKey("1")},
			command{id: "Projects page", label: "Go to the Projects page", key: "2", run: replayKey("2")},
		)
	}
	cmds = append(cmds,
		command{id: "Switch", label: "Switch organization or account…", key: "O", run: func(m *Model) tea.Cmd {
			next, cmd := m.openOrgPicker()
			*m = next.(Model)
			return cmd
		}},
		command{id: "Updates", label: "Check for updates", key: "U", run: func(m *Model) tea.Cmd {
			next, cmd := m.checkUpdatesNow()
			*m = next.(Model)
			return cmd
		}},
	)
	return commandGroup{title: "Organization " + m.client.Org, commands: cmds}
}

// fullRepo is a repo with its URLs: from the Projects page when loaded,
// else as Azure DevOps forms them.
func (m Model) fullRepo(r ado.Repository) ado.Repo {
	for _, x := range m.projects.repos {
		if x.ID == r.ID {
			return x
		}
	}
	org, project, name := url.PathEscape(m.client.Org), url.PathEscape(r.Project.Name), url.PathEscape(r.Name)
	return ado.Repo{
		ID: r.ID, Name: r.Name, Project: r.Project,
		WebURL:    fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s", org, project, name),
		RemoteURL: fmt.Sprintf("https://%s@dev.azure.com/%s/%s/_git/%s", org, org, project, name),
		SSHURL:    fmt.Sprintf("git@ssh.dev.azure.com:v3/%s/%s/%s", org, project, name),
	}
}

// --- Jumps ---

// jump opens a project, on a repo's browser or one of its tabs.
type jump struct {
	project ado.Project
	repo    *ado.Repo
	tab     projectTab
}

// covered is what a jump opened on top of; esc at the jump's level comes
// back to it.
type covered struct {
	detail  *detailModel
	project *projectModel
	level   projectLevel // where the jump landed
}

func (m Model) jumpTo(j jump) func(*Model) tea.Cmd {
	return func(m *Model) tea.Cmd { return m.doJump(j) }
}

// doJump opens the project on top of what is open; the project's repos
// come from the Projects page, loaded first if need be.
func (m *Model) doJump(j jump) tea.Cmd {
	if !m.projects.loaded {
		m.pendingJump, m.status = &j, "loading projects…"
		return m.projects.load(m.client)
	}
	info := ado.ProjectInfo{ID: j.project.ID, Name: j.project.Name}
	for _, p := range m.projects.projects {
		if p.ID == j.project.ID {
			info = p
		}
	}
	p := m.projects.newProject(m.client, info)
	p.width, p.height = m.width, m.height
	cmds := []tea.Cmd{p.init()}
	if j.repo != nil {
		cmds = append(cmds, p.openRepo(*j.repo))
	} else {
		p.tab = j.tab
		cmds = append(cmds, p.onTabChange())
	}
	if m.detail != nil || m.project != nil {
		m.covers = append(m.covers, covered{detail: m.detail, project: m.project, level: p.level})
	}
	m.detail, m.project = nil, p
	return tea.Batch(cmds...)
}

// uncover goes back to what the latest jump opened on top of.
func (m *Model) uncover() {
	c := m.covers[len(m.covers)-1]
	m.covers = m.covers[:len(m.covers)-1]
	m.detail, m.project = c.detail, c.project
}
