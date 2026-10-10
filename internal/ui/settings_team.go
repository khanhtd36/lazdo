package ui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Editing teams, security groups and their members in the Settings tab.

// nameDescForm edits a name and a description: teams and groups share it.
func nameDescForm(title, name, desc string, save func(name, desc string) tea.Cmd) *formModal {
	fields := []*formField{formText("name", "Name", name, ""), formText("description", "Description", desc, "")}
	return newForm(title, fields, func(f *formModal) (tea.Cmd, string) {
		n := f.get("name").value()
		if n == "" {
			return nil, "a name is required"
		}
		return save(n, f.get("description").value()), ""
	})
}

func (s *settingsView) teamKey(k string) (bool, tea.Cmd) {
	it, ok := s.content.selected()
	t, isTeam := it.value.(ado.Team)
	client, project := s.client, s.project.ID
	switch k {
	case "n":
		return true, showModal(nameDescForm("New team", "", "", func(name, desc string) tea.Cmd {
			return s.write("created team "+name, func(ctx context.Context) error { return client.CreateTeam(ctx, project, name, desc) })
		}))
	case "e":
		if ok && isTeam {
			return true, showModal(nameDescForm("Edit team "+t.Name, t.Name, t.Description, func(name, desc string) tea.Cmd {
				return s.write("saved team "+name, func(ctx context.Context) error { return client.UpdateTeam(ctx, project, t.ID, name, desc) })
			}))
		}
	case "d":
		if ok && isTeam {
			return true, showModal(newConfirm("Delete team "+t.Name+"?", s.write("deleted team "+t.Name, func(ctx context.Context) error {
				return client.DeleteTeam(ctx, project, t.ID)
			})))
		}
	default:
		return false, nil
	}
	return true, nil
}

func (s *settingsView) groupKey(k string) (bool, tea.Cmd) {
	it, ok := s.content.selected()
	g, isGroup := it.value.(ado.Group)
	client, project := s.client, s.project.ID
	switch k {
	case "n":
		return true, showModal(nameDescForm("New security group", "", "", func(name, desc string) tea.Cmd {
			return s.write("created group "+name, func(ctx context.Context) error { return client.CreateGroup(ctx, project, name, desc) })
		}))
	case "e":
		if ok && isGroup {
			return true, showModal(nameDescForm("Edit group "+g.Name, g.Name, g.Description, func(name, desc string) tea.Cmd {
				return s.write("saved group "+name, func(ctx context.Context) error { return client.UpdateGroup(ctx, g.Descriptor, name, desc) })
			}))
		}
	case "d":
		if ok && isGroup {
			prompt := "Delete security group " + g.Name + "?\nIts members lose whatever it granted them."
			return true, showModal(newNameConfirm(prompt, g.Name, s.write("deleted group "+g.Name, func(ctx context.Context) error {
				return client.DeleteGroup(ctx, g.Descriptor)
			})))
		}
	default:
		return false, nil
	}
	return true, nil
}

// memberKey adds (a) and removes (d) members in a team's or group's member
// list.
func (s *settingsView) memberKey(k string) (bool, tea.Cmd) {
	d := s.detail
	client := s.client
	container := func(ctx context.Context) (string, error) {
		if d.group != "" {
			return d.group, nil
		}
		return client.Descriptor(ctx, d.team) // a team is a group underneath
	}
	switch k {
	case "a":
		f := newForm("Add to "+strings.TrimPrefix(d.title, "Members of "), []*formField{
			formText("people", "People or groups", "", "names or emails, comma separated"),
		}, func(f *formModal) (tea.Cmd, string) {
			names := f.get("people").value()
			if names == "" {
				return nil, "name someone to add"
			}
			return s.write("added "+names, func(ctx context.Context) error {
				to, err := container(ctx)
				if err != nil {
					return err
				}
				ids, err := resolvePeople(ctx, client, names)
				if err != nil {
					return err
				}
				for _, id := range ids {
					who, err := client.Descriptor(ctx, id)
					if err != nil {
						return err
					}
					if err := client.AddMember(ctx, who, to); err != nil {
						return err
					}
				}
				return nil
			}), ""
		})
		return true, showModal(f)
	case "d":
		it, ok := d.list.selected()
		m, isMember := it.value.(ado.Member)
		if !ok || !isMember {
			return true, nil
		}
		prompt := fmt.Sprintf("Remove %s from %s?", m.Name, strings.TrimPrefix(d.title, "Members of "))
		return true, showModal(newConfirm(prompt, s.write("removed "+m.Name, func(ctx context.Context) error {
			from, err := container(ctx)
			if err != nil {
				return err
			}
			who := m.Descriptor
			if who == "" {
				if who, err = client.Descriptor(ctx, m.ID); err != nil {
					return err
				}
			}
			return client.RemoveMember(ctx, who, from)
		})))
	}
	return false, nil
}
