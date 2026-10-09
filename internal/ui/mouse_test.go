package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func wheelDown(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
}

// findRow returns the screen row and column where text first appears.
func findRow(t *testing.T, view, text string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(view), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, ansi.Strip(view))
	return 0, 0
}

func TestDashboardClickSelectsThenOpens(t *testing.T) {
	m := New(ado.NewClient("org"), 0)
	m.width, m.height, m.loading = 120, 20, false
	pr := ado.PullRequest{ID: 42, Title: "fix: click me", Reviewers: []ado.Reviewer{{Identity: ado.Identity{ID: "me"}}}}
	m.me = ado.Identity{ID: "me"}
	m.sections = ado.Classify("me", []ado.PullRequest{pr}, nil)

	x, y := findRow(t, m.View(), "fix: click me")
	next, _ := m.Update(click(x, y))
	m = next.(Model)
	if m.detail != nil || m.cursor != 1 {
		t.Fatalf("first click: detail open=%v cursor=%d", m.detail != nil, m.cursor)
	}
	next, _ = m.Update(click(x, y))
	if next.(Model).detail == nil {
		t.Fatal("second click on the selected PR should open it")
	}

	_, hy := findRow(t, m.View(), "Wait for approval")
	next, _ = m.Update(click(4, hy))
	if !next.(Model).collapsed[ado.SectionNeedsReview] {
		t.Fatal("clicking a section header should collapse it")
	}
}

func TestDetailClickTabsButtonsAndMenu(t *testing.T) {
	d := fakeDetail(t, 140)
	x, y := findRow(t, d.view(), "3 Commits")
	d.onMouse(click(x+1, y))
	if d.tab != tabCommits {
		t.Fatalf("tab = %v, want Commits", d.tab)
	}

	x, y = findRow(t, d.view(), "v Approve")
	d.onMouse(click(x+1, y))
	menu, ok := d.modal.(*menuModal)
	if !ok {
		t.Fatalf("vote button opened %T", d.modal)
	}
	x, y = findRow(t, d.view(), "Wait for author")
	d.onMouse(click(x, y))
	if d.modal != nil || menu.cursor != 2 {
		t.Fatalf("clicking a menu item should run it: modal=%T cursor=%d", d.modal, menu.cursor)
	}

	_, y = findRow(t, d.view(), "3 Commits")
	d.onMouse(click(findTitleX(t, d, "m Set auto-complete"), 0))
	if _, ok := d.modal.(*menuModal); !ok {
		t.Fatalf("complete button opened %T", d.modal)
	}
	d.onMouse(click(0, y+5)) // outside the popup
	if d.modal != nil {
		t.Fatal("click outside a menu should close it")
	}
}

func findTitleX(t *testing.T, d *detailModel, text string) int {
	t.Helper()
	x, y := findRow(t, d.view(), text)
	if y != 0 {
		t.Fatalf("%q on row %d, want title row", text, y)
	}
	return x + 1
}

func TestOverviewClickSelectsThenEntersThread(t *testing.T) {
	d := fakeDetail(t, 140)
	x, y := findRow(t, d.view(), "old note")
	d.onMouse(click(x, y))
	e, ok := d.selectedEntry()
	if !ok || e.thread == nil || e.thread.ID != 2 || d.inThread {
		t.Fatalf("first click should select thread 2 without entering it")
	}
	d.onMouse(click(x, y))
	if !d.inThread {
		t.Fatal("second click should step into the thread")
	}
}

func TestFilesClickTreeAndDiffSide(t *testing.T) {
	d := fakeFiles(t, 200)
	d.files.pane = paneTree
	x, y := findRow(t, d.view(), "func B() {}")
	d.onMouse(click(x, y))
	if d.files.pane != paneDiff || d.files.side != ado.SideRight {
		t.Fatalf("click on new code: pane=%v side=%v", d.files.pane, d.files.side)
	}
	if l := d.diffLines()[d.files.cursor]; l.right != 4 {
		t.Fatalf("cursor on new line %d, want 4", l.right)
	}
	x, y = findRow(t, d.view(), "func A() {}")
	d.onMouse(click(x, y))
	if d.files.side != ado.SideLeft {
		t.Fatal("click on old code should pick the old side")
	}
	x, y = findRow(t, d.view(), "why?")
	d.onMouse(click(x, y))
	if !d.files.inThread {
		t.Fatal("click on an inline thread should step into it")
	}
	d.onMouse(wheelDown(x, y))
	if d.files.inThread {
		t.Fatal("wheel should move the cursor out of the thread")
	}
}
