package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// fakeFolders opens the Files tree on:
//
//	▾ docs
//	    readme.md
//	▾ src
//	  ▾ lib
//	      b.go
//	      c.go
//	    a.go
func fakeFolders(t *testing.T) *detailModel {
	t.Helper()
	d := fakeDetail(t, 160)
	d.data.Pushes = []ado.Push{{ID: 1}}
	d.tab = tabFiles
	d.files.cmp = comparison{label: "All changes", target: 1}
	changes := make([]ado.Change, 0, 4)
	for _, p := range []string{"/src/a.go", "/src/lib/b.go", "/src/lib/c.go", "/docs/readme.md"} {
		changes = append(changes, ado.Change{ChangeType: "edit", Item: ado.ChangeItem{Path: p}})
	}
	d.onFilesLoaded(filesLoadedMsg{key: d.files.cmp.key(), changes: changes})
	d.files.pane = paneTree
	return d
}

func treeAt(d *detailModel) string { return d.selectedPath() }

func TestFolderRowsAreSelectable(t *testing.T) {
	d := fakeFolders(t)
	if got := treeAt(d); got != "/docs/readme.md" {
		t.Fatalf("the tree opens on the first file, got %q", got)
	}
	press(d, "k")
	if got := treeAt(d); got != "/docs" {
		t.Fatalf("k from the first file lands on its folder, got %q", got)
	}
	press(d, "j", "j", "j")
	if got := treeAt(d); got != "/src/lib" {
		t.Fatalf("j walks folders and files, got %q", got)
	}
}

func TestFolderOpenClose(t *testing.T) {
	d := fakeFolders(t)
	press(d, "j", "enter") // on /src: close it
	if !d.files.closed["/src"] {
		t.Fatal("enter on an open folder closes it")
	}
	view := ansi.Strip(d.view())
	if strings.Contains(view, "M b.go") || !strings.Contains(view, "▸ src") {
		t.Fatalf("a closed folder hides its files:\n%s", view)
	}
	press(d, "j")
	if got := treeAt(d); got != "/src" {
		t.Fatalf("nothing below a closed last folder, got %q", got)
	}
	press(d, "l")
	if d.files.closed["/src"] || treeAt(d) != "/src" {
		t.Fatal("l opens a closed folder, the cursor staying on it")
	}
	press(d, "j", "j") // /src/lib, then b.go
	press(d, "h")
	if got := treeAt(d); got != "/src/lib" {
		t.Fatalf("h on a file goes up to its folder, got %q", got)
	}
	press(d, "h")
	if !d.files.closed["/src/lib"] {
		t.Fatal("h on an open folder closes it")
	}
	press(d, "h")
	if got := treeAt(d); got != "/src" {
		t.Fatalf("h on a closed folder goes up, got %q", got)
	}
}

func TestNextFileOpensClosedFolders(t *testing.T) {
	d := fakeFolders(t)
	press(d, "j", "enter", "k") // close /src, back on readme.md
	if treeAt(d) != "/docs/readme.md" {
		t.Fatalf("setup: on %q", treeAt(d))
	}
	d.jumpFile(1)
	if got := treeAt(d); got != "/src/lib/b.go" || d.files.closed["/src"] {
		t.Fatalf("n into a closed folder opens it: on %q, closed %v", got, d.files.closed)
	}
}

func TestFilterFindsFilesInClosedFolders(t *testing.T) {
	d := fakeFolders(t)
	press(d, "j", "enter") // close /src
	press(d, "/", "c", ".", "g", "o", "enter")
	if got := treeAt(d); got != "/src/lib/c.go" || d.files.closed["/src"] {
		t.Fatalf("picking a match opens its folders: on %q, closed %v", got, d.files.closed)
	}
}

func TestFolderSummaryAndCopy(t *testing.T) {
	d := fakeFolders(t)
	press(d, "j", "j") // /src/lib
	view := ansi.Strip(d.view())
	if !strings.Contains(view, "/src/lib  2 changed files") || !strings.Contains(view, "M c.go") {
		t.Fatalf("a folder shows its changed files:\n%s", view)
	}
	press(d, "a")
	if d.modal != nil {
		t.Fatal("a folder takes no comment")
	}
}

func TestFolderKeptAcrossReload(t *testing.T) {
	d := fakeFolders(t)
	press(d, "j", "j")
	d.onFilesLoaded(filesLoadedMsg{key: d.files.cmp.key(), changes: d.files.changes})
	if got := treeAt(d); got != "/src/lib" {
		t.Fatalf("a reload keeps the cursor on the folder, got %q", got)
	}
}

func TestClickFolderTwiceCloses(t *testing.T) {
	d := fakeFolders(t)
	click := func() {
		d.onMouse(tea.MouseMsg{X: 3, Y: d.bodyTop() + 1 + 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}) // /src's row
	}
	click()
	if treeAt(d) != "/src" || d.files.closed["/src"] {
		t.Fatalf("one click selects (on %q)", treeAt(d))
	}
	click()
	if !d.files.closed["/src"] {
		t.Fatal("a second click closes it")
	}
}
