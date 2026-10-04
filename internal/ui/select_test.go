package ui

import (
	"strings"
	"testing"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func TestContentSelectionCopiesSourceLines(t *testing.T) {
	b := fakeBrowser(t)
	long := "x := " + strings.Repeat("abc ", 40) // wraps on a narrow pane
	src := "package a\n\t" + long + "\nfunc B() {}\n"
	b.file = "/a.go"
	b.update(contentMsg{repoID: "r", branch: "develop", path: "/a.go", content: &fileContent{
		raw: splitLines(src), hl: highlight("a.go", src),
	}})
	b.pane = paneContent
	width := 100
	b.key(keyMsg("j"), width, 20) // onto line 2 (wrapped)
	b.key(keyMsg("V"), width, 20)
	for range 6 {
		b.key(keyMsg("j"), width, 20) // past the wrap rows into line 3
	}
	got := b.selectedText(width)
	want := "\t" + long + "\nfunc B() {}"
	if got != want {
		t.Fatalf("selection = %q\nwant        %q", got, want)
	}
	if _, cmd := b.key(keyMsg("y"), width, 20); cmd == nil || b.anchor != -1 {
		t.Fatal("y copies the selection and clears it")
	}
}

func TestLogSelectionCopiesAsShown(t *testing.T) {
	v := newRunView(ado.NewClient("org"), ado.ProjectInfo{}, ado.Run{ID: 1})
	v.logPane = true
	v.lines = []string{
		"2026-10-04T08:00:00.0000000Z one",
		"2026-10-04T08:00:01.0000000Z ##[error]two",
		"2026-10-04T08:00:02.0000000Z three",
	}
	v.key(keyMsg("V"), 20)
	v.key(keyMsg("j"), 20)
	lo, hi := min(v.anchor, v.cur), max(v.anchor, v.cur)
	var got []string
	for i := lo; i <= hi; i++ {
		got = append(got, logText(v.lines[i]))
	}
	if strings.Join(got, "|") != "one|two" {
		t.Fatalf("selected log text = %q", got)
	}
	if _, cmd := v.key(keyMsg("y"), 20); cmd == nil || v.anchor != -1 {
		t.Fatal("y copies the selected log lines")
	}
	v.key(keyMsg("esc"), 20)
	if v.logPane {
		t.Fatal("esc with nothing selected goes back to the steps")
	}
}

func TestDiffSelectionCopiesCursorSide(t *testing.T) {
	d := fakeFiles(t, 200) // side-by-side: old "func A() {}" / new "func A() { return }", "func B() {}"
	press(d, "n", "V", "j")
	text, n := d.diffSelectionText()
	if n != 2 || text != "func A() { return }\nfunc B() {}" {
		t.Fatalf("new side selection = %q (%d lines)", text, n)
	}
	press(d, "h") // old side: only line 3 exists in the range
	text, n = d.diffSelectionText()
	if n != 1 || text != "func A() {}" {
		t.Fatalf("old side selection = %q (%d lines)", text, n)
	}
}
