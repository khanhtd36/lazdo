package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

const samID = "5a1e0000-0000-0000-0000-00000000005a"

// mentioning is a new comment on !7, the org's users listed.
func mentioning(t *testing.T) (*detailModel, *editorModal, *string) {
	t.Helper()
	d := fakeDetail(t, 140)
	d.people = &peopleCache{loaded: true, users: []ado.Person{
		{ID: samID, Name: "Sam Rivera", Mail: "sam.rivera@contoso.com"},
		{ID: "a11ce000-0000-0000-0000-000000000000", Name: "Alice Nguyen", Mail: "alice@contoso.com"},
	}}
	d.teams = []ado.Team{{ID: "7ea70000-0000-0000-0000-000000000000", Name: "Web Team"}}
	var posted string
	e := d.mentionEditor("New comment", "", func(content string) tea.Cmd { posted = content; return nil })
	d.modal = e
	return d, e, &posted
}

func typeInto(d *detailModel, s string) {
	for _, r := range s {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{r}}
		}
		d.update(k)
	}
}

func TestMentionFuzzyByNameOrEmail(t *testing.T) {
	for _, q := range []string{"smr", "sam.riv", "rivera@con"} {
		d, e, posted := mentioning(t)
		typeInto(d, "hi @"+q)
		if !e.mention.open {
			t.Fatalf("@%s opens the list", q)
		}
		if people, _ := e.mention.matches(); len(people) == 0 || people[0].name != "Sam Rivera" {
			t.Fatalf("@%s finds Sam first: %+v", q, people)
		}
		d.update(tea.KeyMsg{Type: tea.KeyEnter})
		if v := e.ta.Value(); v != "hi @Sam Rivera " {
			t.Fatalf("@%s inserted: %q", q, v)
		}
		typeInto(d, "please look")
		d.update(tea.KeyMsg{Type: tea.KeyCtrlS})
		if *posted != "hi @<"+samID+"> please look" {
			t.Fatalf("posted %q", *posted)
		}
	}
}

func TestMentionNotInEmailsAndEscKeepsDraft(t *testing.T) {
	d, e, _ := mentioning(t)
	typeInto(d, "mail a@b")
	if e.mention.open {
		t.Fatal("an @ inside a word isn't a mention")
	}
	typeInto(d, " @we")
	if people, _ := e.mention.matches(); !e.mention.open || len(people) == 0 || people[0].name != "Web Team" {
		t.Fatalf("teams can be mentioned: %+v", people)
	}
	if !strings.Contains(ansi.Strip(e.view(140)), "Web Team  team") {
		t.Fatalf("the list shows under the box:\n%s", ansi.Strip(e.view(140)))
	}
	d.update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.mention.open || d.modal != e {
		t.Fatal("esc closes only the list")
	}
}

func TestMentionTypedNameIsPlainText(t *testing.T) {
	d, _, posted := mentioning(t)
	typeInto(d, "ask @Sam Rivera")
	d.update(tea.KeyMsg{Type: tea.KeyEsc}) // close the list, don't pick
	d.update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if *posted != "ask @Sam Rivera" {
		t.Fatalf("a name not picked stays text: %q", *posted)
	}
}

func TestMentionEditShowsNamesAndKeepsUnknown(t *testing.T) {
	d, _, _ := mentioning(t)
	var posted string
	e := d.mentionEditor("Edit comment", "cc @<"+samID+"> and @<00000000-0000-0000-0000-0000000000ff>",
		func(c string) tea.Cmd { posted = c; return nil })
	d.modal = e
	if v := e.ta.Value(); v != "cc @Sam Rivera and @<00000000-0000-0000-0000-0000000000ff>" {
		t.Fatalf("editor shows %q", v)
	}
	d.update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if posted != "cc @<"+samID+"> and @<00000000-0000-0000-0000-0000000000ff>" {
		t.Fatalf("saved %q", posted)
	}
}
