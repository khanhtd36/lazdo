package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func TestStackedListPanes(t *testing.T) {
	head := func(s string) pickItem { return pickItem{header: true, render: func(int) string { return s }} }
	row := func(s string) pickItem { return pickItem{search: s, render: func(int) string { return s }} }
	l := &pickList{}
	l.setItems([]pickItem{head("Recent (2)"), row("a1"), row("a2"), head("Active (0)"), head("All (3)"), row("c1"), row("c2"), row("c3")})

	view := ansi.Strip(strings.Join(l.stackedView(40, 20), "\n"))
	if strings.Contains(view, "Active") {
		t.Fatalf("an empty group shows no pane:\n%s", view)
	}
	if strings.Count(view, "┌") != 2 || strings.Count(view, "└") != 2 {
		t.Fatalf("two groups, two panes:\n%s", view)
	}
	if !strings.Contains(view, "1/2") {
		t.Fatalf("the focused group shows the cursor's place:\n%s", view)
	}
	l.move(1)
	l.move(1) // past the empty group into All
	view = ansi.Strip(strings.Join(l.stackedView(40, 20), "\n"))
	if sel, _ := l.selected(); sel.search != "c1" || !strings.Contains(view, "1/3") {
		t.Fatalf("j crosses into the next group: on %q\n%s", sel.search, view)
	}
	// Lines: Recent top, a1, a2, bottom, All top, c1, c2.
	if l.stackedClick(6) || func() string { s, _ := l.selected(); return s.search }() != "c2" {
		t.Fatal("a click selects the row under it")
	}
	if !l.stackedClick(6) {
		t.Fatal("a second click activates it")
	}
	if l.stackedClick(3) || l.stackedClick(4) {
		t.Fatal("edges aren't rows")
	}
}

func TestListShowsHeadersAboveTheTopRow(t *testing.T) {
	head := func(s string) pickItem { return pickItem{header: true, render: func(int) string { return s }} }
	row := func(s string) pickItem { return pickItem{search: s, render: func(int) string { return s }} }
	items := make([]pickItem, 0, 32)
	items = append(items, head("repo"), head("src"))
	for i := range 30 {
		items = append(items, row("file"+strconv.Itoa(i)))
	}
	l := &pickList{}
	l.setItems(items)
	l.key(keyMsg("G"), 10)
	_ = l.view(40, 10)
	l.key(keyMsg("g"), 10)
	view := ansi.Strip(strings.Join(l.view(40, 10), "\n"))
	if !strings.HasPrefix(strings.TrimSpace(view), "repo") || !strings.Contains(view, "src") {
		t.Fatalf("after G then g the folders above the first file should show:\n%s", view)
	}
}

func TestPickListTintsFocusedCursorRow(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	bg := "\x1b[48;5;237m" // cursorRow

	head := func(s string) pickItem { return pickItem{header: true, render: func(int) string { return s }} }
	row := func(s string) pickItem { return pickItem{search: s, render: func(int) string { return s }} }

	l := &pickList{}
	l.setItems([]pickItem{head("Group"), row("first item"), row("second item")})

	// Focused view tints only the selected row to the full pane width.
	lines := l.view(40, 5)
	if !strings.Contains(lines[1], bg) || ansi.StringWidth(lines[1]) != 40 {
		t.Fatalf("selected row should be tinted across full width 40: width=%d line=%q", ansi.StringWidth(lines[1]), lines[1])
	}
	if strings.Contains(lines[0], bg) || strings.Contains(lines[2], bg) {
		t.Fatalf("headers and unselected rows must not be tinted: header=%q row2=%q", lines[0], lines[2])
	}

	// Unfocused lists keep only the faint marker without tinting.
	l.unfocused = true
	unfocusedLines := l.view(40, 5)
	if strings.Contains(unfocusedLines[1], bg) {
		t.Fatalf("unfocused pickList must not tint its cursor row: %q", unfocusedLines[1])
	}
	l.unfocused = false

	// Stacked view tints selected row inside the frame borders while keeping exact width.
	stacked := l.stackedView(40, 10)
	var selLine string
	for _, sl := range stacked {
		if strings.Contains(ansi.Strip(sl), "first item") {
			selLine = sl
			break
		}
	}
	if !strings.Contains(selLine, bg) || strings.HasPrefix(selLine, bg) || ansi.StringWidth(selLine) != 40 {
		t.Fatalf("stackedView selected row should be tinted inside │...│ at width 40: %q", selLine)
	}

	// Project -> 2 Pull requests tints selected PR row inside borders, leaving unselected row untinted.
	prs := []ado.PullRequest{
		makeDashPR(11, "active pr", false),
		makeDashPR(12, "draft pr", true),
	}
	m := New(ado.NewClient("org"), time.Minute)
	m.me, m.loading = ado.Identity{ID: "me"}, false
	p := newProject(m.client, ado.ProjectInfo{ID: "p1", Name: "Demo"}, prs, nil)
	p.tab = projTabPRs
	p.buildsLoaded = true
	p.lists[projTabPRs].refreshItems(p.prItems(), samePR)
	m.project = p
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 18})
	m = next.(Model)

	view := m.View()
	var pr11Line, pr12Line string
	for _, ln := range strings.Split(view, "\n") {
		plain := ansi.Strip(ln)
		if strings.Contains(plain, "active pr") {
			pr11Line = ln
		} else if strings.Contains(plain, "draft pr") {
			pr12Line = ln
		}
	}
	if !strings.Contains(pr11Line, bg) || strings.HasPrefix(pr11Line, bg) {
		t.Fatalf("Project -> PRs selected row should be tinted inside frame: %q", pr11Line)
	}
	if strings.Contains(pr12Line, bg) {
		t.Fatalf("Project -> PRs unselected row should not be tinted: %q", pr12Line)
	}
}
