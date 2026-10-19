package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func TestRestartKeepsThePageAndPullRequest(t *testing.T) {
	m := dashboardWith(t, 7)
	m.detail = fakeDetail(t, 120)
	m.detail.tab = tabFiles
	m.installed = "9.9.9"
	next, _ := m.Update(restartMsg{})
	state, ok := next.(Model).RestartState()
	if !ok || state != "page=0,pr=7,tab=1" {
		t.Fatalf("restart state %q %v", state, ok)
	}

	r := New(ado.NewClient("org"), 0).Restore(state)
	if r.restorePR != 7 || r.restoreTab != tabFiles || r.page != pagePRs {
		t.Fatalf("restored %+v", r)
	}
	if r.reopenPR() == nil {
		t.Fatal("a restored pull request is fetched")
	}
	r.width, r.height = 120, 30
	next, _ = r.Update(restoredPRMsg{id: 7, pr: ado.PullRequest{ID: 7, Title: "fix"}})
	if d := next.(Model).detail; d == nil || d.pr.ID != 7 || d.tab != tabFiles {
		t.Fatalf("the pull request reopens on its tab: %+v", d)
	}
}

func TestRestoreIgnoresJunkAndMissingPR(t *testing.T) {
	r := New(ado.NewClient("org"), 0).Restore("page=9,pr=x,tab=-1,what=1")
	if r.page != pagePRs || r.restorePR != 0 || r.reopenPR() != nil {
		t.Fatalf("junk is ignored: %+v", r)
	}
	next, _ := r.Restore("pr=5").Update(restoredPRMsg{id: 5, err: errors.New("not found")})
	if m := next.(Model); m.detail != nil || !strings.Contains(m.status, "couldn't reopen !5") {
		t.Fatalf("a missing pull request leaves the dashboard with a note: %q", m.status)
	}
}

func TestRestartWarnsAboutADraft(t *testing.T) {
	m := dashboardWith(t, 7)
	m.detail = fakeDetail(t, 120)
	m.installed = "9.9.9"
	m.detail.modal = m.detail.newCommentEditor()
	if c := m.restartPrompt().(*confirmModal); !strings.Contains(c.prompt, "What you are typing will be lost.") || !c.yes {
		t.Fatalf("prompt %q (Yes focused %v)", c.prompt, c.yes)
	}
}
