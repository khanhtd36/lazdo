package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// The command palette's groups for a pull request: the focused item first
// (a thread, file, folder, line, commit, conflict or run), then the pull
// request, its reviewers and build policies. The root adds the repo,
// project and organization.

// onDetail runs a detail action as a command.
func onDetail(f func() tea.Cmd) func(*Model) tea.Cmd {
	return func(*Model) tea.Cmd { return f() }
}

// replayKey runs a command as its key, for what a key already does.
func replayKey(k string) func(*Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		next, cmd := m.Update(keyMsg(k))
		*m = next.(Model)
		return cmd
	}
}

// showModal runs a command that opens a popup in the pull request.
func (d *detailModel) showModal(f func() modal) func(*Model) tea.Cmd {
	return onDetail(func() tea.Cmd { d.modal = f(); return nil })
}

// menuCommands are a menu's choices as commands, each still running as
// the menu would; prefix names the menu ("Show: ").
func (d *detailModel) menuCommands(m modal, key, prefix string) []command {
	menu, ok := m.(*menuModal)
	if !ok {
		return nil
	}
	out := make([]command, 0, len(menu.items))
	for _, it := range menu.items {
		label := strings.TrimPrefix(strings.TrimPrefix(it.label, "✓ "), "  ")
		// "Show everything" needs no "Show: " before it.
		if word := strings.TrimSuffix(prefix, ": "); word == "" || !strings.HasPrefix(label, word+" ") {
			label = prefix + label
		}
		out = append(out, command{
			id: ansi.Strip(label), label: label, key: key, disabled: it.disabled,
			run: onDetail(func() tea.Cmd {
				var cmd tea.Cmd
				d.modal, cmd = it.run()
				return cmd
			}),
		})
	}
	return out
}

// copyCommands are a Copy menu's choices as commands.
func copyCommands(c tea.Cmd, key string) []command {
	if c == nil {
		return nil
	}
	msg, _ := c().(copyMenuMsg)
	out := make([]command, 0, len(msg.items))
	for _, it := range msg.items {
		out = append(out, command{
			id: "Copy " + it.label, label: "Copy " + lowerFirst(it.label), key: key, local: true,
			run: func(*Model) tea.Cmd { return copyText(it.value, "copied "+it.value) },
		})
	}
	return out
}

// lowerFirst lowers "Web URL" to "web URL", but not "ID".
func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 1 && r[1] >= 'a' && r[1] <= 'z' {
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}

// runPending runs the command picked before the pull request loaded, now
// that its details can say whether it may run.
func (d *detailModel) runPending() tea.Cmd {
	id := d.pending
	d.pending = ""
	for _, g := range d.commandGroups() {
		for _, c := range g.commands {
			if c.id != id {
				continue
			}
			if c.disabled != "" {
				return statusCmd(c.disabled)
			}
			return func() tea.Msg { return paletteRunMsg{run: c.run} }
		}
	}
	return nil
}

func (d *detailModel) commandGroups() []commandGroup {
	if d.data == nil {
		return nil
	}
	var gs []commandGroup
	switch {
	case d.run != nil:
		gs = append(gs, d.runCommands())
	case d.tab == tabOverview:
		gs = append(gs, d.overviewCommands()...)
	case d.tab == tabFiles:
		gs = append(gs, d.filesCommands()...)
	case d.tab == tabCommits, d.tab == tabConflicts:
		gs = append(gs, d.listCommands()...)
	}
	if d.standalone {
		return gs
	}
	return append(gs, d.prCommands(), d.reviewerCommands(), d.policyCommands())
}

// --- The pull request and its children ---

func (d *detailModel) prCommands() commandGroup {
	pr := d.pr
	cmds := d.menuCommands(d.voteMenu(), "v", "")
	for _, c := range d.menuCommands(d.completeMenu(), "m", "") {
		switch c.id {
		case "Complete", "Set auto-complete", "Abandon":
			c.label += "…" // opens a dialog or asks
		}
		cmds = append(cmds, c)
	}
	comment := ""
	if d.tab == tabOverview {
		comment = "a"
	}
	cmds = append(cmds,
		command{id: "Edit title and description", label: "Edit title and description…", key: "E", run: d.showModal(func() modal { return d.newPREditor() })},
		command{id: "Add a comment", label: "Add a comment…", key: comment, run: d.showModal(d.newCommentEditor)},
		command{id: "Reviewers", label: "Reviewers…", key: "w", run: onDetail(d.openReviewers)},
		command{id: "Pipeline runs", label: "Pipeline runs…", key: "b", run: onDetail(d.loadRuns)},
		command{id: "Open in browser", label: "Open in browser", key: "o", local: true, run: func(*Model) tea.Cmd {
			return openURL(pr.WebURL(d.client.Org), fmt.Sprintf("opened !%d", pr.ID))
		}},
	)
	for _, l := range d.checkLinks() {
		cmds = append(cmds, command{id: "Open " + l.label, label: "Open " + l.label, run: func(*Model) tea.Cmd {
			return openURL(l.value, "opened "+l.label)
		}})
	}
	cmds = append(cmds, command{
		id: "Check out the source branch", label: "Check out " + pr.SourceBranch() + "…", key: "c", local: true,
		run: func(*Model) tea.Cmd { return prCheckout(d.client.Org, pr) },
	})
	cmds = append(cmds, copyCommands(copyPR(d.client.Org, pr), "y")...)
	for t := range tabCount {
		if t != d.tab || d.run != nil {
			k := fmt.Sprint(int(t) + 1)
			cmds = append(cmds, command{id: "Go to " + t.title(), label: "Go to " + t.title(), key: k, run: replayKey(k)})
		}
	}
	cmds = append(cmds, command{id: "Refresh", label: "Refresh", key: "r", run: onDetail(d.reload)})
	return commandGroup{title: fmt.Sprintf("Pull request !%d %s", pr.ID, pr.Title), commands: cmds}
}

func (d *detailModel) reviewerCommands() commandGroup {
	closed := d.closed()
	list := &reviewersModal{d: d}
	reviewers := list.reviewers()
	cmds := make([]command, 0, 2+2*len(reviewers))
	cmds = append(cmds,
		command{id: "Add reviewer", label: "Add reviewer…", disabled: closed, run: d.showModal(func() modal { return newPeoplePicker(d, list, false) })},
		command{id: "Add required reviewer", label: "Add required reviewer…", disabled: closed, run: d.showModal(func() modal { return newPeoplePicker(d, list, true) })},
	)
	for _, rv := range reviewers {
		cmds = append(cmds,
			command{
				id: "Make required " + rv.ID, label: fmt.Sprintf("Make %s %s", rv.DisplayName, pick2(rv.IsRequired, "optional", "required")),
				disabled: closed, run: onDetail(func() tea.Cmd { return d.toggleRequired(rv) }),
			},
			command{
				id: "Remove " + rv.ID, label: "Remove " + rv.DisplayName + "…",
				disabled: closed, run: d.showModal(func() modal { return d.confirmRemoveReviewer(rv, nil) }),
			})
	}
	return commandGroup{title: "Reviewers", commands: cmds}
}

func (d *detailModel) policyCommands() commandGroup {
	ps := d.buildPolicies()
	cmds := make([]command, 0, 2*len(ps))
	for _, p := range ps {
		name := buildPolicyName(p)
		open := command{id: "Open run " + p.EvaluationID, label: "Open the run of " + name, run: onDetail(func() tea.Cmd { return d.openPolicyRun(p) })}
		if p.BuildID() == 0 {
			open.disabled = name + " hasn't run"
		}
		requeue := command{id: "Re-queue " + p.EvaluationID, label: "Re-queue " + name + "…", key: "Q", run: onDetail(func() tea.Cmd {
			var cmd tea.Cmd
			d.modal, cmd = d.confirmRequeue(p, nil)
			return cmd
		})}
		if p.Status == "running" {
			requeue.disabled = name + " is already running"
		}
		cmds = append(cmds, open, requeue)
	}
	return commandGroup{title: "Build policies", commands: cmds}
}

// --- Focused items ---

func (d *detailModel) overviewCommands() []commandGroup {
	var gs []commandGroup
	if e, ok := d.selectedEntry(); ok && e.isHuman {
		gs = append(gs, d.threadCommands(*e.thread, d.inThread, d.commentSel))
	}
	return append(gs, commandGroup{title: "Overview", commands: append(d.menuCommands(d.filterMenu(), "f", "Show: "),
		command{id: "Find", label: "Find text", key: "/", run: replayKey("/")})})
}

// threadCommands act on a comment thread and, as its children, the
// comments in it that are Me's.
func (d *detailModel) threadCommands(t ado.Thread, inThread bool, sel int) commandGroup {
	where := "on the pull request"
	if t.ThreadContext != nil && t.ThreadContext.FilePath != "" {
		where = "on " + t.ThreadContext.FilePath
	}
	cmds := []command{{id: "Reply", label: "Reply…", key: "R", run: d.showModal(func() modal { return d.replyEditor(t) })}}
	cmds = append(cmds, d.menuCommands(d.threadStatusMenu(t), "s", "Set status: ")...)
	if !inThread {
		cmds = append(cmds, command{id: "Step in", label: "Step into the thread", key: "enter", run: replayKey("enter")})
	}
	cmds = append(cmds, copyCommands(d.copyComment(t, sel), "y")...)
	for i, c := range t.LiveComments() {
		if c.Author.ID != d.me.ID {
			continue
		}
		snippet := truncate(strings.Join(strings.Fields(c.Content), " "), 30)
		cmds = append(cmds,
			command{
				id: fmt.Sprintf("Edit comment %d", c.ID), label: "Edit my comment “" + snippet + "”…", key: "e",
				run: onDetail(func() tea.Cmd { return d.threadKey("e", t, true, i) }),
			},
			command{
				id: fmt.Sprintf("Delete comment %d", c.ID), label: "Delete my comment “" + snippet + "”…", key: "d",
				run: onDetail(func() tea.Cmd { return d.threadKey("d", t, true, i) }),
			})
	}
	return commandGroup{title: fmt.Sprintf("Thread %s · %s", where, ado.ThreadStatusTitle(t.Status)), commands: cmds}
}

func (d *detailModel) filesCommands() []commandGroup {
	f := &d.files
	var gs []commandGroup
	ch := d.selectedChange()
	if f.pane == paneDiff && ch != nil {
		if line, ok := d.lineCommands(); ok {
			gs = append(gs, line)
		}
		if t, ok := d.diffThread(); ok {
			gs = append(gs, d.threadCommands(*t, f.inThread, f.commentSel))
		}
	}
	switch folder, onFolder := d.selectedFolder(); {
	case ch != nil:
		gs = append(gs, d.fileCommands(ch))
	case onFolder:
		gs = append(gs, commandGroup{title: "Folder " + folder, commands: append([]command{
			{id: "Open or close", label: pick2(f.closed[folder], "Open the folder", "Close the folder"), key: "enter", run: onDetail(func() tea.Cmd {
				d.setFolder(folder, !f.closed[folder])
				return nil
			})},
			{id: "Open in browser", label: "Open in browser", key: "o", run: func(*Model) tea.Cmd { return openURL(d.fileURL(folder), "opened "+folder) }},
		}, copyCommands(copyMenu("folder", copyItem{"Path", folder}, copyItem{"Web URL", d.fileURL(folder)}), "y")...)})
	}
	view := []command{
		{id: "Side by side", label: pick2(d.isSideBySide(), "Show the diff inline", "Show the diff side by side"), key: "S", run: replayKey("S")},
		{id: "Tree", label: pick2(f.hideTree, "Show the file tree", "Hide the file tree"), key: "z", run: replayKey("z")},
	}
	if !d.standalone {
		view = append(d.menuCommands(d.comparisonMenu(), "u", "Compare: "), view...)
	}
	return append(gs, commandGroup{title: "Files", commands: view})
}

// lineCommands act on the diff's cursor line or V range.
func (d *detailModel) lineCommands() (commandGroup, bool) {
	f := &d.files
	_, fd := d.currentDiff()
	lines := d.diffLines()
	if fd == nil || f.cursor >= len(lines) {
		return commandGroup{}, false
	}
	n := lineOn(lines[f.cursor], d.cursorSide(lines[f.cursor]))
	title := fmt.Sprintf("Line %d", n)
	cmds := []command{{id: "Comment", label: "Comment on the line…", key: "a", run: onDetail(d.lineComment)}}
	if f.anchor >= 0 {
		title = fmt.Sprintf("Lines %d–%d of the diff", min(f.anchor, f.cursor)+1, max(f.anchor, f.cursor)+1)
		cmds = []command{
			{id: "Comment", label: "Comment on the lines…", key: "a", run: onDetail(d.lineComment)},
			{id: "Copy", label: "Copy the lines", key: "y", run: replayKey("y")},
		}
	}
	if _, ok := d.diffThread(); ok && !f.inThread {
		cmds = append(cmds, command{id: "Step in", label: "Step into the line's thread", key: "enter", run: replayKey("enter")})
	}
	return commandGroup{title: title, commands: cmds}, true
}

func (d *detailModel) fileCommands(ch *ado.Change) commandGroup {
	p := ch.Item.Path
	cmds := []command{}
	if d.files.pane == paneTree {
		cmds = append(cmds, command{id: "Show diff", label: "Show the diff", key: "enter", run: replayKey("enter")})
	}
	if !d.standalone {
		cmds = append(cmds, command{id: "Comment on file", label: "Comment on the file…", run: onDetail(d.fileComment)})
	}
	cmds = append(cmds, command{id: "Open in browser", label: "Open in browser", key: "o", run: func(*Model) tea.Cmd {
		return openURL(d.fileURL(p), "opened "+p)
	}})
	cmds = append(cmds, copyCommands(d.copyFile(p), "y")...)
	return commandGroup{title: "File " + p, commands: cmds}
}

func (d *detailModel) listCommands() []commandGroup {
	it, ok := d.lists[d.tab].selected()
	if !ok {
		return nil
	}
	line, _ := it.value.(listLine)
	switch {
	case line.commit != "":
		id := line.commit
		cmds := []command{{id: "Show diff", label: "Show its diff", key: "enter", run: onDetail(func() tea.Cmd { return d.openCommitDiff(id) })}}
		return []commandGroup{{title: "Commit " + shortSHA(id) + " " + ansi.Strip(commitSubject(d.data.Commits, id)), commands: append(cmds, copyCommands(d.copyCommit(id), "y")...)}}
	case line.path != "":
		cmds := make([]command, 0, 2)
		if line.url != "" {
			cmds = append(cmds, command{id: "Open in browser", label: "Open in browser", key: "enter", run: func(*Model) tea.Cmd { return openURL(line.url, "opened in browser") }})
		}
		return []commandGroup{{title: "Conflict " + line.path, commands: append(cmds, copyCommands(copyMenu("conflict", copyItem{"Path", line.path}), "y")...)}}
	}
	return nil
}

func commitSubject(commits []ado.Commit, id string) string {
	for _, c := range commits {
		if c.ID == id {
			return firstLine(c.Comment)
		}
	}
	return ""
}

func (d *detailModel) runCommands() commandGroup {
	r := d.run.run
	cmds := []command{}
	if d.runPolicy != nil {
		p := *d.runPolicy
		cmds = append(cmds, command{id: "Re-queue", label: "Re-queue " + buildPolicyName(p) + "…", key: "Q", run: replayKey("Q")})
	}
	cmds = append(cmds,
		command{id: "Open in browser", label: "Open the run in the browser", key: "o", run: replayKey("o")},
		command{id: "Follow", label: "Follow the log", key: "G", run: replayKey("G")},
		command{id: "Refresh", label: "Refresh the run", key: "r", run: replayKey("r")},
	)
	cmds = append(cmds, copyCommands(copyRun(d.client, d.prProject().Name, r), "y")...)
	cmds = append(cmds, command{id: "Back", label: "Back to the pull request", run: onDetail(d.closeRun)})
	return commandGroup{title: fmt.Sprintf("Run %s %s", d.runName, r.BuildNumber), commands: cmds}
}
