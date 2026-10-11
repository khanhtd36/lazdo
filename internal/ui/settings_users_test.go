package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func fakeUserSettings(t *testing.T) *settingsView {
	t.Helper()
	s := newSettingsView(ado.NewClient("org"), ado.ProjectInfo{ID: "p1", Name: "Demo"})
	contributors := ado.Group{Descriptor: "g-c", Name: "Contributors"}
	readers := ado.Group{Descriptor: "g-r", Name: "Readers"}
	s.update(settingsLoadedMsg{projectID: "p1", data: &ado.ProjectSettings{
		Groups: []ado.Group{contributors, {Descriptor: "g-v", Name: validUsers}, readers},
		Users: []ado.ProjectUser{
			{Name: "Jo Tanaka", Email: "jo@contoso.com", Descriptor: "u-jo", Via: []string{"Developers"}},
			{Name: "Sam Rivera", Email: "sam@contoso.com", Descriptor: "u-sam", Groups: []ado.Group{contributors, readers}, Via: []string{"Developers"}},
		},
		Errs: map[string]error{},
	}})
	s.section, s.onRight = secPermissions, true
	s.content.setItems(s.contentItems())
	return s
}

func contentText(s *settingsView) string {
	return ansi.Strip(strings.Join(s.content.view(100, 20), "\n"))
}

func TestPermissionsListGroupsThenUsers(t *testing.T) {
	s := fakeUserSettings(t)
	text := contentText(s)
	for _, want := range []string{"Groups (3)", "Users (2)", "Sam Rivera  sam@contoso.com  Contributors, Readers · via Developers", "Jo Tanaka  jo@contoso.com  via Developers"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if strings.Index(text, "Groups (3)") > strings.Index(text, "Users (2)") {
		t.Error("groups should come before users")
	}
}

func selectUser(t *testing.T, s *settingsView, name string) {
	t.Helper()
	for i, idx := range s.content.visible() {
		if u, ok := s.content.items[idx].value.(ado.ProjectUser); ok && u.Name == name {
			s.content.cursor = i
			return
		}
	}
	t.Fatalf("no user %s", name)
}

func TestRemoveUserFromProject(t *testing.T) {
	s := fakeUserSettings(t)
	selectUser(t, s, "Sam Rivera")
	_, cmd := s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, 20)
	c, ok := cmd().(showModalMsg).modal.(*confirmModal)
	if !ok {
		t.Fatal("d should ask first")
	}
	prompt := ansi.Strip(c.prompt)
	for _, want := range []string{"Remove Sam Rivera from Demo?", "They leave Contributors, Readers.", "keep access through Developers"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt %q lacks %q", prompt, want)
		}
	}

	// Someone here only through an outside group can't be removed here.
	selectUser(t, s, "Jo Tanaka")
	_, cmd = s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, 20)
	if msg, ok := cmd().(statusMsg); !ok || !strings.Contains(string(msg), "only through Developers") {
		t.Fatalf("got %#v", cmd())
	}
}

func TestAddUsersOffersGroupsWithContributorsPicked(t *testing.T) {
	s := fakeUserSettings(t)
	_, cmd := s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, 20)
	f, ok := cmd().(showModalMsg).modal.(*formModal)
	if !ok {
		t.Fatal("a should open a form")
	}
	g := f.get("group")
	if strings.Join(g.choices, ",") != "Contributors,Readers" || g.choices[g.choice] != "Contributors" {
		t.Fatalf("groups %v, picked %d", g.choices, g.choice)
	}
}

func TestUserGroupsDetail(t *testing.T) {
	s := fakeUserSettings(t)
	selectUser(t, s, "Sam Rivera")
	s.key(tea.KeyMsg{Type: tea.KeyEnter}, 20)
	if s.detail == nil || s.detail.title != "Groups of Sam Rivera" {
		t.Fatalf("enter should open the user's groups, got %+v", s.detail)
	}
	text := ansi.Strip(strings.Join(s.detail.list.view(100, 10), "\n"))
	if !strings.Contains(text, "Contributors") || !strings.Contains(text, "also through Developers") {
		t.Fatalf("detail:\n%s", text)
	}
	// a offers only the groups Sam isn't in; with none left it says so.
	_, cmd := s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, 20)
	if msg, ok := cmd().(statusMsg); !ok || !strings.Contains(string(msg), "every group already") {
		t.Fatalf("got %#v", cmd())
	}
	_, cmd = s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, 20)
	c, ok := cmd().(showModalMsg).modal.(*confirmModal)
	if !ok || !strings.Contains(c.prompt, "Remove Sam Rivera from Contributors?") || strings.Contains(c.prompt, "last group") {
		t.Fatalf("d on a group: %#v", c)
	}
}
