package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// formModal edits a settings item: checkboxes, text and number fields,
// and choices, with what will change listed before saving.

type fieldKind int

const (
	fieldBool fieldKind = iota
	fieldText
	fieldNumber
	fieldChoice
)

type formField struct {
	key, label, hint string
	kind             fieldKind
	on               bool
	input            textinput.Model
	choices          []string
	choice           int
	initial          string // value() when the form opened, for the change list
}

func (f *formField) value() string {
	switch f.kind {
	case fieldBool:
		if f.on {
			return "on"
		}
		return "off"
	case fieldChoice:
		if f.choice < len(f.choices) {
			return f.choices[f.choice]
		}
		return ""
	case fieldText, fieldNumber:
	}
	return strings.TrimSpace(f.input.Value())
}

func formBool(key, label string, on bool) *formField {
	return &formField{key: key, label: label, kind: fieldBool, on: on}
}

func formText(key, label, value, hint string) *formField {
	in := textinput.New()
	in.SetValue(value)
	in.Prompt = ""
	in.Width = 50
	in.Cursor.SetMode(cursor.CursorStatic)
	return &formField{key: key, label: label, kind: fieldText, input: in, hint: hint}
}

func formNumber(key, label string, value int, hint string) *formField {
	f := formText(key, label, strconv.Itoa(value), hint)
	f.kind = fieldNumber
	f.input.Width = 10
	return f
}

func formChoice(key, label string, choices []string, choice int) *formField {
	return &formField{key: key, label: label, kind: fieldChoice, choices: choices, choice: max(0, min(choice, len(choices)-1))}
}

type formModal struct {
	title  string
	fields []*formField
	focus  int // len(fields) is the Save button
	// save checks the form and returns the write to run, or why it can't.
	save func(f *formModal) (tea.Cmd, string)
}

func newForm(title string, fields []*formField, save func(*formModal) (tea.Cmd, string)) *formModal {
	f := &formModal{title: title, fields: fields, save: save}
	for _, fd := range fields {
		fd.initial = fd.value()
	}
	f.setFocus(0)
	return f
}

func (f *formModal) get(key string) *formField {
	for _, fd := range f.fields {
		if fd.key == key {
			return fd
		}
	}
	return &formField{}
}

// changes lists each field that differs from when the form opened.
func (f *formModal) changes() []string {
	var out []string
	for _, fd := range f.fields {
		if v := fd.value(); v != fd.initial {
			out = append(out, fd.label+": "+cmpOr(fd.initial, "(empty)")+" → "+cmpOr(v, "(empty)"))
		}
	}
	return out
}

func (f *formModal) setFocus(i int) {
	n := len(f.fields) + 1
	f.focus = (i + n) % n
	for j, fd := range f.fields {
		if j == f.focus && (fd.kind == fieldText || fd.kind == fieldNumber) {
			fd.input.Focus()
		} else {
			fd.input.Blur()
		}
	}
}

// typingField is the focused text or number field, if any: keys there are
// typed, not commands.
func (f *formModal) typingField() *formField {
	if f.focus < len(f.fields) {
		if fd := f.fields[f.focus]; fd.kind == fieldText || fd.kind == fieldNumber {
			return fd
		}
	}
	return nil
}

func (f *formModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc":
		if len(f.changes()) > 0 {
			return &confirmModal{prompt: "Discard your changes?", back: f}, nil
		}
		return nil, nil
	case "ctrl+s":
		return f.submit()
	case "tab", "down":
		f.setFocus(f.focus + 1)
		return f, nil
	case "shift+tab", "up":
		f.setFocus(f.focus - 1)
		return f, nil
	}
	if fd := f.typingField(); fd != nil {
		if k == "enter" {
			f.setFocus(f.focus + 1)
			return f, nil
		}
		if fd.kind == fieldNumber && msg.Type == tea.KeyRunes && strings.Trim(string(msg.Runes), "0123456789") != "" {
			return f, nil // digits only
		}
		var cmd tea.Cmd
		fd.input, cmd = fd.input.Update(msg)
		return f, cmd
	}
	if f.focus == len(f.fields) { // Save
		switch k {
		case "enter", " ":
			return f.submit()
		case "j":
			f.setFocus(f.focus + 1)
		case "k":
			f.setFocus(f.focus - 1)
		}
		return f, nil
	}
	fd := f.fields[f.focus]
	switch k {
	case "j":
		f.setFocus(f.focus + 1)
	case "k":
		f.setFocus(f.focus - 1)
	case " ", "enter", "x":
		if fd.kind == fieldBool {
			fd.on = !fd.on
		} else {
			fd.choice = (fd.choice + 1) % len(fd.choices)
		}
	case "l", "right":
		if fd.kind == fieldChoice {
			fd.choice = (fd.choice + 1) % len(fd.choices)
		}
	case "h", "left":
		if fd.kind == fieldChoice {
			fd.choice = (fd.choice + len(fd.choices) - 1) % len(fd.choices)
		}
	}
	return f, nil
}

func (f *formModal) submit() (modal, tea.Cmd) {
	if len(f.changes()) == 0 && !strings.HasPrefix(f.title, "New ") {
		return nil, statusCmd("nothing changed")
	}
	cmd, refused := f.save(f)
	if refused != "" {
		return f, statusCmd(refused)
	}
	return nil, cmd
}

func (f *formModal) view(width int) string {
	w := min(90, max(40, width-12))
	mark := func(i int) string {
		if i == f.focus {
			return styleSelected.Render("› ")
		}
		return "  "
	}
	lines := []string{styleSection.Render(f.title), ""}
	for i, fd := range f.fields {
		var line string
		switch fd.kind {
		case fieldBool:
			box := "[ ]"
			if fd.on {
				box = "[x]"
			}
			line = box + " " + fd.label
		case fieldChoice:
			line = fd.label + "  ‹ " + fd.value() + " ›"
		case fieldText, fieldNumber:
			line = fd.label + "  " + fd.input.View()
		}
		if fd.hint != "" && i == f.focus {
			line += styleDim.Render("  " + fd.hint)
		}
		lines = append(lines, truncate(mark(i)+line, w))
	}
	if ch := f.changes(); len(ch) > 0 {
		lines = append(lines, "", styleYellow.Render("Changes"))
		for _, c := range ch {
			lines = append(lines, truncate("  "+c, w))
		}
	}
	lines = append(lines, "", mark(len(f.fields))+"[ Save ]", "",
		styleDim.Render("tab/↑↓ move · space toggle · ←/→ choose · ctrl+s save · esc cancel"))
	return styleModal.Render(strings.Join(lines, "\n"))
}
