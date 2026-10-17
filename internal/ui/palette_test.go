package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func sendKeys(m Model, keys ...string) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(keyMsg(k))
		m = next.(Model)
	}
	return m, cmd
}

// typeText types each rune of s.
func typeText(m Model, s string) Model {
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	return m
}

func openedPalette(t *testing.T, m Model) *paletteModal {
	t.Helper()
	p, ok := m.modal.(*paletteModal)
	if !ok {
		t.Fatalf("want the palette open, got %T", m.modal)
	}
	return p
}

func groupTitles(p *paletteModal) []string {
	out := make([]string, 0, len(p.groups))
	for _, g := range p.groups {
		out = append(out, ansi.Strip(g.title))
	}
	return out
}

// onPR puts the dashboard cursor on !7.
func onPR(t *testing.T, m Model) Model {
	t.Helper()
	const id = 7
	for i, r := range m.rows() {
		if r.pr != nil && r.pr.ID == id {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no row for !%d", id)
	return m
}

func TestPaletteOnADashboardRow(t *testing.T) {
	m := onPR(t, dashboardWith(t, 7))
	m, _ = sendKeys(m, ":")
	got := strings.Join(groupTitles(openedPalette(t, m)), " | ")
	want := "Pull request !7 pr | Reviewers | Repo  | Project  | Pull requests page | Organization org"
	if got != want {
		t.Fatalf("groups nearest first:\n got %s\nwant %s", got, want)
	}
}

func TestPaletteRunsAfterTheDashboardOpensThePR(t *testing.T) {
	m := onPR(t, dashboardWith(t, 7))
	m, _ = sendKeys(m, ":")
	m = typeText(m, "approve with")
	if c, ok := openedPalette(t, m).selected(); !ok || c.id != "Approve with suggestions" {
		t.Fatalf("filter picks %+v", c)
	}
	m, cmd := sendKeys(m, "enter")
	if m.modal != nil {
		t.Fatal("enter closes the palette")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.detail == nil || m.detail.pending != "Approve with suggestions" {
		t.Fatalf("the pull request opens with the command waiting: %+v", m.detail)
	}
	run := m.detail.update(detailLoadedMsg{prID: 7, d: &ado.PRDetail{PullRequest: m.detail.pr, Status: "active"}})
	if _, ok := run().(paletteRunMsg); !ok || m.detail.pending != "" {
		t.Fatal("once loaded, the command runs")
	}
}

func TestPaletteIsATextBox(t *testing.T) {
	m := onPR(t, dashboardWith(t, 7))
	m, _ = sendKeys(m, ":", "q")
	if p := openedPalette(t, m); p.filter != "q" {
		t.Fatalf("q types into the filter, got %q", p.filter)
	}
	m, _ = sendKeys(m, "esc")
	if m.modal != nil {
		t.Fatal("esc closes it")
	}
	m, _ = sendKeys(m, "/", ":")
	if m.modal != nil || m.filter != ":" {
		t.Fatalf("in a filter, : is typed (filter %q)", m.filter)
	}
}

func TestPaletteInThePR(t *testing.T) {
	m := dashboardWith(t, 7)
	m.detail = fakeDetail(t, 140)
	m.detail.data.Status = "active"
	m.detail.threadSel = 0 // the thread "please fix"
	m.detail.rebuildOverview()
	m, _ = sendKeys(m, ":")
	p := openedPalette(t, m)
	titles := groupTitles(p)
	if !strings.HasPrefix(titles[0], "Thread on the pull request") || titles[1] != "Overview" || !strings.HasPrefix(titles[2], "Pull request !7") {
		t.Fatalf("the thread, then the tab, then the pull request: %v", titles)
	}
	labels := make([]string, 0, len(p.groups[0].commands))
	for _, c := range p.groups[0].commands {
		labels = append(labels, ansi.Strip(c.label))
	}
	all := strings.Join(labels, " | ")
	for _, want := range []string{"Reply…", "Set status: Resolved", "Step into the thread", "Edit my comment “please fix”…"} {
		if !strings.Contains(all, want) {
			t.Errorf("thread commands miss %q: %s", want, all)
		}
	}
}

func TestPaletteBlockedCommandsSinkAndSayWhy(t *testing.T) {
	m := dashboardWith(t, 7)
	m.detail = fakeDetail(t, 140)
	m.detail.data.IsDraft = true
	m, _ = sendKeys(m, ":")
	m = typeText(m, "co")
	blocked := 0
	for _, r := range openedPalette(t, m).rows() {
		switch {
		case r.cmd.disabled != "":
			blocked++
		case blocked > 0:
			t.Fatalf("%q can run but comes after a blocked command", r.cmd.label)
		}
	}
	if blocked == 0 {
		t.Fatal("Complete on a draft can't run")
	}
	if view := ansi.Strip(m.modal.view(m.width)); !strings.Contains(view, "publish the draft before completing it") {
		t.Fatalf("a blocked command says why:\n%s", view)
	}
}

func TestPaletteJumpToRepoAndBack(t *testing.T) {
	m := dashboardWith(t, 7)
	m.projects.loaded = true
	m.detail = fakeDetail(t, 140)
	d := m.detail
	m, _ = sendKeys(m, ":")
	m = typeText(m, "open the repo")
	if c, _ := openedPalette(t, m).selected(); c.id != "Open repo" {
		t.Fatalf("filter picks %q", c.label)
	}
	m, cmd := sendKeys(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.detail != nil || m.project == nil || m.project.level != levelRepo {
		t.Fatalf("the repo browser opens on top (detail %v, project %v)", m.detail, m.project)
	}
	m, _ = sendKeys(m, "esc")
	if m.detail != d || m.project != nil {
		t.Fatalf("esc comes back to the pull request (detail %v, project %v)", m.detail, m.project)
	}
}

func TestPaletteOnAFolder(t *testing.T) {
	m := dashboardWith(t, 7)
	m.detail = fakeFolders(t)
	press(m.detail, "j", "j") // /src/lib
	m, _ = sendKeys(m, ":")
	if titles := groupTitles(openedPalette(t, m)); titles[0] != "Folder /src/lib" || titles[1] != "Files" {
		t.Fatalf("a folder's commands first: %v", titles)
	}
	m = typeText(m, "close the folder")
	m, cmd := sendKeys(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !m.detail.files.closed["/src/lib"] {
		t.Fatal("the command closed the folder")
	}
}

func TestPaletteSizeFollowsTheScreenOnly(t *testing.T) {
	m := onPR(t, dashboardWith(t, 7))
	m, _ = sendKeys(m, ":")
	size := func() (int, int) {
		box := m.modal.view(m.width)
		return strings.Count(box, "\n") + 1, ansi.StringWidth(firstLine(box))
	}
	h, w := size()
	for _, typed := range []string{"a", "pprove", "zzzz"} {
		m = typeText(m, typed)
		if gh, gw := size(); gh != h || gw != w {
			t.Fatalf("after typing %q the box is %dx%d, was %dx%d", typed, gw, gh, w, h)
		}
	}
	m.width, m.height = 200, 60
	m, _ = sendKeys(m, "esc", ":")
	if gh, gw := size(); gh <= h || gw < w {
		t.Fatalf("a taller screen gets a taller box (width capped): %dx%d, was %dx%d", gw, gh, w, h)
	}
}

func TestPaletteCommandTextComesFirst(t *testing.T) {
	m := dashboardWith(t, 7)
	m.detail = fakeDetail(t, 140)
	m.detail.pr.Title = strings.Repeat("a very long pull request title ", 5)
	m.detail.data.PullRequest = m.detail.pr
	m, _ = sendKeys(m, ":")
	m = typeText(m, "approve with")
	view := ansi.Strip(m.modal.view(m.width))
	if !strings.Contains(view, "Approve with suggestions") {
		t.Fatalf("the command shows whole, the group gives way:\n%s", view)
	}
	if !strings.Contains(view, "Pull request !7  v") || strings.Contains(view, "very long") {
		t.Fatalf("beside a match, the pull request's short name and the key:\n%s", view)
	}
}
