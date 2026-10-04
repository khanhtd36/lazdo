package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func fakeProjectsPage() projectsPage {
	mits := ado.ProjectInfo{ID: "p1", Name: "MITS11", Description: "DAS, WebConsole"}
	dw := ado.ProjectInfo{ID: "p2", Name: "DataWatcher_Git"}
	old := ado.ProjectInfo{ID: "p3", Name: "Archive"}
	p := projectsPage{}
	p.onLoaded(projectsLoadedMsg{
		projects: []ado.ProjectInfo{old, dw, mits},
		recent:   []string{"p1"},
		repos: []ado.Repo{
			{ID: "r1", Name: "MITS11", Project: ado.Project{ID: "p1", Name: "MITS11"}, DefaultBranch: "refs/heads/develop"},
			{ID: "r2", Name: "dev-tools", Project: ado.Project{ID: "p1", Name: "MITS11"}, DefaultBranch: "refs/heads/develop"},
		},
		prs: []ado.PullRequest{
			{ID: 1, Repository: ado.Repository{Project: ado.Project{ID: "p2"}}},
			{ID: 2, Repository: ado.Repository{Project: ado.Project{ID: "p1"}}},
		},
	})
	return p
}

func listText(l *pickList) string {
	return ansi.Strip(strings.Join(l.view(120, 20), "\n"))
}

func TestProjectsPageGroups(t *testing.T) {
	p := fakeProjectsPage()
	text := listText(&p.list)
	recent, active, all := strings.Index(text, "Recent (1)"), strings.Index(text, "Active (1)"), strings.Index(text, "All projects (3)")
	if recent < 0 || active < recent || all < active {
		t.Fatalf("groups out of order:\n%s", text)
	}
	if strings.Contains(text, "dev-tools") {
		t.Fatal("repos should only show while searching")
	}
	if it, _ := p.list.selected(); it.value.(ado.ProjectInfo).Name != "MITS11" {
		t.Fatal("cursor should start on the first project, not a header")
	}
}

func TestProjectsSearchFindsRepoAndOpensBranches(t *testing.T) {
	p := fakeProjectsPage()
	for _, k := range []string{"/", "d", "e", "v", "-", "t"} {
		p.list.key(keyMsg(k), 20)
	}
	it, ok := p.list.selected()
	if r, isRepo := it.value.(ado.Repo); !ok || !isRepo || r.Name != "dev-tools" {
		t.Fatalf("search should select the dev-tools repo, got %+v", it.value)
	}
	open, cmd := p.openSelected(ado.NewClient("org"))
	if open == nil || cmd == nil || open.level != levelRepo || open.repo.Name != "dev-tools" {
		t.Fatalf("opening a found repo should land on its browser: %+v", open)
	}
}

func TestProjectMainRepoFirst(t *testing.T) {
	p := fakeProjectsPage()
	m := p.newProject(ado.NewClient("org"), ado.ProjectInfo{ID: "p1", Name: "MITS11"})
	if m.repos[0].Name != "MITS11" || m.repos[1].Name != "dev-tools" {
		t.Fatalf("main repo first, then A-Z: %s, %s", m.repos[0].Name, m.repos[1].Name)
	}
}

func TestPickListSkipsHeadersAndClicks(t *testing.T) {
	var l pickList
	row := func(s string) pickItem {
		return pickItem{search: s, value: s, render: func(int) string { return s }}
	}
	head := pickItem{header: true, render: func(int) string { return "H" }}
	l.setItems([]pickItem{head, row("a"), head, row("b")})
	l.move(1)
	if it, _ := l.selected(); it.value != "b" {
		t.Fatalf("j from a should skip the header to b, got %v", it.value)
	}
	if l.click(1) || l.click(1) != true {
		t.Fatal("first click selects a, second click activates it")
	}
}

func TestPagesSwitchAndProjectEsc(t *testing.T) {
	m := New(ado.NewClient("org"), time.Minute)
	m.width, m.height, m.loading = 120, 20, false
	next, cmd := m.Update(keyMsg("2"))
	if next.(Model).page != pageProjects || cmd == nil {
		t.Fatal("2 should switch to Projects and start loading")
	}
	m = next.(Model)
	m.projects = fakeProjectsPage()
	next, _ = m.Update(keyMsg("enter"))
	m = next.(Model)
	if m.project == nil || m.project.project.Name != "MITS11" {
		t.Fatal("enter should open the selected project")
	}
	// / then 1 types into the filter instead of switching pages.
	next, _ = m.Update(keyMsg("/"))
	next, _ = next.Update(keyMsg("1"))
	if p := next.(Model).project; p == nil || p.tab != projTabRepos || p.lists[projTabRepos].filter != "1" {
		t.Fatal("1 while filtering should be typed, not switch pages")
	}
	next, _ = next.Update(keyMsg("esc")) // clears the filter
	next, _ = next.Update(keyMsg("esc")) // closes the project
	if next.(Model).project != nil {
		t.Fatal("esc at the top of a project should go back to the Projects page")
	}
}

func TestProjectTabKeysLeavePipelineRuns(t *testing.T) {
	p := fakeProjectsPage()
	client := ado.NewClient("org")
	for _, level := range []projectLevel{levelRuns, levelRun} {
		m := p.newProject(client, ado.ProjectInfo{ID: "p1", Name: "MITS11"})
		m.width, m.height = 120, 20
		m.tab, m.level = projTabPipelines, level
		if level == levelRun {
			m.run = newRunView(client, m.project, ado.Run{ID: 1})
		}
		m.key(keyMsg("1"))
		if m.level != levelTabs || m.tab != projTabRepos || m.run != nil {
			t.Fatalf("1 at level %d should switch to Repos at the tabs level, got level %d tab %d", level, m.level, m.tab)
		}
	}
}

func TestRunTreeFlattensPhases(t *testing.T) {
	v := newRunView(ado.NewClient("org"), ado.ProjectInfo{}, ado.Run{ID: 1, Status: "completed"})
	v.update(runLoadedMsg{runID: 1, run: ado.Run{ID: 1, Status: "completed"}, records: []ado.TimelineRecord{
		{ID: "s", Type: "Stage", Name: "__default"},
		{ID: "ph", ParentID: "s", Type: "Phase", Name: "Build"},
		{ID: "j", ParentID: "ph", Type: "Job", Name: "Build linux", Order: 1},
		{ID: "t1", ParentID: "j", Type: "Task", Name: "Checkout", Order: 1, Result: "succeeded", Log: &struct {
			ID int `json:"id"`
		}{ID: 5}},
		{ID: "t2", ParentID: "j", Type: "Task", Name: "Compile", Order: 2, Result: "failed", Log: &struct {
			ID int `json:"id"`
		}{ID: 6}},
	}})
	text := listText(&v.tree)
	if strings.Contains(text, "__default") || strings.Contains(text, "Build\n") {
		t.Fatalf("stage __default and phases should be flattened:\n%s", text)
	}
	if v.logID != 6 {
		t.Fatalf("the failed step's log should open first, got log %d", v.logID)
	}
}
