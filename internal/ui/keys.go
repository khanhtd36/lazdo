package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// binding documents one shortcut. press is the key the help popup replays
// when the user runs the entry; empty for entries that only describe keys.
type binding struct {
	keys  string
	desc  string
	press string
}

type bindingGroup struct {
	title    string
	bindings []binding
}

var (
	dashboardKeys = []binding{
		{"j / k", "move down / up", ""},
		{"tab / shift+tab", "next / previous section", "tab"},
		{"enter", "open the pull request, or collapse a section", "enter"},
		{"o", "open the pull request in the browser", "o"},
		{"y", "copy the pull request URL", "y"},
		{"c", "check out the source branch", "c"},
		{"r", "refresh", "r"},
		{"g / G", "first / last row", ""},
		{"q", "quit", "q"},
	}
	detailKeys = []binding{
		{"v", "vote: approve, suggestions, wait for author, reject, reset", "v"},
		{"m", "complete, auto-complete, draft/publish, abandon", "m"},
		{"1-4", "Overview, Files, Commits, Conflicts tab", ""},
		{"[ / ]", "previous / next tab", "]"},
		{"o", "open in the browser (the file, on the Files tab)", "o"},
		{"y", "copy the pull request URL", "y"},
		{"c", "check out the source branch", "c"},
		{"r", "refresh", "r"},
		{"esc", "back out: thread, range, pane, then to the list", "esc"},
	}
	overviewKeys = []binding{
		{"j / k", "scroll", ""},
		{"J / K", "next / previous activity entry", "J"},
		{"enter", "step into the selected comment thread", "enter"},
		{"f", "cycle the activity filter", "f"},
		{"n", "new comment on the pull request", "n"},
		{"R", "reply to the selected thread", "R"},
		{"s", "set the selected thread's status", "s"},
	}
	threadKeys = []binding{
		{"j / k", "pick a comment", ""},
		{"e", "edit your comment", "e"},
		{"d", "delete your comment", "d"},
		{"R", "reply", "R"},
		{"s", "set the thread's status", "s"},
	}
	treeKeys = []binding{
		{"j / k", "next / previous file", ""},
		{"enter / l / tab", "focus the diff", "enter"},
	}
	diffKeys = []binding{
		{"j / k", "next / previous line", ""},
		{"ctrl+d / ctrl+u", "half page down / up", "ctrl+d"},
		{"n / N (or p)", "next / previous change", "n"},
		{"h / l", "old / new side (side-by-side); h on old goes to the tree", ""},
		{"V", "start or clear a line range", "V"},
		{"a", "comment on the line or range", "a"},
		{"enter", "step into the line's thread", "enter"},
		{"R / s", "reply / status on the line's thread", ""},
		{"tab", "focus the file tree", "tab"},
	}
	filesKeys = []binding{
		{"S", "side-by-side ⇄ inline", "S"},
		{"u", "compare: all changes, since last visit, updates", "u"},
		{"z", "hide / show the file tree", "z"},
	}
	listKeys = []binding{
		{"j / k", "move", ""},
		{"enter", "open (a commit shows its diff)", "enter"},
	}
	pageKeys = []binding{
		{"1 / 2", "Pull requests / Projects page", ""},
		{"[ / ]", "previous / next page", "]"},
	}
	projectsPageKeys = []binding{
		{"j / k", "move", ""},
		{"enter", "open the project (or a found repo's branches)", "enter"},
		{"/", "search projects and every repo", "/"},
		{"o", "open in the browser", "o"},
		{"y / Y", "copy a found repo's https / ssh URL", "y"},
		{"r", "refresh", "r"},
	}
	projectKeys = []binding{
		{"1-3", "Pull requests, Repos, Pipelines tab", ""},
		{"[ / ]", "previous / next tab", "]"},
		{"enter", "open: pull request, repo's branches, pipeline's runs", "enter"},
		{"/", "filter the list", "/"},
		{"y / Y", "copy the repo's https / ssh URL (or the PR URL)", "y"},
		{"o", "open in the browser", "o"},
		{"r", "refresh", "r"},
		{"esc", "back to the Projects page", "esc"},
	}
	branchKeys = []binding{
		{"y", "copy the branch name", "y"},
		{"Y", "copy the repo's ssh URL", "Y"},
		{"c", "check out the branch (inside a clone of the repo)", "c"},
		{"o", "open the branch in the browser", "o"},
		{"/", "filter branches", "/"},
		{"esc", "back to the repos", "esc"},
	}
	runsKeys = []binding{
		{"enter", "open the run", "enter"},
		{"o", "open in the browser", "o"},
		{"/", "filter runs", "/"},
		{"esc", "back to the pipelines", "esc"},
	}
	runKeys = []binding{
		{"j / k", "move (steps) or scroll (log)", ""},
		{"tab / h / l", "steps ⇄ log", "tab"},
		{"G", "jump to the end and follow new lines", "G"},
		{"g", "top of the log", "g"},
		{"o", "open the run in the browser", "o"},
		{"esc", "back to the runs", "esc"},
	}
	globalKeys = []binding{
		{"?", "this help", ""},
		{"ctrl+c", "quit", ""},
		{"mouse", "click selects, click again opens, wheel scrolls", ""},
	}
)

// helpGroups lists the bindings for where the user is, most specific first.
func (m Model) helpGroups() []bindingGroup {
	d := m.detail
	if d == nil {
		return m.pageHelpGroups()
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
		groups = append(groups, bindingGroup{"Commits", listKeys})
	case tabConflicts:
		groups = append(groups, bindingGroup{"Conflicts", listKeys})
	case tabCount:
	}
	return append(groups, bindingGroup{"Pull request", detailKeys}, bindingGroup{"Global", globalKeys})
}

func (m Model) pageHelpGroups() []bindingGroup {
	global := bindingGroup{"Global", globalKeys}
	if p := m.project; p != nil {
		switch p.level {
		case levelBranches:
			return []bindingGroup{{"Branches", branchKeys}, global}
		case levelRuns:
			return []bindingGroup{{"Runs", runsKeys}, global}
		case levelRun:
			return []bindingGroup{{"Run", runKeys}, global}
		case levelTabs:
		}
		return []bindingGroup{{"Project", projectKeys}, global}
	}
	if m.page == pageProjects {
		return []bindingGroup{{"Projects", projectsPageKeys}, {"Pages", pageKeys}, global}
	}
	return []bindingGroup{{"Dashboard", dashboardKeys}, {"Pages", pageKeys}, global}
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
