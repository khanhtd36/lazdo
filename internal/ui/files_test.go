package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// fakeFiles opens the Files tab on one edited file with a comment thread on
// new line 3.
func fakeFiles(t *testing.T, width int) *detailModel {
	t.Helper()
	d := fakeDetail(t, width)
	d.data.Pushes = []ado.Push{{ID: 1}, {ID: 2}}
	ch := ado.Change{ChangeType: "edit", ChangeTrackingID: 4, Item: ado.ChangeItem{Path: "/src/a.go"}}
	thread := ado.Thread{ID: 9, Status: "active", Comments: []ado.Comment{{ID: 1, Author: d.me, Content: "why?", CommentType: "text"}}}
	thread.ThreadContext = &struct {
		FilePath       string        `json:"filePath"`
		LeftFileStart  *ado.Position `json:"leftFileStart"`
		LeftFileEnd    *ado.Position `json:"leftFileEnd"`
		RightFileStart *ado.Position `json:"rightFileStart"`
		RightFileEnd   *ado.Position `json:"rightFileEnd"`
	}{FilePath: "/src/a.go", RightFileStart: &ado.Position{Line: 3, Offset: 1}, RightFileEnd: &ado.Position{Line: 3, Offset: 5}}

	d.tab = tabFiles
	d.files.cmp = comparison{label: "All changes", target: 2}
	d.onFilesLoaded(filesLoadedMsg{key: d.files.cmp.key(), changes: []ado.Change{ch}, threads: []ado.Thread{thread}})
	left := "package a\n\nfunc A() {}\n"
	right := "package a\n\nfunc A() { return }\nfunc B() {}\n"
	fd := &fileDiff{
		leftRaw: splitLines(left), rightRaw: splitLines(right),
		leftHL: highlight("a.go", left), rightHL: highlight("a.go", right),
	}
	fd.sbs = sideBySideLines([]ado.LineBlock{
		{ChangeType: "none", OriginalStart: 1, OriginalCount: 2, ModifiedStart: 1, ModifiedCount: 2},
		{ChangeType: "edit", OriginalStart: 3, OriginalCount: 1, ModifiedStart: 3, ModifiedCount: 2},
	}, 3, 4)
	fd.inline = inlineLines(fd.sbs)
	d.files.diffs[d.diffKey(d.selectedChange())] = fd
	d.files.pane = paneDiff
	return d
}

func TestFilesRendersThreadAndFits(t *testing.T) {
	for _, width := range []int{90, 200} {
		d := fakeFiles(t, width)
		view := d.view()
		if !strings.Contains(ansi.Strip(view), "why?") {
			t.Fatalf("width %d: inline thread missing:\n%s", width, ansi.Strip(view))
		}
		for _, line := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(line); w >= width {
				t.Errorf("width %d: line %d wide: %q", width, w, ansi.Strip(line))
			}
		}
	}
}

func TestFilesLineCommentRange(t *testing.T) {
	d := fakeFiles(t, 200) // side-by-side
	press(d, "n")          // first change: line index 2 (new lines 3)
	if d.files.cursor != 2 {
		t.Fatalf("cursor after n = %d, want 2", d.files.cursor)
	}
	press(d, "G", "N")
	if d.files.cursor != 2 {
		t.Fatalf("N from the end should go back to the change, got %d", d.files.cursor)
	}
	press(d, "V", "j", "a")
	e, ok := d.modal.(*editorModal)
	if !ok {
		t.Fatalf("a opened %T, want editor", d.modal)
	}
	if !strings.Contains(e.title, "lines 3–4") {
		t.Fatalf("title %q, want lines 3–4", e.title)
	}
	press(d, "esc")

	// The old side has no line 4: commenting there is refused.
	d.files.anchor = -1
	press(d, "k", "h", "a")
	if d.modal == nil {
		t.Fatal("old line 3 should be commentable")
	}
	press(d, "esc", "j", "a")
	if d.modal != nil {
		t.Fatal("no old line on this row; a should refuse")
	}
}

func TestFilesThreadSelection(t *testing.T) {
	d := fakeFiles(t, 200)
	press(d, "n", "enter")
	if !d.files.inThread {
		t.Fatal("enter on a line with a thread should step into it")
	}
	press(d, "e")
	if _, ok := d.modal.(*editorModal); !ok {
		t.Fatalf("e on own comment opened %T", d.modal)
	}
}

// fakeCommits gives the PR four commits, newest first; c2 is a merge.
func fakeCommits(t *testing.T) *detailModel {
	t.Helper()
	d := fakeDetail(t, 140)
	d.data.Pushes = []ado.Push{{ID: 1}}
	d.data.Commits = []ado.Commit{{ID: "c4", Comment: "four"}, {ID: "c3", Comment: "three"}, {ID: "c2", Comment: "Merge develop"}, {ID: "c1", Comment: "one"}}
	d.parents = map[string]int{"c4": 1, "c3": 1, "c2": 2, "c1": 1}
	d.rebuildLists()
	return d
}

func TestCommitPickerFillsGapsIntoOneRun(t *testing.T) {
	d := fakeCommits(t)
	p := d.newCommitPicker()
	space := tea.KeyMsg{Type: tea.KeySpace}
	p.update(space)       // c4
	p.update(keyMsg("j")) // c3
	p.update(keyMsg("j")) // c2
	p.update(space)       // c2: c3 is included
	if !strings.Contains(ansi.Strip(p.view(140)), "(included)") {
		t.Fatal("c3, between the marked commits, should show as included")
	}
	m, _ := p.update(tea.KeyMsg{Type: tea.KeyEnter})
	if m != nil || d.files.cmp.commit != "c4" || d.files.cmp.oldest != "c2" {
		t.Fatalf("enter should show the run c2..c4, got %+v", d.files.cmp)
	}
	if label := ansi.Strip(d.tabLabels()[tabFiles]); label != "2 Files (3 of 4 commits)" {
		t.Fatalf("tab label: %q", label)
	}
	if !d.runMerge() {
		t.Fatal("the run holds merge c2")
	}
	if marks := strings.Count(ansi.Strip(listText(&d.lists[tabCommits])), "●"); marks != 3 {
		t.Fatalf("the Commits tab should mark the 3 shown commits, got %d", marks)
	}
}

func TestCommitPickerAllIsAllChanges(t *testing.T) {
	d := fakeCommits(t)
	d.openCommitDiff("c3")
	if d.files.cmp.commit != "c3" || d.files.cmp.oldest != "" || d.tab != tabFiles {
		t.Fatalf("enter on a commit shows just it: %+v", d.files.cmp)
	}
	p := d.newCommitPicker()
	if !p.marked["c3"] {
		t.Fatal("the picker should start from what Files shows")
	}
	p.update(keyMsg("a")) // clears
	p.update(keyMsg("a")) // all
	p.update(tea.KeyMsg{Type: tea.KeyEnter})
	if d.files.cmp.label != "All changes" || d.runMerge() {
		t.Fatalf("every commit is All changes, got %+v", d.files.cmp)
	}
}

// fakeTwoFiles opens Files on a.go and b.go, each with changes on lines 2
// and 5 of 6.
func fakeTwoFiles(t *testing.T) *detailModel {
	t.Helper()
	d := fakeDetail(t, 140)
	d.data.Pushes = []ado.Push{{ID: 1}}
	d.tab = tabFiles
	d.files.cmp = comparison{label: "All changes", target: 1}
	changes := []ado.Change{
		{ChangeType: "edit", Item: ado.ChangeItem{Path: "/a.go"}},
		{ChangeType: "edit", Item: ado.ChangeItem{Path: "/b.go"}},
	}
	d.onFilesLoaded(filesLoadedMsg{key: d.files.cmp.key(), changes: changes})
	for i := range changes {
		d.files.diffs[d.diffKey(&changes[i])] = twoChangeDiff()
	}
	if ch := d.selectedChange(); ch == nil || ch.Item.Path != "/a.go" {
		t.Fatalf("the tree should start on a.go, got %+v", ch)
	}
	d.files.pane = paneDiff
	return d
}

func twoChangeDiff() *fileDiff {
	text := "1\n2\n3\n4\n5\n6\n"
	fd := &fileDiff{leftRaw: splitLines(text), rightRaw: splitLines(text)}
	fd.sbs = sideBySideLines([]ado.LineBlock{
		{ChangeType: "none", OriginalStart: 1, OriginalCount: 1, ModifiedStart: 1, ModifiedCount: 1},
		{ChangeType: "edit", OriginalStart: 2, OriginalCount: 1, ModifiedStart: 2, ModifiedCount: 1},
		{ChangeType: "none", OriginalStart: 3, OriginalCount: 2, ModifiedStart: 3, ModifiedCount: 2},
		{ChangeType: "edit", OriginalStart: 5, OriginalCount: 1, ModifiedStart: 5, ModifiedCount: 1},
		{ChangeType: "none", OriginalStart: 6, OriginalCount: 1, ModifiedStart: 6, ModifiedCount: 1},
	}, 6, 6)
	fd.inline = inlineLines(fd.sbs)
	return fd
}

func TestDiffNextChangeCrossesFiles(t *testing.T) {
	d := fakeTwoFiles(t)
	f := &d.files
	path := func() string { return d.selectedChange().Item.Path }
	press(d, "n", "n")
	last := f.cursor
	press(d, "n") // no change left: stays, warns
	if path() != "/a.go" || f.cursor != last || f.edge != 1 {
		t.Fatalf("n at the last change should stay once: %s cursor %d edge %d", path(), f.cursor, f.edge)
	}
	press(d, "n") // again: next file, first change
	first := firstChanged(d.diffLines())
	if path() != "/b.go" || f.cursor != first {
		t.Fatalf("n again should open b.go at its first change %d, got %s at %d", first, path(), f.cursor)
	}
	press(d, "N", "p") // stay at b's first change, then back to a.go (p is N)
	if path() != "/a.go" || f.cursor != last {
		t.Fatalf("N N should go back to a.go's last change %d, got %s at %d", last, path(), f.cursor)
	}
	press(d, "n", "j", "n") // a key in between disarms
	if path() != "/a.go" || f.edge != 1 {
		t.Fatalf("j between should disarm the jump: %s edge %d", path(), f.edge)
	}
}

func TestDiffNextFileLandsWhenLoaded(t *testing.T) {
	d := fakeTwoFiles(t)
	bKey := d.files.cmp.key() + "|/b.go"
	delete(d.files.diffs, bKey) // b.go not loaded yet
	press(d, "n", "n", "n", "n")
	if d.selectedChange().Item.Path != "/b.go" || d.files.land != 1 {
		t.Fatalf("should wait on b.go to land on its first change, land %d", d.files.land)
	}
	d.update(fileDiffMsg{key: bKey, diff: twoChangeDiff()})
	if want := firstChanged(d.diffLines()); d.files.cursor != want || d.files.land != 0 {
		t.Fatalf("after loading, cursor should be on the first change %d, got %d", want, d.files.cursor)
	}
}

func firstChanged(lines []diffLine) int {
	for i, l := range lines {
		if l.kind != lineContext {
			return i
		}
	}
	return -1
}

// bom is a byte order mark, built from its code point so no editor or tool
// along the way turns it into an invisible literal.
var bom = string(rune(0xfeff))

func TestDisplayTextIsSafeToDraw(t *testing.T) {
	in := bom + "// <auto-generated />\r\nusing\tSystem;\rold mac\x1b[2Jboom\x7f\n"
	want := "// <auto-generated />\nusing    System;?old mac?[2Jboom?\n"
	if got := displayText(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDiffOfFileWithBOMFitsTheScreen(t *testing.T) {
	d := fakeFiles(t, 120)
	right := bom + "package a\n\nfunc A() { return }\nfunc B() {}\n"
	fd := d.files.diffs[d.diffKey(d.selectedChange())]
	fd.rightRaw, fd.rightHL = splitLines(right), highlight("a.go", right)
	for _, line := range strings.Split(d.view(), "\n") {
		if strings.Contains(line, bom) {
			t.Fatalf("a byte order mark reached the screen: %q", ansi.Strip(line))
		}
		if w := ansi.StringWidth(line); w >= 120 {
			t.Fatalf("line %d wide: %q", w, ansi.Strip(line))
		}
	}
}

func TestSubmoduleParsing(t *testing.T) {
	gm := "[submodule \"unit-sim/external/MITS11\"]\n\tpath = unit-sim/external/MITS11\n\turl = https://arbinSW@dev.azure.com/arbinSW/MITS11/_git/MITS11\n" +
		"[submodule \"x\"]\n\turl = ../tools\n\tpath = vendor/tools/\n"
	m := parseGitmodules(gm)
	if m["unit-sim/external/MITS11"] != "https://arbinSW@dev.azure.com/arbinSW/MITS11/_git/MITS11" || m["vendor/tools"] != "../tools" {
		t.Fatalf("parsed %v", m)
	}
	for url, want := range map[string][2]string{
		"https://arbinSW@dev.azure.com/arbinSW/MITS11/_git/MITS11": {"MITS11", "MITS11"},
		"https://arbinsw.visualstudio.com/Other/_git/lib.git":      {"Other", "lib"},
		"git@ssh.dev.azure.com:v3/arbinSW/MITS11/dev-tools":        {"MITS11", "dev-tools"},
		"../tools": {"", "tools"},
	} {
		if p, n := submoduleRepo(url); p != want[0] || n != want[1] {
			t.Errorf("%s: got %s/%s", url, p, n)
		}
	}
	repos := []ado.Repo{
		{Name: "tools", Project: ado.Project{Name: "Elsewhere"}},
		{Name: "tools", Project: ado.Project{Name: "MITS11"}},
	}
	if r := findRepo(repos, "", "tools", "MITS11"); r == nil || r.Project.Name != "MITS11" {
		t.Fatalf("a relative URL should prefer the PR's own project: %+v", r)
	}
	if r := findRepo(repos, "Elsewhere", "tools", "MITS11"); r == nil || r.Project.Name != "Elsewhere" {
		t.Fatalf("a URL naming its project should get that one: %+v", r)
	}
}
