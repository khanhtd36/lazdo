package ui

import (
	"context"
	"fmt"
	"maps"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Editing a group's project-level permissions. Changes wait as pending
// until ctrl+s, which lists them and asks before saving them together: a
// permission changes what people can do, so no single key press does.

type permEdit struct {
	group   ado.Group
	base    []ado.Permission
	pending map[int]string // bit → the state it will have
}

var permCycle = map[string]string{"Not set": "Allow", "Allow": "Deny", "Deny": "Not set"}

// explicit is the state set on the group itself; an inherited one counts
// as not set.
func explicit(p ado.Permission) string {
	if p.Inherited {
		return "Not set"
	}
	return p.State
}

func (e *permEdit) state(p ado.Permission) string {
	if s, ok := e.pending[p.Bit]; ok {
		return s
	}
	return explicit(p)
}

// openPermissions shows a group's permissions, ready to edit.
func (s *settingsView) openPermissions(g ado.Group) {
	perms, ok := s.data.Permissions[g.SID()]
	if !ok {
		perms = s.data.Permissions[""] // nothing set on the group: every permission not set
	}
	s.detail = &settingsDetail{title: "Project permissions of " + g.Name}
	s.detail.perm = &permEdit{group: g, base: perms, pending: map[int]string{}}
	s.detail.list.setItems(s.permItems())
}

func (s *settingsView) permItems() []pickItem {
	e := s.detail.perm
	if len(e.base) == 0 {
		return []pickItem{settingsRow(styleDim.Render("Couldn't read the project's permissions."), nil)}
	}
	items := make([]pickItem, 0, len(e.base))
	for _, p := range e.base {
		now := e.state(p)
		text := stateText(now)
		switch {
		case now != explicit(p):
			text = stateText(explicit(p)) + styleYellow.Render(" → ") + text
		case p.Inherited:
			text += styleDim.Render(" (inherited " + p.State + ")")
		}
		items = append(items, settingsRow(fit(p.Name, 44)+" "+text, p))
	}
	return items
}

func stateText(s string) string {
	switch s {
	case "Allow":
		return styleGreen.Render("Allow")
	case "Deny":
		return styleRed.Render("Deny")
	}
	return styleDim.Render("Not set")
}

// permKey cycles (space), saves (ctrl+s) and discards (esc) changes.
func (s *settingsView) permKey(k string) (bool, tea.Cmd) {
	e := s.detail.perm
	switch k {
	case " ", "enter":
		it, ok := s.detail.list.selected()
		p, isPerm := it.value.(ado.Permission)
		if !ok || !isPerm {
			return true, nil
		}
		next := permCycle[e.state(p)]
		if next == explicit(p) {
			delete(e.pending, p.Bit)
		} else {
			e.pending[p.Bit] = next
		}
		cursor := s.detail.list.cursor
		s.detail.list.setItems(s.permItems())
		s.detail.list.cursor = cursor
		return true, nil
	case "ctrl+s":
		if len(e.pending) == 0 {
			return true, statusCmd("nothing changed")
		}
		return true, showModal(newConfirm(s.permPrompt(), s.savePermissions()))
	case "esc", "h", "left":
		if len(e.pending) > 0 {
			return true, showModal(&confirmModal{
				prompt: fmt.Sprintf("Discard %d permission %s?", len(e.pending), plural(len(e.pending), "change", "changes")),
				onYes:  func() tea.Msg { return permDiscardMsg{} },
			})
		}
	}
	return false, nil
}

type (
	permDiscardMsg    struct{}
	settingsReloadMsg struct{} // read the settings again, after a save settles
)

// applySaved shows saved changes as set without waiting for the server,
// whose access list may still read back the old values.
func (s *settingsView) applySaved(e *permEdit) {
	base := make([]ado.Permission, len(e.base))
	for i, p := range e.base {
		if now, ok := e.pending[p.Bit]; ok {
			p.State, p.Inherited = now, false
		}
		base[i] = p
	}
	e.base, e.pending = base, map[int]string{}
	s.data.Permissions[e.group.SID()] = base
	cursor := s.detail.list.cursor
	s.detail.list.setItems(s.permItems())
	s.detail.list.cursor = cursor
}

func (s *settingsView) permPrompt() string {
	e := s.detail.perm
	lines := []string{"Change " + e.group.Name + "'s project permissions?", ""}
	for _, p := range e.base {
		if now, ok := e.pending[p.Bit]; ok {
			lines = append(lines, fmt.Sprintf("  %s: %s → %s", p.Name, explicit(p), now))
		}
	}
	if strings.EqualFold(e.group.Name, "Project Administrators") {
		lines = append(lines, "", "This is the Project Administrators group: denying it permissions can lock everyone out of these settings.")
	}
	return strings.Join(lines, "\n")
}

// savePermissions writes only the changed permissions, one each, so the
// group's other permissions are never rewritten from a possibly stale read.
func (s *settingsView) savePermissions() tea.Cmd {
	e := s.detail.perm
	changes := maps.Clone(e.pending)
	client, project, sid, name, n := s.client, s.project.ID, e.group.SID(), e.group.Name, len(changes)
	return s.write(fmt.Sprintf("saved %d permission %s for %s", n, plural(n, "change", "changes"), name), func(ctx context.Context) error {
		if sid == "" {
			return fmt.Errorf("couldn't work out %s's security ID", name)
		}
		for bit, state := range changes {
			if err := client.SetProjectPermission(ctx, project, sid, bit, state); err != nil {
				return err
			}
		}
		return nil
	})
}
