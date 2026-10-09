package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func fakeBrowser(t *testing.T) *repoBrowser {
	t.Helper()
	r := ado.Repo{ID: "r", Name: "repo", DefaultBranch: "refs/heads/develop", WebURL: "https://x/_git/repo"}
	b := newRepoBrowser(ado.NewClient("org"), ado.ProjectInfo{Name: "proj"}, r)
	b.init()
	b.update(branchesMsg{repoID: "r", branches: []ado.Branch{{Name: "develop", IsBaseVersion: true}, {Name: "feat/x", AheadCount: 2}}})
	b.update(folderMsg{repoID: "r", branch: "develop", folder: "/", items: []ado.RepoItem{
		{Path: "/README.md"}, {Path: "/src", IsFolder: true},
	}})
	return b
}

func treeText(b *repoBrowser) string { return listText(&b.tree) }

func TestRepoBrowserTreeFoldersFirstAndExpand(t *testing.T) {
	b := fakeBrowser(t)
	if text := treeText(b); strings.Index(text, "src") > strings.Index(text, "README.md") {
		t.Fatalf("folders come before files:\n%s", text)
	}
	if _, cmd := b.key(keyMsg("enter"), 120, 20); cmd == nil || !b.expanded["/src"] {
		t.Fatal("enter on a folder expands it and loads its children")
	}
	b.update(folderMsg{repoID: "r", branch: "develop", folder: "/src", items: []ado.RepoItem{{Path: "/src/a.go"}}})
	if !strings.Contains(treeText(b), "a.go") {
		t.Fatalf("expanded folder shows its files:\n%s", treeText(b))
	}
}

func TestRepoBrowserGoToFileAndBranchSwitch(t *testing.T) {
	b := fakeBrowser(t)
	// / loads the full index; files in unopened folders become findable.
	if _, cmd := b.key(keyMsg("/"), 120, 20); cmd == nil {
		t.Fatal("/ should start loading the full file index")
	}
	b.update(indexMsg{repoID: "r", branch: "develop", items: []ado.RepoItem{
		{Path: "/src", IsFolder: true}, {Path: "/src/deep", IsFolder: true}, {Path: "/src/deep/main.go"}, {Path: "/README.md"},
	}})
	for _, k := range []string{"m", "a", "i", "n", ".", "g", "o"} {
		b.key(keyMsg(k), 120, 20)
	}
	if _, cmd := b.key(keyMsg("enter"), 120, 20); cmd == nil {
		t.Fatal("enter on a found file should load it")
	}
	if b.file != "/src/deep/main.go" || !b.expanded["/src"] || !b.expanded["/src/deep"] || b.pane != paneContent {
		t.Fatalf("go-to-file should open the file and its folders: file=%q expanded=%v", b.file, b.expanded)
	}
	b.update(contentMsg{repoID: "r", branch: "develop", path: b.file, content: &fileContent{
		raw: []string{"package main", "func main() {}"}, hl: highlight("main.go", "package main\nfunc main() {}\n"),
	}})
	if !strings.Contains(ansi.Strip(strings.Join(b.view(120, 10), "\n")), "func main") {
		t.Fatal("content should render")
	}

	// Switching branch keeps the open folders and file and reloads them.
	b.pane = paneBranches
	b.branches.move(1)
	if _, cmd := b.key(keyMsg("enter"), 160, 20); cmd == nil || b.branch != "feat/x" {
		t.Fatalf("enter on a branch switches to it, got %q", b.branch)
	}
	if !b.expanded["/src/deep"] || b.file != "/src/deep/main.go" {
		t.Fatal("open folders and file survive a branch switch")
	}
}

func TestRepoBrowserFitsAndFolds(t *testing.T) {
	b := fakeBrowser(t)
	for _, width := range []int{100, 200} {
		view := b.view(width, 12)
		for _, line := range view {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line %d wide: %q", width, w, ansi.Strip(line))
			}
		}
		folded := !strings.Contains(strings.Join(view, "\n"), "branches")
		if folded != (width < browserFoldWidth) {
			t.Errorf("width %d: branches folded=%v", width, folded)
		}
	}
}
