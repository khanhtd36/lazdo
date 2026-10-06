package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// prEditor edits a pull request's title and description (E). Saving sends
// only what changed and overwrites whatever is on the server.
type prEditor struct {
	d                   *detailModel
	title               textinput.Model
	desc                textarea.Model
	onDesc              bool
	initTitle, initDesc string
}

func (d *detailModel) newPREditor() *prEditor {
	w := min(100, max(40, d.width-10))
	title := textinput.New()
	title.CharLimit = 0
	title.Width = w - 2
	title.SetValue(d.pr.Title)
	title.Cursor.SetMode(cursor.CursorStatic)
	title.Focus()
	desc := textarea.New()
	desc.ShowLineNumbers = false
	desc.CharLimit = 0
	desc.SetWidth(w)
	desc.SetHeight(min(16, max(6, d.height-14)))
	desc.SetValue(d.data.Description)
	desc.Cursor.SetMode(cursor.CursorStatic)
	return &prEditor{d: d, title: title, desc: desc, initTitle: d.pr.Title, initDesc: d.data.Description}
}

func (p *prEditor) changed() bool {
	return p.title.Value() != p.initTitle || p.desc.Value() != p.initDesc
}

func (p *prEditor) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if p.changed() {
			return &confirmModal{prompt: "Discard your changes to the title and description?", back: p}, nil
		}
		return nil, nil
	case "tab", "shift+tab":
		p.onDesc = !p.onDesc
		if p.onDesc {
			p.title.Blur()
			p.desc.Focus()
		} else {
			p.desc.Blur()
			p.title.Focus()
		}
		return p, nil
	case "ctrl+s":
		return p.save()
	case "ctrl+e":
		return p, editExternally(joinEditForm(p.title.Value(), p.desc.Value()))
	}
	var cmd tea.Cmd
	if p.onDesc {
		p.desc, cmd = p.desc.Update(msg)
	} else if msg.String() != "enter" { // the title is one line
		p.title, cmd = p.title.Update(msg)
	}
	return p, cmd
}

func (p *prEditor) editorDone(msg editorDoneMsg) tea.Cmd {
	if msg.err != nil {
		return statusCmd("error: editor: " + msg.err.Error())
	}
	title, desc := splitEditForm(msg.content)
	p.title.SetValue(title)
	p.desc.SetValue(desc)
	return nil
}

// edits is what saving sends: nil for an unchanged field, or why it can't.
func (p *prEditor) edits() (newTitle, newDesc *string, refused string) {
	title := strings.TrimSpace(p.title.Value())
	desc := strings.TrimRight(p.desc.Value(), " \n")
	switch {
	case title == "":
		return nil, nil, "a pull request needs a title"
	case utf8.RuneCountInString(title) > ado.MaxTitleLength:
		return nil, nil, fmt.Sprintf("the title is over %d characters", ado.MaxTitleLength)
	case utf8.RuneCountInString(desc) > ado.MaxDescriptionLength:
		return nil, nil, fmt.Sprintf("the description is over %d characters", ado.MaxDescriptionLength)
	}
	if title != p.initTitle {
		newTitle = &title
	}
	if desc != strings.TrimRight(p.initDesc, " \n") {
		newDesc = &desc
	}
	return newTitle, newDesc, ""
}

func (p *prEditor) save() (modal, tea.Cmd) {
	newTitle, newDesc, refused := p.edits()
	if refused != "" {
		return p, statusCmd(refused)
	}
	var saved []string
	if newTitle != nil {
		saved = append(saved, "title")
	}
	if newDesc != nil {
		saved = append(saved, "description")
	}
	if len(saved) == 0 {
		return nil, statusCmd("nothing changed")
	}
	d := p.d
	return nil, d.act(strings.Join(saved, " and ")+" saved", false, func(ctx context.Context) error {
		return d.client.EditPR(ctx, d.pr, newTitle, newDesc)
	})
}

func (p *prEditor) view(int) string {
	counter := func(s string, limit int) string {
		n := utf8.RuneCountInString(s)
		text := fmt.Sprintf("%d/%d", n, limit)
		if n > limit {
			return styleRed.Render(text)
		}
		return styleDim.Render(text)
	}
	label := func(name string, focused bool) string {
		if focused {
			return styleSelected.Render("› ") + name
		}
		return "  " + styleDim.Render(name)
	}
	return styleModal.Render(strings.Join([]string{
		styleSection.Render(fmt.Sprintf("Edit !%d", p.d.pr.ID)),
		"",
		label("Title", !p.onDesc) + "  " + counter(strings.TrimSpace(p.title.Value()), ado.MaxTitleLength),
		"  " + p.title.View(),
		"",
		label("Description (markdown)", p.onDesc) + "  " + counter(p.desc.Value(), ado.MaxDescriptionLength),
		p.desc.View(),
		"",
		styleDim.Render("ctrl+s save · tab next field · ctrl+e open in $EDITOR · esc cancel"),
	}, "\n"))
}

// joinEditForm writes title and description like a commit message: the
// title, a blank line, the description.
func joinEditForm(title, desc string) string {
	return strings.TrimSpace(title) + "\n\n" + desc
}

// splitEditForm reads joinEditForm's format back: the first line is the
// title, and the description starts after the blank lines that follow it.
func splitEditForm(s string) (title, desc string) {
	title, rest, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(title), strings.TrimRight(strings.TrimLeft(rest, "\n"), "\n")
}
