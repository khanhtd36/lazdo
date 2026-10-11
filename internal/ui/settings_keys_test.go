package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// a adds to the list on screen in every Settings section; n is left to
// mean next match, as everywhere else.
func TestSettingsAddsWithA(t *testing.T) {
	s := fakeSettings(t)
	s.onRight = true
	for _, sec := range []settingsSection{secPolicies, secRepos, secTeams, secGroups} {
		s.section = sec
		s.content.setItems(s.contentItems())
		if _, cmd := s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, 20); cmd == nil {
			t.Errorf("%s: a should open a form", sec.title())
		} else if _, ok := cmd().(showModalMsg); !ok {
			t.Errorf("%s: a gave %T", sec.title(), cmd())
		}
		if _, cmd := s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, 20); cmd != nil {
			if _, ok := cmd().(showModalMsg); ok {
				t.Errorf("%s: n should no longer add", sec.title())
			}
		}
	}
	s.section = secVarGroups
	s.content.setItems(s.contentItems())
	if _, cmd := s.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}, 20); cmd == nil {
		t.Error("A should add a variable group")
	}
}
