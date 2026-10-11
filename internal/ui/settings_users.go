package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// The Users half of Settings › Permissions: the people in the project's
// groups, which groups they are in, and adding or removing them.

// validUsers is the group Azure DevOps fills on its own with every other
// project group; nobody is added to it.
const validUsers = "Project Valid Users"

// permissionItems are the project's groups, then its users.
func (s *settingsView) permissionItems() []pickItem {
	d := s.data
	items := make([]pickItem, 0, len(d.Groups)+len(d.Users)+2)
	items = append(items, settingsHeader(fmt.Sprintf("Groups (%d)", len(d.Groups))))
	for _, g := range d.Groups {
		items = append(items, settingsRow(g.Name+styleDim.Render("  "+firstLine(g.Description)), g))
	}
	if err := d.Errs["users"]; err != nil {
		return append(items, settingsHeader("Users"), settingsRow(styleRed.Render("error: "+err.Error()), nil))
	}
	items = append(items, settingsHeader(fmt.Sprintf("Users (%d)", len(d.Users))))
	for _, u := range d.Users {
		items = append(items, settingsRow(userRow(u), u))
	}
	return items
}

// userRow: "Sam Rivera  sam@contoso.com  Contributors, Readers · via Developers".
func userRow(u ado.ProjectUser) string {
	names := make([]string, 0, len(u.Groups))
	for _, g := range u.Groups {
		names = append(names, g.Name)
	}
	text := u.Name + styleDim.Render("  "+u.Email) + "  " + strings.Join(names, ", ")
	if len(u.Via) > 0 {
		sep := " · "
		if len(names) == 0 {
			sep = ""
		}
		text += styleDim.Render(sep + "via " + strings.Join(u.Via, ", "))
	}
	return text
}

// openUser lists the project groups a user is directly in.
func (s *settingsView) openUser(u ado.ProjectUser) {
	s.detail = &settingsDetail{title: "Groups of " + u.Name, user: u.Descriptor}
	s.detail.list.setItems(userGroupItems(u))
}

func userGroupItems(u ado.ProjectUser) []pickItem {
	items := make([]pickItem, 0, len(u.Groups)+1)
	for _, g := range u.Groups {
		items = append(items, settingsRow(g.Name+styleDim.Render("  "+firstLine(g.Description)), g))
	}
	if len(u.Via) > 0 {
		items = append(items, settingsHeader(styleDim.Render("also through "+strings.Join(u.Via, ", ")+", outside the project")))
	}
	return items
}

// findUser is the user with the descriptor in the loaded settings.
func (s *settingsView) findUser(desc string) (ado.ProjectUser, bool) {
	for _, u := range s.data.Users {
		if u.Descriptor == desc {
			return u, true
		}
	}
	return ado.ProjectUser{}, false
}

// refreshUser shows a user's groups as last loaded.
func (s *settingsView) refreshUser() {
	d := s.detail
	if u, ok := s.findUser(d.user); ok {
		d.list.setItems(userGroupItems(u))
		return
	}
	d.list.setItems(nil) // no longer in any project group
}

// groupChoice offers the project's groups a user can join, Contributors
// first picked; except lists groups to leave out.
func (s *settingsView) groupChoice(except []ado.Group) (*formField, []ado.Group) {
	var groups []ado.Group
	names := []string{}
	pick := 0
	for _, g := range s.data.Groups {
		if g.Name == validUsers || slices.ContainsFunc(except, func(e ado.Group) bool { return e.Descriptor == g.Descriptor }) {
			continue
		}
		if g.Name == "Contributors" {
			pick = len(groups)
		}
		groups = append(groups, g)
		names = append(names, g.Name)
	}
	return formChoice("group", "Group", names, pick), groups
}

// permissionKey adds people to the project (a) and removes a user from
// it (d) on the Permissions content.
func (s *settingsView) permissionKey(k string) (bool, tea.Cmd) {
	client := s.client
	switch k {
	case "a":
		choice, groups := s.groupChoice(nil)
		if len(groups) == 0 {
			return true, statusCmd("no group to add people to")
		}
		f := newForm("Add users to "+s.project.Name, []*formField{
			formText("people", "People", "", "names or emails, comma separated"), choice,
		}, func(f *formModal) (tea.Cmd, string) {
			names := f.get("people").value()
			if names == "" {
				return nil, "name someone to add"
			}
			g := groups[f.get("group").choice]
			return s.write("added "+names+" to "+g.Name, func(ctx context.Context) error {
				ids, err := resolvePeople(ctx, client, names)
				if err != nil {
					return err
				}
				for _, id := range ids {
					who, err := client.Descriptor(ctx, id)
					if err != nil {
						return err
					}
					if err := client.AddMember(ctx, who, g.Descriptor); err != nil {
						return err
					}
				}
				return nil
			}), ""
		})
		return true, showModal(f)
	case "d":
		it, ok := s.content.selected()
		u, isUser := it.value.(ado.ProjectUser)
		if !ok || !isUser {
			return false, nil
		}
		if len(u.Groups) == 0 {
			return true, statusCmd(u.Name + " is here only through " + strings.Join(u.Via, ", ") + "; change that group instead")
		}
		names := make([]string, 0, len(u.Groups))
		for _, g := range u.Groups {
			names = append(names, g.Name)
		}
		prompt := fmt.Sprintf("Remove %s from %s?\nThey leave %s.", u.Name, s.project.Name, strings.Join(names, ", "))
		if len(u.Via) > 0 {
			prompt += "\n" + styleYellow.Render("They keep access through "+strings.Join(u.Via, ", ")+", managed outside the project.")
		}
		return true, showModal(newConfirm(prompt, s.write("removed "+u.Name+" from "+s.project.Name, func(ctx context.Context) error {
			for _, g := range u.Groups {
				if err := client.RemoveMember(ctx, u.Descriptor, g.Descriptor); err != nil {
					return err
				}
			}
			return nil
		})))
	}
	return false, nil
}

// userKey adds the open user to a group (a) or removes them from the
// selected one (d).
func (s *settingsView) userKey(k string) (bool, tea.Cmd) {
	u, found := s.findUser(s.detail.user)
	if !found {
		return false, nil
	}
	client := s.client
	switch k {
	case "a":
		choice, groups := s.groupChoice(u.Groups)
		if len(groups) == 0 {
			return true, statusCmd(u.Name + " is in every group already")
		}
		f := newForm("Add "+u.Name+" to a group", []*formField{choice}, func(f *formModal) (tea.Cmd, string) {
			g := groups[f.get("group").choice]
			return s.write("added "+u.Name+" to "+g.Name, func(ctx context.Context) error {
				return client.AddMember(ctx, u.Descriptor, g.Descriptor)
			}), ""
		})
		return true, showModal(f)
	case "d":
		it, ok := s.detail.list.selected()
		g, isGroup := it.value.(ado.Group)
		if !ok || !isGroup {
			return true, nil
		}
		prompt := fmt.Sprintf("Remove %s from %s?", u.Name, g.Name)
		if len(u.Groups) == 1 && len(u.Via) == 0 {
			prompt += "\n" + styleYellow.Render("It is their last group: they lose access to "+s.project.Name+".")
		}
		return true, showModal(newConfirm(prompt, s.write("removed "+u.Name+" from "+g.Name, func(ctx context.Context) error {
			return client.RemoveMember(ctx, u.Descriptor, g.Descriptor)
		})))
	}
	return false, nil
}
