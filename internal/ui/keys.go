package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// binding documents one shortcut; this table is the single source for the
// help popup (?) and the footer hints. press is the key the help popup
// replays when the user runs the entry (empty: describe only); hint is the
// footer's short label (empty: help only).
type binding struct {
	keys  string
	desc  string
	press string
	hint  string
}

type bindingGroup struct {
	title    string
	bindings []binding
}

// The same keys mean the same thing everywhere outside text inputs:
// j/k move, ctrl+d/ctrl+u half page, g/G ends, enter deeper, esc back one
// level, q quit, / filter or find, n/N next match, J/K next group, tab
// next pane, y copy menu, o browser, r refresh.
var (
	moveKeys = []binding{
		{keys: "j / k", desc: "move (or scroll) down / up", hint: "move"},
		{keys: "ctrl+d / ctrl+u", desc: "half page down / up", press: "ctrl+d"},
		{keys: "g / G", desc: "top / bottom", press: "g"},
	}
	dashboardKeys = []binding{
		{keys: "enter", desc: "open the pull request, or collapse a section", press: "enter", hint: "open"},
		{keys: "J / K", desc: "next / previous section", press: "J", hint: "section"},
		{keys: "/", desc: "filter pull requests by title, author, ID or branch", press: "/", hint: "filter"},
		{keys: "y", desc: "copy menu: URL, branch, ID, title", press: "y", hint: "copy"},
		{keys: "o", desc: "open the pull request in the browser", press: "o", hint: "browser"},
		{keys: "c", desc: "check out the source branch (asks where)", press: "c", hint: "checkout"},
		{keys: "r", desc: "refresh", press: "r", hint: "refresh"},
	}
	projectsPageKeys = []binding{
		{keys: "enter", desc: "open the project (or a found repo's browser)", press: "enter", hint: "open"},
		{keys: "J / K", desc: "next / previous group", press: "J"},
		{keys: "/", desc: "search projects and every repo", press: "/", hint: "search"},
		{keys: "y", desc: "copy menu", press: "y", hint: "copy"},
		{keys: "o", desc: "open in the browser", press: "o", hint: "browser"},
		{keys: "r", desc: "refresh", press: "r", hint: "refresh"},
	}
	pageKeys = []binding{
		{keys: "1 / 2", desc: "Pull requests / Projects page", hint: "pages"},
		{keys: "[ / ]", desc: "previous / next page", press: "]"},
	}
	detailKeys = []binding{
		{keys: "1-4", desc: "Overview, Files, Commits, Conflicts tab", hint: "tabs"},
		{keys: "[ / ]", desc: "previous / next tab", press: "]"},
		{keys: "v", desc: "vote: approve, suggestions, wait for author, reject, reset", press: "v", hint: "vote"},
		{keys: "m", desc: "complete, auto-complete, draft/publish, abandon", press: "m", hint: "complete"},
		{keys: "o", desc: "open in the browser (the file, on the Files tab)", press: "o", hint: "browser"},
		{keys: "c", desc: "check out the source branch (asks where)", press: "c", hint: "checkout"},
		{keys: "r", desc: "refresh", press: "r"},
		{keys: "esc", desc: "back one level: thread, range, pane, then to the list", press: "esc", hint: "back"},
	}
	overviewKeys = []binding{
		{keys: "J / K", desc: "next / previous activity entry", press: "J", hint: "entry"},
		{keys: "enter", desc: "step into the selected comment thread", press: "enter", hint: "thread"},
		{keys: "a", desc: "add a comment on the pull request", press: "a", hint: "comment"},
		{keys: "R / s", desc: "reply / status on the selected thread", press: "R"},
		{keys: "f", desc: "choose the activity filter", press: "f", hint: "filter"},
		{keys: "/", desc: "find text; n / N (or p) next / previous match", press: "/", hint: "find"},
		{keys: "y", desc: "copy menu: URL, branch, ID, title", press: "y", hint: "copy"},
	}
	threadKeys = []binding{
		{keys: "j / k", desc: "pick a comment", hint: "comment"},
		{keys: "e / d", desc: "edit / delete your comment", press: "e", hint: "edit/delete"},
		{keys: "R", desc: "reply", press: "R", hint: "reply"},
		{keys: "s", desc: "set the thread's status", press: "s", hint: "status"},
		{keys: "y", desc: "copy the comment text or the whole thread", press: "y", hint: "copy"},
	}
	treeKeys = []binding{
		{keys: "enter / l / tab", desc: "focus the diff", press: "enter", hint: "diff"},
		{keys: "/", desc: "filter files", press: "/", hint: "filter"},
		{keys: "y", desc: "copy menu: path, web URL, name", press: "y", hint: "copy"},
	}
	diffKeys = []binding{
		{keys: "n / N", desc: "next / previous change (p is N too); at the last (first) one, again for the next (previous) file", press: "n", hint: "change"},
		{keys: "h / l", desc: "old / new side (side-by-side); h on old goes to the tree", hint: "side"},
		{keys: "tab", desc: "focus the file tree", press: "tab", hint: "tree"},
		{keys: "V / drag", desc: "select lines: a comments on them, y copies them", press: "V", hint: "select"},
		{keys: "a", desc: "comment on the line or range", press: "a", hint: "comment"},
		{keys: "enter", desc: "step into the line's thread", press: "enter", hint: "thread"},
		{keys: "R / s", desc: "reply / status on the line's thread", press: "R"},
		{keys: "y", desc: "copy the selected lines (none selected: copy menu)", press: "y", hint: "copy"},
	}
	filesKeys = []binding{
		{keys: "S", desc: "side-by-side ⇄ inline", press: "S", hint: "mode"},
		{keys: "u", desc: "compare: all changes, since last visit, updates, commits", press: "u", hint: "compare"},
		{keys: "z", desc: "hide / show the file tree", press: "z"},
	}
	diffViewKeys = []binding{
		{keys: "S", desc: "side-by-side ⇄ inline", press: "S", hint: "mode"},
		{keys: "z", desc: "hide / show the file tree", press: "z"},
		{keys: "o", desc: "open in the browser", press: "o", hint: "browser"},
		{keys: "esc", desc: "back to the repo", press: "esc", hint: "back"},
	}
	commitsKeys = []binding{
		{keys: "enter", desc: "show the commit's diff", press: "enter", hint: "diff"},
		{keys: "J / K", desc: "next / previous push", press: "J", hint: "push"},
		{keys: "/", desc: "filter commits", press: "/", hint: "filter"},
		{keys: "y", desc: "copy menu: ID, message, web URL", press: "y", hint: "copy"},
	}
	conflictsKeys = []binding{
		{keys: "enter", desc: "open the conflict in the browser", press: "enter", hint: "open"},
		{keys: "/", desc: "filter conflicts", press: "/", hint: "filter"},
		{keys: "y", desc: "copy the path", press: "y", hint: "copy"},
	}
	projectKeys = []binding{
		{keys: "1-3", desc: "Repos, Pull requests, Pipelines tab", hint: "tabs"},
		{keys: "[ / ]", desc: "previous / next tab", press: "]"},
		{keys: "enter", desc: "open: repo browser, pull request, pipeline runs", press: "enter", hint: "open"},
		{keys: "/", desc: "filter the list", press: "/", hint: "filter"},
		{keys: "y", desc: "copy menu", press: "y", hint: "copy"},
		{keys: "o", desc: "open in the browser", press: "o", hint: "browser"},
		{keys: "r", desc: "refresh", press: "r", hint: "refresh"},
		{keys: "esc", desc: "back to the Projects page", press: "esc", hint: "back"},
	}
	repoTabKeys = []binding{
		{keys: "1-3", desc: "Files, Commits, Tags tab", hint: "tabs"},
		{keys: "d", desc: "branch pane: delete the branch (asks first; never the default)", press: "d"},
	}
	repoCommitsKeys = []binding{
		{keys: "enter", desc: "show the commit's diff", press: "enter", hint: "diff"},
		{keys: "tab", desc: "branches ⇄ commits (switch branch in the branch pane)", press: "tab", hint: "pane"},
		{keys: "/", desc: "filter the loaded commits (more load as you scroll)", press: "/", hint: "filter"},
		{keys: "T", desc: "tag the commit (empty message: lightweight tag)", press: "T", hint: "tag"},
		{keys: "y", desc: "copy menu: web URL, commit ID, short ID, message", press: "y", hint: "copy"},
		{keys: "c", desc: "check out the commit, detached (asks where)", press: "c", hint: "checkout"},
		{keys: "o", desc: "open in the browser", press: "o"},
		{keys: "esc", desc: "release changes: back to the tags · else back to the repos", press: "esc", hint: "back"},
	}
	repoTagsKeys = []binding{
		{keys: "enter", desc: "release changes: commits since the previous tag", press: "enter", hint: "changes"},
		{keys: "D", desc: "one diff of everything since the previous tag", press: "D", hint: "diff"},
		{keys: "/", desc: "filter tags", press: "/", hint: "filter"},
		{keys: "d", desc: "delete the tag (asks first)", press: "d", hint: "delete"},
		{keys: "y", desc: "copy the tag name", press: "y", hint: "copy"},
		{keys: "c", desc: "check out the tag, detached (asks where)", press: "c", hint: "checkout"},
		{keys: "o", desc: "open in the browser", press: "o"},
		{keys: "esc", desc: "back to the repos", press: "esc", hint: "back"},
	}
	repoKeys = []binding{
		{keys: "tab / shift+tab", desc: "next / previous pane: branches, files, content", press: "tab", hint: "pane"},
		{keys: "h / l", desc: "collapse / expand a folder, or move between panes", hint: "fold"},
		{keys: "enter", desc: "switch branch, open folder or file", press: "enter", hint: "open"},
		{keys: "/", desc: "branches: filter · files: go to any file · content: find text", press: "/", hint: "find"},
		{keys: "n / N", desc: "next / previous match in the file (p is N too)", press: "n"},
		{keys: "M", desc: "markdown: rendered ⇄ raw", press: "M"},
		{keys: "V / drag", desc: "content: select lines", press: "V", hint: "select"},
		{keys: "y", desc: "copy the selected lines (none: copy menu for branch or file)", press: "y", hint: "copy"},
		{keys: "c", desc: "check out a branch (asks where)", press: "c", hint: "checkout"},
		{keys: "z", desc: "hide / show the branches", press: "z"},
		{keys: "o", desc: "open in the browser", press: "o"},
		{keys: "esc", desc: "side pane: back to the tree · tree: back to the repos", press: "esc", hint: "back"},
	}
	runsKeys = []binding{
		{keys: "enter", desc: "open the run", press: "enter", hint: "open"},
		{keys: "/", desc: "filter runs", press: "/", hint: "filter"},
		{keys: "y", desc: "copy menu: URL, number, branch", press: "y", hint: "copy"},
		{keys: "o", desc: "open in the browser", press: "o", hint: "browser"},
		{keys: "r", desc: "refresh", press: "r", hint: "refresh"},
		{keys: "esc", desc: "back to the pipelines", press: "esc", hint: "back"},
	}
	runKeys = []binding{
		{keys: "tab / h / l", desc: "steps ⇄ log", press: "tab", hint: "pane"},
		{keys: "/", desc: "steps: filter · log: find text; n / N (or p) next / previous", press: "/", hint: "find"},
		{keys: "G", desc: "log: jump to the end and follow new lines", press: "G", hint: "follow"},
		{keys: "V / drag", desc: "log: select lines", press: "V", hint: "select"},
		{keys: "y", desc: "log: copy the selected lines (none: current line or whole log) · steps: copy menu", press: "y", hint: "copy"},
		{keys: "o", desc: "open the run in the browser", press: "o"},
		{keys: "r", desc: "refresh", press: "r"},
		{keys: "esc", desc: "log: back to the steps · steps: back to the runs", press: "esc", hint: "back"},
	}
	globalKeys = []binding{
		{keys: "?", desc: "this help", hint: "help"},
		{keys: "q", desc: "quit (anywhere outside a text box)", hint: "quit"},
		{keys: "ctrl+c", desc: "quit, even while typing"},
		{keys: "mouse", desc: "click selects, click again opens, wheel scrolls"},
	}
)

// helpGroups lists the bindings for where the user is, most specific first.
func (m Model) helpGroups() []bindingGroup {
	switch {
	case m.detail != nil:
		return m.detail.helpGroups()
	case m.project != nil:
		return m.project.helpGroups()
	case m.page == pageProjects:
		return withCommon(bindingGroup{"Projects", projectsPageKeys}, bindingGroup{"Pages", pageKeys})
	}
	return withCommon(bindingGroup{"Dashboard", dashboardKeys}, bindingGroup{"Markers", markerLegend()}, bindingGroup{"Pages", pageKeys})
}

// withCommon appends the movement and global keys every view shares.
func withCommon(groups ...bindingGroup) []bindingGroup {
	return append(groups, bindingGroup{"Move", moveKeys}, bindingGroup{"Global", globalKeys})
}

func (d *detailModel) helpGroups() []bindingGroup {
	if d.standalone {
		pane := bindingGroup{"File tree", treeKeys}
		if d.files.pane == paneDiff {
			pane = bindingGroup{"Diff", diffKeys}
		}
		return withCommon(pane, bindingGroup{"Diff view", diffViewKeys})
	}
	var groups []bindingGroup
	switch d.tab {
	case tabOverview:
		if d.inThread {
			groups = append(groups, bindingGroup{"Comment thread", threadKeys})
		}
		groups = append(groups, bindingGroup{"Overview", overviewKeys})
	case tabFiles:
		switch {
		case d.files.inThread:
			groups = append(groups, bindingGroup{"Comment thread", threadKeys})
		case d.files.pane == paneDiff:
			groups = append(groups, bindingGroup{"Diff", diffKeys})
		default:
			groups = append(groups, bindingGroup{"File tree", treeKeys})
		}
		groups = append(groups, bindingGroup{"Files", filesKeys})
	case tabCommits:
		groups = append(groups, bindingGroup{"Commits", commitsKeys})
	case tabConflicts:
		groups = append(groups, bindingGroup{"Conflicts", conflictsKeys})
	case tabCount:
	}
	return withCommon(append(groups, bindingGroup{"Pull request", detailKeys})...)
}

func (m *projectModel) helpGroups() []bindingGroup {
	switch m.level {
	case levelRepo:
		tab := bindingGroup{"Repo", repoTabKeys}
		switch m.browser.tab {
		case repoTabCommits:
			return withCommon(bindingGroup{"Commits", repoCommitsKeys}, tab)
		case repoTabTags:
			return withCommon(bindingGroup{"Tags", repoTagsKeys}, tab)
		case repoTabFiles, repoTabCount:
		}
		return withCommon(bindingGroup{"Files", repoKeys}, tab)
	case levelRuns:
		return withCommon(bindingGroup{"Runs", runsKeys}, bindingGroup{"Project", projectKeys[:2]})
	case levelRun:
		return withCommon(bindingGroup{"Run", runKeys}, bindingGroup{"Project", projectKeys[:2]})
	case levelTabs:
	}
	return withCommon(bindingGroup{"Project", projectKeys})
}

// footer renders the hints of the given groups as the bottom help line.
func footer(groups []bindingGroup) string {
	var parts []string
	for _, g := range groups {
		for _, b := range g.bindings {
			if b.hint != "" {
				parts = append(parts, strings.ReplaceAll(b.keys, " / ", "/")+" "+b.hint)
			}
		}
	}
	return strings.Join(parts, "  ")
}

// keyMsg turns a binding's press string back into the key it stands for.
func keyMsg(press string) tea.KeyMsg {
	switch press {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}
	if k, ok := strings.CutPrefix(press, "ctrl+"); ok && len(k) == 1 {
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(k[0]-'a')}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(press)}
}
