package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
