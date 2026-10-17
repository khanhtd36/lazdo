package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// The command palette inside a project: the focused item of the level
// shown (a repo, pull request, pipeline, settings entry, branch, file,
// commit, tag or run), then what holds it: the repo or pipeline, the
// project and the organization.

func (m Model) projectViewCommands() []commandGroup {
	p := m.project
	var gs []commandGroup
	switch p.level {
	case levelTabs:
		gs = m.projectTabCommands()
	case levelRepo:
		gs = append(m.repoBrowserCommands(), m.inRepoCommands())
	case levelRuns:
		gs = append(m.runsCommands(), m.pipelineCommands(p.pipeline))
	case levelRun:
		gs = append([]commandGroup{m.projectRunCommands()}, m.pipelineCommands(p.pipeline))
	}
	return append(gs, m.inProjectCommands())
}

// inProjectCommands are the open project's own: its tabs.
func (m Model) inProjectCommands() commandGroup {
	p := m.project
	cmds := make([]command, 0, int(projTabCount)+3)
	for t := range projTabCount {
		if p.level != levelTabs || t != p.tab {
			key := fmt.Sprint(int(t) + 1)
			if p.level == levelRepo {
				key = "" // there 1-3 are the repo's tabs
			}
			cmds = append(cmds, command{id: "Tab " + t.title(), label: "Go to " + t.title(), key: key, run: func(m *Model) tea.Cmd {
				m.project.level, m.project.tab, m.project.run, m.project.browser = levelTabs, t, nil, nil
				return m.project.onTabChange()
			}})
		}
	}
	cmds = append(cmds, command{id: "Open in browser", label: "Open the project in the browser", run: func(*Model) tea.Cmd {
		return openURL(m.client.ProjectURL(p.project), "opened "+p.project.Name)
	}})
	cmds = append(cmds, copyCommands(copyProject(m.client, p.project), "")...)
	return commandGroup{title: "Project " + p.project.Name, commands: cmds}
}

// --- The tabs ---

func (m Model) projectTabCommands() []commandGroup {
	p := m.project
	if p.tab == projTabSettings {
		return m.settingsCommands()
	}
	it, ok := p.lists[p.tab].selected()
	if !ok {
		return nil
	}
	switch v := it.value.(type) {
	case ado.Repo:
		return []commandGroup{m.repoCommands(v, true)}
	case ado.PullRequest:
		gs := m.prCommandsFor(v)
		return append(gs, m.repoCommands(m.fullRepo(v.Repository), false))
	case ado.Pipeline:
		gs := []commandGroup{m.pipelineCommands(v)}
		if r := v.LatestBuild; r != nil {
			gs = append(gs, m.latestRunCommands(v, *r))
		}
		return gs
	}
	return nil
}

// repoCommands are a repo's in its own project: open opens its browser
// there, not a project on top.
func (m Model) repoCommands(r ado.Repo, selected bool) commandGroup {
	open := command{id: "Open repo", label: "Open the repo", key: "enter", run: replayKey("enter")}
	if !selected {
		open = command{id: "Open repo", label: "Open the repo", run: func(m *Model) tea.Cmd { return m.project.openRepo(r) }}
	}
	cmds := append([]command{
		open,
		{id: "Open repo in browser", label: "Open the repo in the browser", run: func(*Model) tea.Cmd { return openURL(r.WebURL, "opened "+r.Name) }},
	}, copyCommands(copyRepo(r), pick2(selected, "y", ""))...)
	return commandGroup{title: "Repo " + r.Name, commands: cmds}
}

func (m Model) pipelineCommands(pl ado.Pipeline) commandGroup {
	p := m.project
	url := m.client.PipelineURL(p.project.Name, pl.ID)
	cmds := []command{}
	if p.level == levelTabs {
		cmds = append(cmds, command{id: "Runs", label: "Open its runs", key: "enter", run: replayKey("enter")})
	} else {
		cmds = append(cmds, command{id: "Runs", label: "Back to its runs", run: func(m *Model) tea.Cmd {
			m.project.level, m.project.run = levelRuns, nil
			return nil
		}})
	}
	cmds = append(cmds, command{id: "Open in browser", label: "Open the pipeline in the browser", run: func(*Model) tea.Cmd { return openURL(url, "opened "+pl.Name) }})
	cmds = append(cmds, copyCommands(copyMenu("pipeline "+pl.Name, copyItem{"Web URL", url}, copyItem{"Name", pl.Name}), "")...)
	return commandGroup{title: "Pipeline " + pl.Name, commands: cmds}
}

// latestRunCommands are a pipeline's latest run, a child of it.
func (m Model) latestRunCommands(pl ado.Pipeline, r ado.Run) commandGroup {
	open := command{id: "Open latest", label: "Open the run", run: func(m *Model) tea.Cmd {
		p := m.project
		load := p.openPipeline(pl)
		p.run = newRunView(p.client, p.project, r)
		p.level = levelRun
		return tea.Batch(load, p.run.refresh())
	}}
	browser := command{id: "Open latest in browser", label: "Open the run in the browser", run: func(*Model) tea.Cmd {
		return openURL(m.client.RunURL(m.project.project.Name, r.ID), "opened run "+r.BuildNumber)
	}}
	cmds := append([]command{open, browser}, copyCommands(copyRun(m.client, m.project.project.Name, r), "")...)
	return commandGroup{title: fmt.Sprintf("Latest run %s · %s", r.BuildNumber, runOutcome(r)), commands: cmds}
}

func runOutcome(r ado.Run) string {
	if r.Result != "" {
		return r.Result
	}
	return r.Status
}

// --- Settings ---

// settingsCommands are the open settings section's: what its keys do to
// the selected entry, or to the list a member, permission or group view
// shows. Keys act on the content pane, so commands move there first.
func (m Model) settingsCommands() []commandGroup {
	s := m.project.settings
	if s.data == nil {
		return nil
	}
	right := func(k string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			m.project.settings.onRight = true
			return replayKey(k)(m)
		}
	}
	c := func(label, k string) command {
		return command{id: label, label: label, key: pick2(k == " ", "space", k), run: right(k)}
	}
	reload := command{id: "Reload", label: "Reload the settings", key: "r", run: replayKey("r")}
	if d := s.detail; d != nil {
		var cmds []command
		switch {
		case d.reload != nil:
			cmds = []command{c("Add a member…", "a"), c("Remove the member…", "d")}
		case d.perm != nil:
			cmds = []command{c("Change the permission", " "), c("Save permission changes…", "ctrl+s")}
		case d.user != "":
			cmds = []command{c("Add to a group…", "a"), c("Remove from the group…", "d")}
		}
		return []commandGroup{{title: "Settings · " + s.section.title(), commands: cmds}}
	}
	cmds := make([]command, 0, 5)
	_, entry := s.content.selected()
	switch s.section {
	case secTeams:
		cmds = []command{c("New team…", "a")}
		if entry {
			cmds = append(cmds, c("Members of the team", "enter"), c("Edit the team…", "e"), c("Delete the team…", "d"))
		}
	case secGroups:
		cmds = []command{c("New group…", "a")}
		if entry {
			cmds = append(cmds, c("Members of the group", "enter"), c("Edit the group…", "e"), c("Delete the group…", "d"))
		}
	case secPermissions:
		cmds = []command{c("Add users…", "a")}
		if it, ok := s.content.selected(); ok {
			if _, user := it.value.(ado.ProjectUser); user {
				cmds = append(cmds, c("Groups of the user", "enter"), c("Remove the user…", "d"))
			} else {
				cmds = append(cmds, c("Permissions of the group", "enter"))
			}
		}
	case secRepos:
		cmds = []command{c("New repository…", "a")}
		if entry {
			cmds = append(cmds, c("Edit the repository…", "e"), c("Delete the repository…", "d"))
		}
	case secPolicies:
		cmds = []command{c("New branch policy…", "a")}
		if entry {
			cmds = append(cmds, c("Edit the policy…", "e"), c("Turn the policy on or off", " "), c("Delete the policy…", "d"))
		}
	case secConnections:
		if entry {
			cmds = []command{c("Open to all pipelines, or stop…", " "), c("Delete the connection…", "d")}
		}
	case secVarGroups:
		cmds = []command{c("New variable group…", "A"), c("New variable…", "a")}
		if entry {
			cmds = append(cmds, c("Edit…", "e"), c("Delete…", "d"))
		}
	case secOverview, secQueues, secCount:
	}
	return []commandGroup{{title: "Settings · " + s.section.title(), commands: append(cmds, reload)}}
}

// --- The repo browser ---

func (m Model) repoBrowserCommands() []commandGroup {
	b := m.project.browser
	if b == nil || b.branch == "" {
		return nil
	}
	switch b.tab {
	case repoTabCommits:
		if b.pane == paneBranches {
			return m.branchCommands()
		}
		c, ok := b.selectedCommit()
		if !ok {
			return nil
		}
		g := m.commitCommands(c)
		if b.hist.release != nil {
			g.commands = append(g.commands, command{id: "Back to tags", label: "Back to the tags", key: "esc", run: replayKey("esc")})
		}
		return []commandGroup{g}
	case repoTabTags:
		t, ok := b.selectedTag()
		if !ok {
			return nil
		}
		return []commandGroup{m.tagCommands(t)}
	case repoTabFiles, repoTabCount:
	}
	var gs []commandGroup
	switch b.pane {
	case paneBranches:
		return append(m.branchCommands(), m.filesViewCommands())
	case paneFiles:
		if it, ok := b.selectedItem(); ok {
			gs = append(gs, m.treeItemCommands(it))
		}
	case paneContent:
		if b.file != "" {
			gs = append(gs, m.contentCommands())
		}
	}
	return append(gs, m.currentBranchCommands(), m.filesViewCommands())
}

// branchCommands act on the branch pane's selected branch.
func (m Model) branchCommands() []commandGroup {
	b := m.project.browser
	it, ok := b.branches.selected()
	if !ok {
		return nil
	}
	br, _ := it.value.(ado.Branch)
	del := command{id: "Delete branch", label: "Delete the branch…", key: "d", run: replayKey("d")}
	if br.Name == b.repo.DefaultBranchName() {
		del.disabled = "the default branch can't be deleted"
	}
	cmds := append([]command{
		{id: "Switch", label: "Switch to it", key: "enter", run: replayKey("enter")},
		{id: "Check out", label: "Check out " + br.Name + "…", key: "c", run: replayKey("c")},
		del,
		{id: "Open in browser", label: "Open the branch in the browser", run: func(*Model) tea.Cmd {
			return openURL(m.client.BranchURL(b.repo, br.Name), "opened "+br.Name)
		}},
	}, copyCommands(copyMenu("branch", copyItem{"Name", br.Name}, copyItem{"Web URL", m.client.BranchURL(b.repo, br.Name)}), "y")...)
	return []commandGroup{{title: "Branch " + br.Name, commands: cmds}}
}

// currentBranchCommands are the branch the files are of.
func (m Model) currentBranchCommands() commandGroup {
	b := m.project.browser
	br := b.branch
	cmds := append([]command{
		{id: "Check out", label: "Check out " + br + "…", key: "c", run: replayKey("c")},
		{id: "Open in browser", label: "Open the branch in the browser", run: func(*Model) tea.Cmd {
			return openURL(m.client.BranchURL(b.repo, br), "opened "+br)
		}},
	}, copyCommands(copyMenu("branch", copyItem{"Name", br}, copyItem{"Web URL", m.client.BranchURL(b.repo, br)}), "")...)
	return commandGroup{title: "Branch " + br, commands: cmds}
}

func (m Model) treeItemCommands(it ado.RepoItem) commandGroup {
	b := m.project.browser
	url := m.client.FileURL(b.repo, b.branch, it.Path)
	open := command{id: "Open", label: "Open the file", key: "enter", run: replayKey("enter")}
	kind := "File "
	if it.IsFolder {
		open.label = pick2(b.expanded[it.Path], "Close the folder", "Open the folder")
		kind = "Folder "
	}
	cmds := append([]command{open, {id: "Open in browser", label: "Open in browser", run: func(*Model) tea.Cmd { return openURL(url, "opened "+it.Path) }}}, copyCommands(b.copySelection(), "y")...)
	return commandGroup{title: kind + it.Path, commands: cmds}
}

func (m Model) contentCommands() commandGroup {
	b := m.project.browser
	cmds := []command{
		{id: "Find", label: "Find text", key: "/", run: replayKey("/")},
		{id: "Select", label: pick2(b.anchor >= 0, "Stop selecting lines", "Select lines"), key: "V", run: replayKey("V")},
	}
	if b.anchor >= 0 {
		cmds = append(cmds, command{id: "Copy lines", label: "Copy the selected lines", key: "y", run: replayKey("y")})
	}
	if c := b.currentContent(); c != nil && c.markdown {
		cmds = append(cmds, command{id: "Markdown", label: pick2(b.markdownRaw, "Show the markdown rendered", "Show the markdown raw"), key: "M", run: replayKey("M")})
	}
	cmds = append(cmds, command{id: "Open in browser", label: "Open in browser", key: "o", run: replayKey("o")})
	if b.anchor < 0 {
		cmds = append(cmds, copyCommands(b.copySelection(), "y")...)
	}
	return commandGroup{title: "File " + b.file, commands: cmds}
}

func (m Model) filesViewCommands() commandGroup {
	b := m.project.browser
	return commandGroup{title: "Files", commands: []command{
		{id: "Branches", label: pick2(b.hideBranches, "Show the branches", "Hide the branches"), key: "z", run: replayKey("z")},
		{id: "Refresh", label: "Refresh", key: "r", run: replayKey("r")},
	}}
}

func (m Model) commitCommands(c ado.RepoCommit) commandGroup {
	b := m.project.browser
	short := shortSHA(c.ID)
	cmds := append([]command{
		{id: "Diff", label: "Show its diff", key: "enter", run: replayKey("enter")},
		{id: "Tag", label: "Tag it…", key: "T", run: replayKey("T")},
		{id: "Check out", label: "Check out the commit…", key: "c", run: replayKey("c")},
		{id: "Open in browser", label: "Open in browser", key: "o", run: replayKey("o")},
	}, copyCommands(copyMenu("commit "+short,
		copyItem{"Web URL", m.client.CommitURL(b.repo, c.ID)}, copyItem{"Commit ID", c.ID},
		copyItem{"Short ID", short}, copyItem{"Message", firstLine(c.Comment)}), "y")...)
	return commandGroup{title: "Commit " + short + " " + firstLine(c.Comment), short: "Commit " + short, commands: cmds}
}

func (m Model) tagCommands(t ado.Tag) commandGroup {
	name := t.Name
	return commandGroup{title: "Tag " + name, commands: []command{
		{id: "Changes", label: "Release changes", key: "enter", run: replayKey("enter")},
		{id: "Diff", label: "Diff since the previous tag", key: "D", run: replayKey("D")},
		{id: "Check out", label: "Check out the tag…", key: "c", run: replayKey("c")},
		{id: "Delete", label: "Delete the tag…", key: "d", run: replayKey("d")},
		{id: "Open in browser", label: "Open in browser", key: "o", run: replayKey("o")},
		{id: "Copy", label: "Copy the tag name", key: "y", local: true, run: func(*Model) tea.Cmd { return copyText(name, "copied "+name) }},
	}}
}

// inRepoCommands are the open repo's own: its tabs.
func (m Model) inRepoCommands() commandGroup {
	b := m.project.browser
	r := m.project.repo
	cmds := []command{}
	if b != nil {
		for i, t := range []string{"Files", "Commits", "Tags"} {
			if repoTab(i) != b.tab {
				k := fmt.Sprint(i + 1)
				cmds = append(cmds, command{id: "Tab " + t, label: "Go to " + t, key: k, run: replayKey(k)})
			}
		}
	}
	cmds = append(cmds, command{id: "Open in browser", label: "Open the repo in the browser", run: func(*Model) tea.Cmd { return openURL(r.WebURL, "opened "+r.Name) }})
	cmds = append(cmds, copyCommands(copyRepo(r), "")...)
	return commandGroup{title: "Repo " + r.Name, commands: cmds}
}

// --- Runs ---

func (m Model) runsCommands() []commandGroup {
	p := m.project
	it, ok := p.runList.selected()
	if !ok {
		return nil
	}
	r, _ := it.value.(ado.Run)
	cmds := append([]command{
		{id: "Open", label: "Open the run", key: "enter", run: replayKey("enter")},
		{id: "Open in browser", label: "Open the run in the browser", key: "o", run: replayKey("o")},
		{id: "Refresh", label: "Refresh the runs", key: "r", run: replayKey("r")},
	}, copyCommands(copyRun(m.client, p.project.Name, r), "y")...)
	return []commandGroup{{title: fmt.Sprintf("Run %s · %s", r.BuildNumber, runOutcome(r)), commands: cmds}}
}

func (m Model) projectRunCommands() commandGroup {
	p := m.project
	r := p.run.run
	cmds := append([]command{
		{id: "Open in browser", label: "Open the run in the browser", key: "o", run: replayKey("o")},
		{id: "Follow", label: "Follow the log", key: "G", run: func(m *Model) tea.Cmd {
			m.project.run.logPane = true
			return replayKey("G")(m)
		}},
		{id: "Refresh", label: "Refresh the run", key: "r", run: replayKey("r")},
	}, copyCommands(copyRun(m.client, p.project.Name, r), "")...)
	return commandGroup{title: fmt.Sprintf("Run %s · %s", r.BuildNumber, strings.TrimSpace(runOutcome(r))), commands: cmds}
}
