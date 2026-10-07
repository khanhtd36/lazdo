package ui

import (
	"encoding/json"
	"fmt"
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

// fakeExternalChecks adds two required Status policies like arbin-ci's: one
// passed, one running, plus a status no policy asks for.
func fakeExternalChecks(t *testing.T) *detailModel {
	t.Helper()
	d := fakeDetail(t, 140)
	policy := func(status, name string, latest int) ado.Policy {
		var p ado.Policy
		raw := fmt.Sprintf(`{"status":%q,"configuration":{"isBlocking":true,"isEnabled":true,
			"type":{"id":%q,"displayName":"Status"},
			"settings":{"statusName":%q,"statusGenre":"github-actions"}},
			"context":{"latestStatusId":%d}}`, status, ado.PolicyTypeStatus, name, latest)
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	status := func(id int, state, genreName, desc string) ado.Status {
		s := ado.Status{ID: id, State: state, Description: desc, TargetURL: fmt.Sprintf("https://ci/%d", id)}
		s.Context.Genre, s.Context.Name, _ = strings.Cut(genreName, "/")
		s.CreationDate = time.Unix(int64(id), 0)
		return s
	}
	d.data.Policies = []ado.Policy{policy("approved", "mits-cloud", 13), policy("running", "mits-cloud-e2e", 14)}
	d.data.Statuses = []ado.Status{
		status(11, "failed", "github-actions/mits-cloud-e2e", "E2E: failure"),
		status(13, "succeeded", "github-actions/mits-cloud", "arbin-ci: success"),
		status(14, "pending", "github-actions/mits-cloud-e2e", "E2E on arbin-ci"),
		status(15, "pending", "lint/style", "Linting"),
		status(16, "succeeded", "lint/style", "Lint clean"),
	}
	d.rebuildOverview()
	return d
}

func TestDetailExternalChecks(t *testing.T) {
	d := fakeExternalChecks(t)
	text := ansi.Strip(strings.Join(d.renderChecks(140), "\n"))
	for _, want := range []string{
		"✓ arbin-ci: success  external · succeeded",
		"● E2E on arbin-ci  external · running",
		"Optional checks",
		"✓ Lint clean  external · succeeded",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("checks should show %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "E2E: failure") || strings.Contains(text, "Linting") {
		t.Errorf("only the latest status of each check shows:\n%s", text)
	}
	links := d.checkLinks()
	if len(links) != 3 || links[1].value != "https://ci/14" {
		t.Fatalf("each check's run should be in the menus: %+v", links)
	}
	press(d, "o")
	if m, ok := d.modal.(*menuModal); !ok || len(m.items) != 4 {
		t.Fatalf("o should ask: the PR or one of 3 checks, got %T", d.modal)
	}
}

func TestDetailSubtitleKeepsTargetBranch(t *testing.T) {
	d := fakeDetail(t, 100)
	d.pr.SourceRefName = "refs/heads/QA-11284-workflow-worker-is-orphaned-when-the-main-das-process-is-killed-the-next-start"
	line := ansi.Strip(d.subtitleLine())
	if !strings.HasSuffix(line, "… into develop") || ansi.StringWidth(line) > 99 {
		t.Fatalf("a long source branch should be cut so the target shows: %q", line)
	}
}

func TestCompleteSendsWebMergeMessage(t *testing.T) {
	d := fakeDetail(t, 120)
	c := d.newCompleteDialog(false)
	want := "Merged PR 7: fix: something\n\n" + d.data.Description
	if got := c.options().MergeCommitMessage; got != want {
		t.Fatalf("an uncustomized merge should still send the web's message:\n%q\nwant\n%q", got, want)
	}
	c.custom = true
	c.message.SetValue("my own words")
	if got := c.options().MergeCommitMessage; got != "my own words" {
		t.Fatalf("a customized message should be sent as typed, got %q", got)
	}
	auto := d.newCompleteDialog(true)
	if auto.options().MergeCommitMessage != want {
		t.Fatal("auto-complete should store the web's message too")
	}
}

func TestPREditorSendsOnlyChanges(t *testing.T) {
	d := fakeDetail(t, 120)
	press(d, "E")
	p, ok := d.modal.(*prEditor)
	if !ok {
		t.Fatalf("E should open the editor, got %T", d.modal)
	}
	if title, desc, why := p.edits(); title != nil || desc != nil || why != "" {
		t.Fatal("nothing changed yet, nothing to send")
	}
	p.title.SetValue("fix: something better")
	if title, desc, _ := p.edits(); title == nil || *title != "fix: something better" || desc != nil {
		t.Fatal("only the title changed, only it should be sent")
	}
	p.desc.SetValue(strings.Repeat("x", 4001))
	if _, _, why := p.edits(); !strings.Contains(why, "4000") {
		t.Fatalf("a description over the limit should be refused, got %q", why)
	}
	p.title.SetValue("  ")
	if _, _, why := p.edits(); why == "" {
		t.Fatal("an empty title should be refused")
	}
	press(d, "esc")
	if _, ok := d.modal.(*confirmModal); !ok {
		t.Fatalf("esc with changes should ask, got %T", d.modal)
	}
}

func TestEditFormRoundTrip(t *testing.T) {
	title, desc := splitEditForm(joinEditForm(" fix: x ", "## Problem\n\nIt broke.\n"))
	if title != "fix: x" || desc != "## Problem\n\nIt broke." {
		t.Fatalf("got %q / %q", title, desc)
	}
	if title, desc := splitEditForm("only a title\n"); title != "only a title" || desc != "" {
		t.Fatalf("got %q / %q", title, desc)
	}
}

func TestDetailMarksDraft(t *testing.T) {
	d := fakeDetail(t, 120)
	d.pr.IsDraft = true
	if title := ansi.Strip(d.titleLine()); !strings.HasPrefix(title, "[draft] fix: something") {
		t.Fatalf("a draft's title should carry the draft marker: %q", title)
	}
	if sub := ansi.Strip(d.subtitleLine()); !strings.HasPrefix(sub, " Draft ") {
		t.Fatalf("the badge should read Draft: %q", sub)
	}
}

func TestCompleteFitsALongDescription(t *testing.T) {
	d := fakeDetail(t, 120)
	d.data.Description = strings.Repeat("A line of the description, as in !15077.\n", 97) // 3,977 characters
	c := d.newCompleteDialog(false)
	o := c.options()
	if o.EncodedLength() > completionBudget {
		t.Fatalf("the options measure %d, over the %d budget", o.EncodedLength(), completionBudget)
	}
	if !strings.HasPrefix(o.MergeCommitMessage, "Merged PR 7: fix: something\n\nA line") || !strings.HasSuffix(o.MergeCommitMessage, "…") {
		t.Fatalf("the description should be cut, not the title: %q…", o.MergeCommitMessage[:60])
	}
	c.custom = true
	c.message.SetValue(strings.Repeat("x", 4100))
	if why := c.submitBlocked(); !strings.Contains(why, "too long") {
		t.Fatalf("an over-long custom message should be refused, got %q", why)
	}
	c.message.SetValue("short")
	c.opts.BypassReason = strings.Repeat("r", 5000) // can't fit at all: must not loop
	c.custom = false
	_ = c.options()
}
