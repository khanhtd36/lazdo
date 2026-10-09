package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// views builds one root Model per view the keymap covers.
func views(t *testing.T) map[string]Model {
	t.Helper()
	base := func() Model {
		m := New(ado.NewClient("org"), time.Minute)
		m.width, m.height, m.loading = 160, 30, false
		return m
	}
	out := map[string]Model{}

	dash := base()
	me := ado.Identity{ID: "me"}
	prs := make([]ado.PullRequest, 0, 30)
	for i := range 30 {
		prs = append(prs, ado.PullRequest{ID: i + 1, Title: fmt.Sprintf("pr %d", i), Reviewers: []ado.Reviewer{{Identity: me}}})
	}
	dash.me, dash.sections = me, ado.Classify("me", prs, nil)
	out["dashboard"] = dash

	projects := base()
	projects.page = pageProjects
	projects.projects = fakeProjectsPage()
	out["projects page"] = projects

	for _, tab := range []detailTab{tabOverview, tabFiles, tabCommits, tabConflicts} {
		m := base()
		m.detail = fakeDetail(t, 160)
		m.detail.tab = tab
		if tab == tabFiles {
			m.detail = fakeFiles(t, 160)
			m.detail.files.pane = paneTree
		}
		out["detail "+tab.title()] = m
	}
	diff := base()
	diff.detail = fakeFiles(t, 160)
	out["detail diff"] = diff

	pp := fakeProjectsPage()
	proj := base()
	proj.project = pp.newProject(proj.client, ado.ProjectInfo{ID: "p1", Name: "MITS11"})
	proj.project.width, proj.project.height = 160, 30
	out["project"] = proj

	repo := base()
	repo.project = pp.newProject(repo.client, ado.ProjectInfo{ID: "p1", Name: "MITS11"})
	repo.project.width, repo.project.height = 160, 30
	repo.project.level, repo.project.browser = levelRepo, fakeBrowser(t)
	out["repo browser"] = repo

	run := base()
	run.project = pp.newProject(run.client, ado.ProjectInfo{ID: "p1", Name: "MITS11"})
	run.project.width, run.project.height = 160, 30
	run.project.level, run.project.run = levelRun, newRunView(run.client, ado.ProjectInfo{}, ado.Run{ID: 1, Status: "completed"})
	out["run"] = run

	menu := base()
	menu.modal = newCopyMenu(copyMenuMsg{title: "x", items: []copyItem{{"A", "a"}, {"B", "b"}}})
	out["copy menu"] = menu
	return out
}

func TestQQuitsOutsideTextInputs(t *testing.T) {
	for name, m := range views(t) {
		if _, cmd := m.Update(keyMsg("q")); !isQuit(cmd) {
			t.Errorf("%s: q should quit", name)
		}
	}
	typing := map[string]func(Model) Model{
		"dashboard filter": func(m Model) Model { m.filterTyping = true; return m },
		"editor": func(m Model) Model {
			m.detail = fakeDetail(t, 160)
			m.detail.modal = m.detail.newCommentEditor()
			return m
		},
		"checkout path": func(m Model) Model {
			m.modal = newCheckoutModal(checkoutRequestMsg{repo: "r", branch: "b"}, []string{"x"}, 120)
			return m
		},
		"help filter": func(m Model) Model { m.help = newHelp(m.helpGroups(), 120, 20); m.help.typing = true; return m },
	}
	for name, f := range typing {
		m := f(views(t)["dashboard"])
		if _, cmd := m.Update(keyMsg("q")); isQuit(cmd) {
			t.Errorf("%s: q must be typed, not quit", name)
		}
	}
}

func TestHalfPageMovesEveryList(t *testing.T) {
	m := views(t)["dashboard"]
	next, _ := m.Update(keyMsg("ctrl+d"))
	if next.(Model).cursor <= m.cursor {
		t.Error("dashboard: ctrl+d should move down")
	}
	next, _ = next.Update(keyMsg("ctrl+u"))
	if next.(Model).cursor != m.cursor {
		t.Error("dashboard: ctrl+u should come back")
	}

	var l pickList
	items := make([]pickItem, 40)
	for i := range items {
		items[i] = pickItem{search: fmt.Sprint(i), render: func(int) string { return "" }}
	}
	l.setItems(items)
	l.key(keyMsg("ctrl+d"), 20)
	if l.cursor != 10 {
		t.Errorf("list: ctrl+d moved to %d, want 10", l.cursor)
	}

	menu := &menuModal{items: make([]menuItem, 10)}
	menu.update(keyMsg("G"))
	menu.update(keyMsg("j"))
	if menu.cursor != 9 {
		t.Errorf("menus stop at the end instead of wrapping, cursor %d", menu.cursor)
	}
}

func TestDashboardTabsMovePanesAndArrowsMoveItems(t *testing.T) {
	m := views(t)["dashboard"]
	if m.dashboardPane != 0 {
		t.Fatalf("initial dashboard pane = %d, want 0", m.dashboardPane)
	}
	next, _ := m.Update(keyMsg("tab"))
	m = next.(Model)
	if m.dashboardPane != 1 {
		t.Fatalf("tab moved to pane %d, want 1", m.dashboardPane)
	}
	next, _ = m.Update(keyMsg("shift+tab"))
	m = next.(Model)
	if m.dashboardPane != 0 {
		t.Fatalf("shift+tab moved to pane %d, want 0", m.dashboardPane)
	}
	next, _ = m.Update(keyMsg("down"))
	m = next.(Model)
	if m.paneCursors[0] != 1 {
		t.Fatalf("down moved item cursor to %d, want 1", m.paneCursors[0])
	}
}

func TestFootersComeFromTheKeyTable(t *testing.T) {
	for name, m := range views(t) {
		if m.modal != nil {
			continue
		}
		f := footer(m.helpGroups())
		if !strings.Contains(f, "? help") || !strings.Contains(f, "q quit") {
			t.Errorf("%s: footer lacks the global hints: %q", name, f)
		}
		for _, g := range m.helpGroups() {
			for _, b := range g.bindings {
				if b.hint != "" && !strings.Contains(f, b.hint) {
					t.Errorf("%s: footer misses %q", name, b.hint)
				}
			}
		}
	}
}

func TestKeyBugsStayFixed(t *testing.T) {
	// 1. Typing a repo-browser filter: z is a letter, not "hide branches".
	b := fakeBrowser(t)
	b.pane = paneBranches
	b.key(keyMsg("/"), 160, 20)
	b.key(keyMsg("z"), 160, 20)
	if b.hideBranches || b.branches.filter != "z" {
		t.Errorf("repo filter swallowed z: hidden=%v filter=%q", b.hideBranches, b.branches.filter)
	}

	// 2. Typing a run-steps filter: l is a letter, not "focus log".
	v := newRunView(ado.NewClient("org"), ado.ProjectInfo{}, ado.Run{ID: 1})
	v.tree.setItems([]pickItem{{search: "lint", render: func(int) string { return "lint" }}})
	v.key(keyMsg("/"), 20)
	v.key(keyMsg("l"), 20)
	if v.logPane || v.tree.filter != "l" {
		t.Errorf("run filter swallowed l: logPane=%v filter=%q", v.logPane, v.tree.filter)
	}

	// 3. A thread entered on Overview doesn't eat esc on another tab.
	d := fakeDetail(t, 160)
	press(d, "enter")
	d.tab = tabCommits
	if !d.closeRequested(keyMsg("esc")) {
		t.Error("esc on Commits should go back, not leave a hidden Overview thread")
	}

	// 4. Overview scrolls with ctrl+d but ignores keys it doesn't bind
	// (u, b and space used to scroll through the viewport).
	d = fakeDetail(t, 60)
	d.resize(60, 10) // a short screen, so the Overview can scroll
	press(d, "ctrl+d")
	at := d.vp.YOffset
	if at == 0 {
		t.Fatal("ctrl+d should scroll the Overview")
	}
	press(d, "u", "b", " ")
	if d.vp.YOffset != at {
		t.Error("u/b/space must not scroll the Overview")
	}
}

func TestCommitsCursorSkipsPushHeaders(t *testing.T) {
	d := fakeDetail(t, 160)
	d.tab = tabCommits
	l := &d.lists[tabCommits]
	for range 5 {
		if it, ok := l.selected(); ok && it.header {
			t.Fatal("cursor rests on a push header")
		}
		press(d, "j")
	}
}
