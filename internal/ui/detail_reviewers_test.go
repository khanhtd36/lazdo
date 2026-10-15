package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

type reviewerCall struct {
	id       string
	required bool
	vote     int
	removed  bool
}

// fakeReviewers is !7 with Me required and Sam optional, having approved;
// the writes it makes land in calls.
func fakeReviewers(t *testing.T) (*detailModel, *[]reviewerCall) {
	t.Helper()
	d := fakeDetail(t, 140)
	d.data.Status = "active"
	d.data.Reviewers = append(d.data.Reviewers, ado.Reviewer{Identity: ado.Identity{ID: "sam", DisplayName: "Sam Rivera"}, Vote: ado.VoteApproved})
	var calls []reviewerCall
	set, remove, search := setReviewer, removeReviewer, searchPeople
	t.Cleanup(func() { setReviewer, removeReviewer, searchPeople = set, remove, search })
	setReviewer = func(_ context.Context, _ *ado.Client, _ ado.PullRequest, id string, required bool, vote int) error {
		calls = append(calls, reviewerCall{id: id, required: required, vote: vote})
		return nil
	}
	removeReviewer = func(_ context.Context, _ *ado.Client, _ ado.PullRequest, id string) error {
		calls = append(calls, reviewerCall{id: id, removed: true})
		return nil
	}
	searchPeople = func(_ context.Context, _ *ado.Client, _ string) ([]ado.Person, error) {
		return []ado.Person{
			{ID: "jo", Name: "Jo Park", Mail: "jo@contoso.com"},
			{ID: "sam", Name: "Sam Rivera", Mail: "sam@contoso.com"},
			{ID: "web", Name: "[Demo]\\Web Team", IsGroup: true},
		}, nil
	}
	d.onKey(key("w"))
	if _, ok := d.modal.(*reviewersModal); !ok {
		t.Fatalf("w should open the reviewers, got %T", d.modal)
	}
	return d, &calls
}

// pressOn sends keys to the open popup and runs what the last one returns.
func pressOn(d *detailModel, keys ...string) tea.Msg {
	var cmd tea.Cmd
	for _, k := range keys {
		cmd = d.update(key(k))
	}
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestReviewersListRequiredFirst(t *testing.T) {
	d, _ := fakeReviewers(t)
	view := ansi.Strip(d.modal.view(d.width))
	me, sam := strings.Index(view, "Me Myself"), strings.Index(view, "Sam Rivera")
	if me < 0 || sam < 0 || me > sam {
		t.Fatalf("required first:\n%s", view)
	}
	if !strings.Contains(view, "optional · Approved") {
		t.Fatalf("each shows required or optional and the vote:\n%s", view)
	}
}

func TestReviewerMadeRequired(t *testing.T) {
	d, calls := fakeReviewers(t)
	msg := pressOn(d, "j", "t")
	if len(*calls) != 1 || (*calls)[0] != (reviewerCall{id: "sam", required: true, vote: ado.VoteApproved}) {
		t.Fatalf("calls %+v", *calls)
	}
	if done, ok := msg.(actionDoneMsg); !ok || done.text != "Sam Rivera is now required" {
		t.Fatalf("got %#v", msg)
	}
	if _, ok := d.modal.(*reviewersModal); !ok {
		t.Fatal("the list stays open")
	}
}

func TestReviewerRemoveAsksFirst(t *testing.T) {
	d, calls := fakeReviewers(t)
	pressOn(d, "j", "d")
	c, ok := d.modal.(*confirmModal)
	if !ok || !strings.Contains(ansi.Strip(c.view(d.width)), "Their vote (Approved) goes with them") {
		t.Fatalf("d should ask, naming the vote lost: %T", d.modal)
	}
	pressOn(d, "enter") // No is focused
	if len(*calls) != 0 {
		t.Fatal("enter answered No")
	}
	if _, ok := d.modal.(*reviewersModal); !ok {
		t.Fatalf("No goes back to the list, got %T", d.modal)
	}
	pressOn(d, "d", "y")
	if len(*calls) != 1 || (*calls)[0] != (reviewerCall{id: "sam", removed: true}) {
		t.Fatalf("calls %+v", *calls)
	}
	if _, ok := d.modal.(*reviewersModal); !ok {
		t.Fatalf("Yes goes back to the list, got %T", d.modal)
	}
}

// typeName types into the picker and lets the pause pass.
func typeName(t *testing.T, d *detailModel, name string) {
	t.Helper()
	for _, r := range name {
		d.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p := d.modal.(*peoplePicker)
	if cmd := d.update(peopleTickMsg{prID: 7, seq: p.seq - 1}); cmd != nil {
		t.Fatal("a pause typing went on after searches nothing")
	}
	cmd := d.update(peopleTickMsg{prID: 7, seq: p.seq})
	if cmd == nil {
		t.Fatal("the pause after the last key searches")
	}
	d.update(cmd())
}

func TestAddReviewer(t *testing.T) {
	for _, tc := range []struct {
		key      string
		required bool
	}{{"a", false}, {"A", true}} {
		d, calls := fakeReviewers(t)
		pressOn(d, tc.key)
		if _, ok := d.modal.(*peoplePicker); !ok {
			t.Fatalf("%s opens the people picker, got %T", tc.key, d.modal)
		}
		typeName(t, d, "jo")
		view := ansi.Strip(d.modal.view(d.width))
		if !strings.Contains(view, "Jo Park") || !strings.Contains(view, "Web Team (group)") || !strings.Contains(view, "already a reviewer") {
			t.Fatalf("results:\n%s", view)
		}
		msg := pressOn(d, "enter")
		if len(*calls) != 1 || (*calls)[0] != (reviewerCall{id: "jo", required: tc.required}) {
			t.Fatalf("%s: calls %+v", tc.key, *calls)
		}
		if _, ok := msg.(actionDoneMsg); !ok {
			t.Fatalf("got %#v", msg)
		}
		if _, ok := d.modal.(*reviewersModal); !ok {
			t.Fatalf("adding goes back to the list, got %T", d.modal)
		}
	}
}

func TestAddReviewerAlreadyThere(t *testing.T) {
	d, calls := fakeReviewers(t)
	pressOn(d, "a")
	typeName(t, d, "sam")
	msg := pressOn(d, "down", "enter")
	if len(*calls) != 0 {
		t.Fatalf("calls %+v", *calls)
	}
	if s, ok := msg.(statusMsg); !ok || !strings.Contains(string(s), "already a reviewer") {
		t.Fatalf("got %#v", msg)
	}
}

func TestStaleSearchIgnored(t *testing.T) {
	d, _ := fakeReviewers(t)
	pressOn(d, "a")
	typeName(t, d, "jo")
	p := d.modal.(*peoplePicker)
	d.update(peopleFoundMsg{prID: 7, seq: p.seq - 1, people: []ado.Person{{ID: "x", Name: "Old Result"}}})
	if strings.Contains(ansi.Strip(p.view(d.width)), "Old Result") {
		t.Fatal("an answer to older text must not replace the results")
	}
}

func TestReviewersOfAClosedPR(t *testing.T) {
	d, calls := fakeReviewers(t)
	d.data.Status = "completed"
	for _, k := range []string{"a", "t", "d"} {
		msg := pressOn(d, k)
		if s, ok := msg.(statusMsg); !ok || string(s) != "pull request is completed" {
			t.Errorf("%s: got %#v", k, msg)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("calls %+v", *calls)
	}
}

func TestClickingReviewersPaneOpensThem(t *testing.T) {
	d := fakeDetail(t, 160)
	if !d.sideBySide() {
		t.Skip("no side panes at this width")
	}
	d.onMouse(tea.MouseMsg{X: d.width - 3, Y: d.bodyTop() + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if _, ok := d.modal.(*reviewersModal); !ok {
		t.Fatalf("got %T", d.modal)
	}
}
