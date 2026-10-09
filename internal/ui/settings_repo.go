package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Editing repositories and variable groups in the Settings tab.

// --- Typed confirmation ---

// nameConfirm asks for a name to be typed before something that can't
// easily be undone, as the web does before deleting a repository.
type nameConfirm struct {
	prompt, want string
	input        textinput.Model
	onYes        tea.Cmd
}

func newNameConfirm(prompt, want string, onYes tea.Cmd) *nameConfirm {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = want
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Focus()
	return &nameConfirm{prompt: prompt, want: want, input: in, onYes: onYes}
}

func (n *nameConfirm) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "enter":
		if n.input.Value() != n.want {
			return n, statusCmd("type " + n.want + " exactly to confirm")
		}
		return nil, n.onYes
	}
	var cmd tea.Cmd
	n.input, cmd = n.input.Update(msg)
	return n, cmd
}

func (n *nameConfirm) view(int) string {
	return styleModal.Render(strings.Join([]string{
		styleRed.Render(n.prompt), "",
		"Type " + styleTitle.Render(n.want) + " to confirm:",
		n.input.View(), "",
		styleDim.Render("enter confirm · esc cancel"),
	}, "\n"))
}

// --- Repositories ---

func (s *settingsView) repoKey(k string) (bool, tea.Cmd) {
	it, ok := s.content.selected()
	r, isRepo := it.value.(ado.Repo)
	client, project := s.client, s.project
	switch k {
	case "n":
		f := newForm("New repository", []*formField{formText("name", "Name", "", "")}, func(f *formModal) (tea.Cmd, string) {
			name := f.get("name").value()
			if name == "" {
				return nil, "a repository needs a name"
			}
			return s.write("created repository "+name, func(ctx context.Context) error {
				return client.CreateRepo(ctx, project.ID, name)
			}), ""
		})
		return true, showModal(f)
	case "e":
		if !ok || !isRepo {
			return true, nil
		}
		f := newForm("Edit repository "+r.Name, []*formField{
			formText("name", "Name", r.Name, ""),
			formText("branch", "Default branch", r.DefaultBranchName(), "must exist"),
		}, func(f *formModal) (tea.Cmd, string) { return s.saveRepo(f, r) })
		return true, showModal(f)
	case "d":
		if !ok || !isRepo {
			return true, nil
		}
		prompt := "Delete repository " + r.Name + "?\nIt goes to the project's recycle bin, restorable for 30 days."
		return true, showModal(newNameConfirm(prompt, r.Name, s.write("deleted repository "+r.Name, func(ctx context.Context) error {
			return client.DeleteRepo(ctx, r)
		})))
	}
	return false, nil
}

func (s *settingsView) saveRepo(f *formModal, r ado.Repo) (tea.Cmd, string) {
	name, branch := f.get("name").value(), strings.TrimPrefix(f.get("branch").value(), "refs/heads/")
	if name == "" || branch == "" {
		return nil, "a repository needs a name and a default branch"
	}
	if name == r.Name {
		name = ""
	}
	if branch == r.DefaultBranchName() {
		branch = ""
	}
	client := s.client
	return s.write("saved repository "+cmpOr(name, r.Name), func(ctx context.Context) error {
		if branch != "" {
			found, err := client.RepoBranchExists(ctx, r, branch)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("%s has no branch %s", r.Name, branch)
			}
		}
		return client.UpdateRepo(ctx, r, name, branch)
	}), ""
}

// --- Variable groups ---

// varGroupRow and varRow are the Variable groups list's rows: a group, and
// one of its variables.
type (
	varGroupRow struct{ group ado.VariableGroup }
	varRow      struct {
		group ado.VariableGroup
		v     ado.Variable
	}
)

func varGroupItems(groups []ado.VariableGroup) []pickItem {
	var items []pickItem
	for _, g := range groups {
		items = append(items, settingsRow(styleHeader.Render(g.Name)+styleDim.Render("  "+g.Description), varGroupRow{g}))
		for _, v := range g.Variables {
			value := v.Value
			if v.Secret {
				value = styleDim.Render("•••••• (secret)")
			}
			items = append(items, settingsRow("  "+v.Name+styleDim.Render(" = ")+value, varRow{g, v}))
		}
	}
	return items
}

func (s *settingsView) varKey(k string) (bool, tea.Cmd) {
	it, ok := s.content.selected()
	var group ado.VariableGroup
	var v *ado.Variable
	switch row := it.value.(type) {
	case varGroupRow:
		group = row.group
	case varRow:
		group, v = row.group, &row.v
	default:
		ok = false
	}
	switch k {
	case "N":
		return true, showModal(s.varGroupForm(nil))
	case "n":
		if ok {
			return true, showModal(s.variableForm(group, nil))
		}
	case "e":
		switch {
		case ok && v != nil:
			return true, showModal(s.variableForm(group, v))
		case ok:
			return true, showModal(s.varGroupForm(&group))
		}
	case "d":
		client, project := s.client, s.project.ID
		switch {
		case ok && v != nil:
			name := v.Name
			return true, showModal(newConfirm("Delete variable "+name+" from "+group.Name+"?", s.write("deleted "+name, func(ctx context.Context) error {
				return client.EditVariableGroup(ctx, project, group.ID, func(g map[string]any) {
					vars, _ := g["variables"].(map[string]any)
					delete(vars, name)
				})
			})))
		case ok:
			return true, showModal(newConfirm("Delete variable group "+group.Name+" and its variables?", s.write("deleted "+group.Name, func(ctx context.Context) error {
				return client.DeleteVariableGroup(ctx, project, group.ID)
			})))
		}
	default:
		return false, nil
	}
	return true, nil
}

func (s *settingsView) varGroupForm(g *ado.VariableGroup) *formModal {
	creating := g == nil
	if creating {
		g = &ado.VariableGroup{}
	}
	title := "Edit variable group " + g.Name
	if creating {
		title = "New variable group"
	}
	fields := []*formField{formText("name", "Name", g.Name, ""), formText("description", "Description", g.Description, "")}
	client, project, id := s.client, s.project, g.ID
	return newForm(title, fields, func(f *formModal) (tea.Cmd, string) {
		name, desc := f.get("name").value(), f.get("description").value()
		if name == "" {
			return nil, "a variable group needs a name"
		}
		if creating {
			return s.write("created variable group "+name, func(ctx context.Context) error {
				return client.CreateVariableGroup(ctx, project, name, desc)
			}), ""
		}
		return s.write("saved variable group "+name, func(ctx context.Context) error {
			return client.EditVariableGroup(ctx, project.ID, id, func(g map[string]any) {
				g["name"], g["description"] = name, desc
				refs, _ := g["variableGroupProjectReferences"].([]any)
				for _, r := range refs { // the project's own copy of the name
					if ref, ok := r.(map[string]any); ok {
						ref["name"], ref["description"] = name, desc
					}
				}
			})
		}), ""
	})
}

// variableForm edits v in group, or adds one when v is nil. A secret's value
// is never shown; left empty, the stored secret stays.
func (s *settingsView) variableForm(group ado.VariableGroup, v *ado.Variable) *formModal {
	creating := v == nil
	if creating {
		v = &ado.Variable{}
	}
	value, hint := v.Value, ""
	if v.Secret {
		value, hint = "", "empty keeps the current secret"
	}
	fields := []*formField{
		formText("name", "Name", v.Name, ""),
		formText("value", "Value", value, hint),
		formBool("secret", "Secret (hidden once saved)", v.Secret),
	}
	title := "Edit " + v.Name + " in " + group.Name
	if creating {
		title = "New variable in " + group.Name
	}
	client, project, old := s.client, s.project.ID, *v
	return newForm(title, fields, func(f *formModal) (tea.Cmd, string) {
		// The value as typed: spaces can matter in a variable.
		name, value, secret := f.get("name").value(), f.get("value").input.Value(), f.get("secret").on
		if name == "" {
			return nil, "a variable needs a name"
		}
		if name != old.Name {
			for _, other := range group.Variables {
				if strings.EqualFold(other.Name, name) {
					return nil, group.Name + " already has " + other.Name
				}
			}
		}
		keepSecret := old.Secret && secret && value == "" && !creating
		if value == "" && !keepSecret && secret {
			return nil, "a new secret needs a value"
		}
		return s.write("saved "+name, func(ctx context.Context) error {
			return client.EditVariableGroup(ctx, project, group.ID, func(g map[string]any) {
				vars, _ := g["variables"].(map[string]any)
				if vars == nil {
					vars = map[string]any{}
					g["variables"] = vars
				}
				entry := map[string]any{"value": value, "isSecret": secret}
				if keepSecret {
					entry = map[string]any{"isSecret": true} // no value: the server keeps the stored one
				}
				if !creating && old.Name != name {
					delete(vars, old.Name)
				}
				vars[name] = entry
			})
		}), ""
	})
}
