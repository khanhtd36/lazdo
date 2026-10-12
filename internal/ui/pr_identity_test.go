package ui

import (
	"context"
	"testing"
	"time"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Writes and answers stay with the pull request they were for, whatever
// the background refresh or a quick switch to another one brings.

func dashboardWith(t *testing.T, prs ...int) Model {
	t.Helper()
	m := New(ado.NewClient("org"), time.Minute)
	m.width, m.height, m.loading = 120, 30, false
	next, _ := m.Update(listFor(prs...))
	return next.(Model)
}

// listFor is a refresh where I review the given pull requests, in order.
func listFor(ids ...int) listMsg {
	me := ado.Identity{ID: "me"}
	prs := make([]ado.PullRequest, 0, len(ids))
	for _, id := range ids {
		prs = append(prs, ado.PullRequest{ID: id, Title: "pr", Reviewers: []ado.Reviewer{{Identity: me}}})
	}
	return listMsg{me: me, sections: ado.Classify("me", prs, nil)}
}

func selectedID(m Model) int {
	if r := m.rows(); m.cursor < len(r) && r[m.cursor].pr != nil {
		return r[m.cursor].pr.ID
	}
	return 0
}

func TestBackgroundRefreshLeavesTheOpenPRAlone(t *testing.T) {
	m := dashboardWith(t, 5, 7, 9)
	m.detail = fakeDetail(t, 120) // !7, open for a long time
	pressed := m.detail.act("voted", false, func(_ context.Context, pr ado.PullRequest) error {
		if pr.ID != 7 {
			t.Errorf("the vote went to !%d", pr.ID)
		}
		return nil
	})
	// Refreshes: new pull requests above it, then !7 gone from the list.
	for _, l := range []listMsg{listFor(1, 2, 3, 5, 7, 9), listFor(1, 2)} {
		next, _ := m.Update(l)
		m = next.(Model)
	}
	if m.detail.pr.ID != 7 || m.detail.data.ID != 7 {
		t.Fatalf("the open detail moved to !%d", m.detail.pr.ID)
	}
	if done, ok := pressed().(actionDoneMsg); !ok || done.prID != 7 {
		t.Fatalf("got %#v", done)
	}
}

func TestLateAnswersForAnotherPRAreDropped(t *testing.T) {
	m := dashboardWith(t)
	a := fakeDetail(t, 120) // !7, its reload still in flight
	late := detailLoadedMsg{prID: 7, d: a.data}
	lateDone := actionDoneMsg{prID: 7, text: "completed !7", leave: true}

	b := ado.PullRequest{ID: 8, Title: "feat: other", Repository: a.pr.Repository}
	m.detail = newDetail(m.client, m.me, b, 120, 30) // esc, then open !8
	next, _ := m.Update(detailLoadedMsg{prID: 8, d: &ado.PRDetail{PullRequest: b}})
	m = next.(Model)
	pressed := m.detail.act("voted", false, func(_ context.Context, pr ado.PullRequest) error {
		if pr.ID != 8 {
			t.Errorf("the vote went to !%d", pr.ID)
		}
		return nil
	})
	for _, msg := range []any{late, visitMsg{prID: 7}, filesLoadedMsg{prID: 7, key: m.detail.files.cmp.key()}, targetChangedMsg{prID: 7, branch: "main"}} {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if m.detail.pr.ID != 8 || m.detail.data.ID != 8 {
		t.Fatalf("!7's answer replaced !8: now !%d", m.detail.pr.ID)
	}
	if m.detail.files.loadedKey != "" {
		t.Fatal("!7's files filled !8's Files tab")
	}
	pressed()

	// !7's merge finishing late must not close !8; it is reported as !7's.
	next, _ = m.Update(lateDone)
	m = next.(Model)
	if m.detail == nil || m.detail.pr.ID != 8 {
		t.Fatal("!7 completing closed !8")
	}
	if m.detail.status != "!7: completed !7" {
		t.Fatalf("status %q", m.detail.status)
	}
}

func TestMergeUsesWhatWasConfirmed(t *testing.T) {
	saved := completePR
	defer func() { completePR = saved }()
	var merged ado.PRDetail
	completePR = func(_ context.Context, _ *ado.Client, d *ado.PRDetail, _ ado.CompletionOptions) error {
		merged = *d
		return nil
	}
	d := fakeDetail(t, 120)
	d.data.LastMergeSource = ado.Commit{ID: "aaa"}
	_, cmd := d.newCompleteDialog(false).submit()
	if cmd == nil {
		t.Fatal("submit should complete")
	}
	// A reload lands before the request goes out, with a push made since.
	newer := *d.data
	newer.LastMergeSource = ado.Commit{ID: "bbb"}
	d.update(detailLoadedMsg{prID: 7, d: &newer})
	if done := cmd().(actionDoneMsg); done.prID != 7 || !done.leave {
		t.Fatalf("got %#v", done)
	}
	if merged.ID != 7 || merged.LastMergeSource.ID != "aaa" {
		t.Fatalf("merged !%d at %s, want !7 at the confirmed aaa", merged.ID, merged.LastMergeSource.ID)
	}
}

func TestDashboardCursorFollowsItsPR(t *testing.T) {
	m := dashboardWith(t, 5, 7, 9)
	for selectedID(m) != 7 {
		next, _ := m.Update(keyMsg("j"))
		m = next.(Model)
	}
	next, _ := m.Update(listFor(1, 2, 3, 5, 7, 9)) // three new ones above
	m = next.(Model)
	if got := selectedID(m); got != 7 {
		t.Fatalf("the cursor drifted to !%d", got)
	}
	next, _ = m.Update(listFor(5, 9)) // !7 merged: the cursor stays put
	if m = next.(Model); m.cursor >= len(m.rows()) {
		t.Fatal("the cursor should stay on the list")
	}
}

func TestProjectPRListCursorFollowsItsPR(t *testing.T) {
	var l pickList
	pr := func(ids ...int) []pickItem {
		items := make([]pickItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, pickItem{search: "pr", value: ado.PullRequest{ID: id}})
		}
		return items
	}
	l.setItems(pr(5, 7, 9))
	l.cursor = 1 // !7
	l.refreshItems(pr(1, 2, 5, 7, 9), samePR)
	if it, _ := l.selected(); it.value.(ado.PullRequest).ID != 7 {
		t.Fatalf("the cursor drifted to !%d", it.value.(ado.PullRequest).ID)
	}
}
