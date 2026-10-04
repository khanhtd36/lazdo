package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func fakeDetail(t *testing.T, width int) *detailModel {
	t.Helper()
	me := ado.Identity{ID: "me", DisplayName: "Me Myself"}
	other := ado.Identity{ID: "other", DisplayName: "Other Person"}
	now := time.Now()
	pr := ado.PullRequest{
		ID: 7, Title: "fix: something", CreatedBy: other, CreationDate: now.Add(-48 * time.Hour),
		SourceRefName: "refs/heads/fix/x", TargetRefName: "refs/heads/develop",
		Repository: ado.Repository{Name: "repo", Project: ado.Project{Name: "proj"}},
		Reviewers:  []ado.Reviewer{{Identity: me, IsRequired: true}},
	}
	comment := func(id int, who ado.Identity, text string, at time.Time) ado.Comment {
		return ado.Comment{ID: id, Author: who, Content: text, CommentType: "text", PublishedDate: at, LastContentDate: at}
	}
	data := &ado.PRDetail{
		PullRequest: pr,
		Description: "## Problem\n\nIt broke. Thanks @<00000000-0000-0000-0000-0000000000aa>.",
		Threads: []ado.Thread{
			{ID: 1, Status: "active", PublishedDate: now.Add(-2 * time.Hour), Comments: []ado.Comment{
				comment(1, me, "please fix", now.Add(-2*time.Hour)),
				comment(2, other, "done", now.Add(-1*time.Hour)),
			}},
			{ID: 2, Status: "fixed", PublishedDate: now.Add(-30 * time.Hour), Comments: []ado.Comment{
				comment(1, other, "old note", now.Add(-30*time.Hour)),
			}},
		},
		Commits: []ado.Commit{{ID: "abcdef1234", Comment: "fix: x"}},
		Changes: []ado.Change{{ChangeType: "edit", Item: ado.ChangeItem{Path: "/src/a/b.go"}}},
	}
	data.MergeStatus = "succeeded"
	d := newDetail(ado.NewClient("org"), me, pr, width, 40)
	d.prevVisit = now.Add(-3 * time.Hour)
	d.update(detailLoadedMsg{d: data})
	return d
}

func press(d *detailModel, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		d.update(msg)
	}
}

func TestDetailFitsWidth(t *testing.T) {
	for _, width := range []int{80, 120, 180} {
		d := fakeDetail(t, width)
		for _, tab := range []string{"1", "2", "3", "4"} {
			press(d, tab)
			for _, line := range strings.Split(d.view(), "\n") {
				if w := ansi.StringWidth(line); w >= width {
					t.Errorf("width %d tab %s: line %d wide: %q", width, tab, w, ansi.Strip(line))
				}
			}
		}
	}
}

func TestDetailGroupKeysWithoutActivity(t *testing.T) {
	d := fakeDetail(t, 120)
	d.data.Threads = nil
	d.filter = filterComments // nothing matches; Everything always has "created"
	d.rebuildOverview()
	press(d, "J", "K", "J") // must not panic with no entries
	if d.threadSel != 0 {
		t.Fatalf("selection should stay at 0 with no entries, got %d", d.threadSel)
	}
}

func TestDetailFilterKeyOpensMenu(t *testing.T) {
	d := fakeDetail(t, 120)
	press(d, "f")
	menu, ok := d.modal.(*menuModal)
	if !ok || d.filter != filterEverything {
		t.Fatal("f should open a menu, not change the filter")
	}
	if menu.cursor != int(filterEverything) || !strings.Contains(menu.items[menu.cursor].label, "✓") {
		t.Fatal("the menu should start on the current filter, checked")
	}
	press(d, "j", "enter") // All comments
	if d.modal != nil || d.filter != filterComments {
		t.Fatalf("enter should apply the picked filter, got %v", d.filter)
	}
}

func TestDetailActivityFilters(t *testing.T) {
	d := fakeDetail(t, 120)
	counts := map[activityFilter]int{}
	for f := range filterCount {
		counts[f] = len(d.entries(f))
	}
	want := map[activityFilter]int{
		filterEverything: 3, // two threads + created
		filterComments:   2,
		filterNew:        1, // "done" by other after the previous visit
		filterMine:       1,
		filterActive:     1,
		filterResolved:   1,
	}
	for f, n := range want {
		if counts[f] != n {
			t.Errorf("%s: got %d, want %d", f.title(), counts[f], n)
		}
	}
}

func TestDetailThreadSelection(t *testing.T) {
	d := fakeDetail(t, 120)
	press(d, "enter", "j")
	if !d.inThread || d.commentSel != 1 {
		t.Fatalf("inThread=%v commentSel=%d, want inside at comment 1", d.inThread, d.commentSel)
	}
	// Comment 1 is someone else's: edit must refuse without opening an editor.
	press(d, "e")
	if d.modal != nil {
		t.Fatal("edit opened for another person's comment")
	}
	press(d, "k", "e")
	if _, ok := d.modal.(*editorModal); !ok {
		t.Fatalf("edit of own comment: modal %T", d.modal)
	}
	press(d, "esc")
	if d.modal != nil || !d.inThread {
		t.Fatal("esc should close only the editor")
	}
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	if d.closeRequested(esc) || d.inThread {
		t.Fatal("first esc outside the editor should only leave the thread")
	}
	if !d.closeRequested(esc) {
		t.Fatal("second esc should close the detail view")
	}
}

func TestCompleteDialogBlocksOnPolicies(t *testing.T) {
	d := fakeDetail(t, 120)
	d.data.Policies = []ado.Policy{{Status: "queued"}}
	d.data.Policies[0].Configuration.IsBlocking = true
	d.data.Policies[0].Configuration.IsEnabled = true
	c := d.newCompleteDialog(false)
	if c.submitBlocked() == "" {
		t.Fatal("complete should be blocked by a pending blocking policy")
	}
	c.override = true
	if !strings.Contains(c.submitBlocked(), "reason") {
		t.Fatalf("override without reason: %q", c.submitBlocked())
	}
	c.reason.SetValue("hotfix")
	if c.submitBlocked() != "" {
		t.Fatalf("override with reason should be allowed: %q", c.submitBlocked())
	}
	if auto := d.newCompleteDialog(true); auto.submitBlocked() != "" {
		t.Fatal("auto-complete must not be blocked by policies")
	}
}

func TestMentionsResolved(t *testing.T) {
	d := fakeDetail(t, 120)
	d.data.Reviewers = append(d.data.Reviewers, ado.Reviewer{Identity: ado.Identity{ID: "00000000-0000-0000-0000-0000000000AA", DisplayName: "Ann"}})
	if got := d.resolveMentions("hi @<00000000-0000-0000-0000-0000000000aa>"); got != "hi **@Ann**" {
		t.Fatalf("got %q", got)
	}
}

func TestTrimStyledRight(t *testing.T) {
	in := "\x1b[38;5;252mhello\x1b[0m\x1b[38;5;252m \x1b[0m\x1b[38;5;252m \x1b[0m"
	if got := ansi.Strip(trimStyledRight(in)); got != "hello" {
		t.Fatalf("got %q", got)
	}
}
