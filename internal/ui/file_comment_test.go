package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// postsTo catches the comments posted, instead of Azure DevOps.
func postsTo(t *testing.T) *[]ado.LineComment {
	t.Helper()
	var posted []ado.LineComment
	was := newLineThread
	t.Cleanup(func() { newLineThread = was })
	newLineThread = func(_ context.Context, _ *ado.Client, _ ado.PullRequest, lc ado.LineComment, _ string) error {
		posted = append(posted, lc)
		return nil
	}
	return &posted
}

// submit types text into the open editor and posts it.
func submit(t *testing.T, d *detailModel) {
	t.Helper()
	e, ok := d.modal.(*editorModal)
	if !ok {
		t.Fatalf("want an editor, got %T", d.modal)
	}
	e.ta.SetValue("looks off")
	if cmd := d.update(tea.KeyMsg{Type: tea.KeyCtrlS}); cmd != nil {
		cmd()
	}
}

func TestFileCommentFromTheTree(t *testing.T) {
	d := fakeFiles(t, 200)
	posted := postsTo(t)
	d.files.pane = paneTree
	press(d, "a")
	if e, ok := d.modal.(*editorModal); !ok || e.title != "Comment on /src/a.go" {
		t.Fatalf("a in the tree should comment on the file, got %T", d.modal)
	}
	submit(t, d)
	want := ado.LineComment{Path: "/src/a.go", ChangeTrackingID: 4, FirstIteration: 2, SecondIteration: 2}
	if len(*posted) != 1 || (*posted)[0] != want {
		t.Fatalf("posted %+v, want %+v", *posted, want)
	}
}

func TestFileCommentWhenNoLines(t *testing.T) {
	d := fakeFiles(t, 200)
	posted := postsTo(t)
	fd := d.files.diffs[d.diffKey(d.selectedChange())]
	*fd = fileDiff{binary: true}
	press(d, "a")
	submit(t, d)
	if len(*posted) != 1 || (*posted)[0].StartLine != 0 {
		t.Fatalf("a on a binary file should comment on the file: %+v", *posted)
	}
}

func TestFileCommentNeedsUpdates(t *testing.T) {
	d := fakeFiles(t, 200)
	d.files.cmp.commit = "c1"
	d.files.pane = paneTree
	press(d, "a")
	if d.modal != nil {
		t.Fatalf("a single commit takes no comments, got %T", d.modal)
	}
}

// withFileThread adds a comment on the whole of /src/a.go.
func withFileThread(t *testing.T, width int) *detailModel {
	t.Helper()
	d := fakeFiles(t, width)
	ft := ado.Thread{ID: 11, Status: "active", Comments: []ado.Comment{{ID: 1, Author: d.me, Content: "rename this file", CommentType: "text"}}}
	ft.ThreadContext = &struct {
		FilePath       string        `json:"filePath"`
		LeftFileStart  *ado.Position `json:"leftFileStart"`
		LeftFileEnd    *ado.Position `json:"leftFileEnd"`
		RightFileStart *ado.Position `json:"rightFileStart"`
		RightFileEnd   *ado.Position `json:"rightFileEnd"`
	}{FilePath: "/src/a.go"}
	d.files.threads = append(d.files.threads, ft)
	return d
}

func TestFileCommentShowsAtopTheDiff(t *testing.T) {
	d := withFileThread(t, 200)
	view := ansi.Strip(d.view())
	at, first := strings.Index(view, "rename this file"), strings.Index(view, "package a")
	if at < 0 || first < 0 || at > first || !strings.Contains(view, "on this file") {
		t.Fatalf("the file's comment goes above line 1:\n%s", view)
	}
	press(d, "enter")
	if th, ok := d.diffThread(); !d.files.inThread || !ok || th.ID != 11 {
		t.Fatal("enter on the first line steps into the file's comment")
	}
	press(d, "e")
	if e, ok := d.modal.(*editorModal); !ok || e.ta.Value() != "rename this file" {
		t.Fatalf("e edits the file's comment, got %T", d.modal)
	}
}

func TestFileCommentShownForBinary(t *testing.T) {
	d := withFileThread(t, 200)
	*d.files.diffs[d.diffKey(d.selectedChange())] = fileDiff{binary: true}
	if view := ansi.Strip(d.view()); !strings.Contains(view, "rename this file") {
		t.Fatalf("a binary file still shows its comments:\n%s", view)
	}
}

func TestClickFileComment(t *testing.T) {
	d := withFileThread(t, 200)
	d.files.cursor = 2
	// Row 1 of the diff pane is the comment's head, above line 1.
	d.onMouse(tea.MouseMsg{X: d.treeWidth() + 10, Y: d.bodyTop() + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if th, ok := d.diffThread(); !d.files.inThread || !ok || th.ID != 11 {
		t.Fatalf("clicking the file's comment steps into it (cursor %d, inThread %v)", d.files.cursor, d.files.inThread)
	}
}
