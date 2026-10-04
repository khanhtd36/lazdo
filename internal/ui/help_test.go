package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func typeKeys(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		m, _ = m.Update(keyMsg(k))
	}
	return m
}

func TestKeyMsgRoundTrip(t *testing.T) {
	for _, k := range []string{"enter", "esc", "tab", "shift+tab", "ctrl+d", "ctrl+u", "J", "?", "]"} {
		if got := keyMsg(k).String(); got != k {
			t.Errorf("keyMsg(%q).String() = %q", k, got)
		}
	}
}

func TestHelpOpensForContextAndScrolls(t *testing.T) {
	m := New(ado.NewClient("org"), 0)
	m.width, m.height, m.loading = 120, 20, false
	next := typeKeys(m, "?")
	view := ansi.Strip(next.View())
	if !strings.Contains(view, "Dashboard") || !strings.Contains(view, "check out the source branch") {
		t.Fatalf("dashboard help missing:\n%s", view)
	}
	h := next.(Model).help
	first := h.cursor
	next = typeKeys(next, "j", "j")
	if next.(Model).help.cursor != first+2 {
		t.Fatalf("j should move the cursor: %d -> %d", first, next.(Model).help.cursor)
	}
	// Scroll past the bottom of a short screen; the cursor stays visible.
	next = typeKeys(next, "G")
	h = next.(Model).help
	if h.cursor < h.offset || h.cursor >= h.offset+h.listHeight() {
		t.Fatalf("cursor %d outside view [%d,%d)", h.cursor, h.offset, h.offset+h.listHeight())
	}
	if next = typeKeys(next, "esc"); next.(Model).help != nil {
		t.Fatal("esc should close help")
	}
}

func TestHelpFuzzySearchAndRun(t *testing.T) {
	d := fakeDetail(t, 140)
	m := Model{client: d.client, detail: d, width: 140, height: 30, collapsed: map[ado.SectionKind]bool{}}
	next := typeKeys(m, "?", "/", "c", "m", "p", "l")
	h := next.(Model).help
	rows := h.rows()
	if len(rows) == 0 || !strings.Contains(rows[0].binding.desc, "complete") {
		t.Fatalf("fuzzy 'cmpl' should rank a complete binding first, got %+v", rows)
	}
	// enter keeps the filter, a second enter runs the binding: the complete
	// menu opens in the detail view.
	next = typeKeys(next, "enter", "enter")
	if next.(Model).help != nil {
		t.Fatal("running a binding should close help")
	}
	if _, ok := next.(Model).detail.modal.(*menuModal); !ok {
		t.Fatalf("running m should open the complete menu, got %T", next.(Model).detail.modal)
	}
}

func TestHelpFollowsContext(t *testing.T) {
	d := fakeFiles(t, 200)
	m := Model{client: d.client, detail: d, width: 200, height: 40}
	groups := m.helpGroups()
	if groups[0].title != "Diff" {
		t.Fatalf("first group = %q, want Diff", groups[0].title)
	}
	d.files.pane = paneTree
	if m.helpGroups()[0].title != "File tree" {
		t.Fatal("tree pane should list file tree keys first")
	}
}
