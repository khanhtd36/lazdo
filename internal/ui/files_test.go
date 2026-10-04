package ui

import (
	"strings"
	"testing"

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
