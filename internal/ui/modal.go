package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modal is a popup that takes all keys until it closes by returning nil.
type modal interface {
	update(msg tea.KeyMsg) (modal, tea.Cmd)
	view(width int) string
}

var styleModal = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(accent).Padding(0, 1)

// --- Menu ---

type menuItem struct {
	label    string
	disabled string // reason the item is unavailable; empty when enabled
	run      func() (modal, tea.Cmd)
}

type menuModal struct {
	title  string
	items  []menuItem
	cursor int
	// keys are extra keys acting on the item under the cursor, named in hint.
	keys map[string]func(i int) (modal, tea.Cmd)
	hint string
}

func (m *menuModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	last := len(m.items) - 1
	switch msg.String() {
	case "esc":
		return nil, nil
	case "j", "down":
		m.cursor = min(m.cursor+1, last)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "ctrl+d", "pgdown":
		m.cursor = min(m.cursor+max(1, len(m.items)/2), last)
	case "ctrl+u", "pgup":
		m.cursor = max(m.cursor-max(1, len(m.items)/2), 0)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = last
	case "enter":
		it := m.items[m.cursor]
		if it.disabled != "" {
			return m, statusCmd(it.disabled)
		}
		return it.run()
	default:
		if f, ok := m.keys[msg.String()]; ok && len(m.items) > 0 {
			return f(m.cursor)
		}
	}
	return m, nil
}

func (m *menuModal) view(int) string {
	lines := make([]string, 0, len(m.items)+4)
	lines = append(lines, styleSection.Render(m.title), "")
	for i, it := range m.items {
		prefix := "  "
		if i == m.cursor {
			prefix = styleSelected.Render("› ")
		}
		label := it.label
		if it.disabled != "" {
			label = styleDim.Render(label)
		}
		lines = append(lines, prefix+label)
	}
	hint := "enter select · esc close"
	if m.hint != "" {
		hint = "enter select · " + m.hint + " · esc close"
	}
	lines = append(lines, "", styleDim.Render(hint))
	return styleModal.Render(strings.Join(lines, "\n"))
}

// --- Confirm ---

// confirmModal asks yes/no with two buttons; enter presses the focused
// one. No starts focused, so a habitual enter can't abandon or delete;
// focusYes is for what is easily undone. No returns to back (nil closes).
type confirmModal struct {
	prompt string
	onYes  tea.Cmd
	back   modal
	then   modal // shown after Yes (nil closes)
	yes    bool  // Yes is focused
}

func newConfirm(prompt string, onYes tea.Cmd) *confirmModal {
	return &confirmModal{prompt: prompt, onYes: onYes}
}

// focusYes starts on Yes, for a question whose yes is safe.
func (c *confirmModal) focusYes() *confirmModal {
	c.yes = true
	return c
}

func (c *confirmModal) bar() buttonBar {
	b := buttonBar{labels: []string{"y Yes", "n No"}, focus: 1, center: true}
	if c.yes {
		b.focus = 0
	}
	return b
}

func (c *confirmModal) press(i int) (modal, tea.Cmd) {
	if i == 0 {
		return c.then, c.onYes
	}
	return c.back, nil
}

func (c *confirmModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch k := msg.String(); k {
	case "y", "Y":
		return c.press(0)
	case "n", "N", "esc":
		return c.press(1)
	case "enter":
		return c.press(c.bar().focus)
	default:
		b := c.bar()
		if b.move(k) {
			c.yes = b.focus == 0
		}
	}
	return c, nil
}

func (c *confirmModal) view(int) string {
	b := c.bar()
	w := b.width()
	for l := range strings.SplitSeq(c.prompt, "\n") {
		w = max(w, lipgloss.Width(l))
	}
	return styleModal.Render(c.prompt + "\n\n" + b.render(w))
}

// --- Editor ---

type editorDoneMsg struct {
	content string
	err     error
}

// editorModal edits markdown in a text box; ctrl+e hands the draft to
// $EDITOR and takes the result back.
type editorModal struct {
	title    string
	ta       textarea.Model
	onSubmit func(content string) tea.Cmd
	initial  string // text the editor opened with; esc only asks if it changed
}

func newEditor(title, initial string, width int, onSubmit func(string) tea.Cmd) *editorModal {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(min(100, max(40, width-10)))
	ta.SetHeight(10)
	ta.SetValue(initial)
	ta.Cursor.SetMode(cursor.CursorStatic) // a blinking cursor needs tick messages we don't route
	ta.Focus()
	return &editorModal{title: title, ta: ta, onSubmit: onSubmit, initial: initial}
}

func (e *editorModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if v := strings.TrimSpace(e.ta.Value()); v != "" && v != strings.TrimSpace(e.initial) {
			return &confirmModal{prompt: "Discard this draft?", back: e}, nil
		}
		return nil, nil
	case "ctrl+s":
		content := strings.TrimSpace(e.ta.Value())
		if content == "" {
			return e, statusCmd("comment is empty")
		}
		return nil, e.onSubmit(content)
	case "ctrl+e":
		return e, e.openEditor()
	}
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return e, cmd
}

func (e *editorModal) openEditor() tea.Cmd { return editExternally(e.ta.Value()) }

// editorDoner takes text back from $EDITOR.
type editorDoner interface {
	editorDone(msg editorDoneMsg) tea.Cmd
}

// editExternally opens text in $EDITOR and reports the result as an
// editorDoneMsg, with Windows line endings made plain.
func editExternally(text string) tea.Cmd {
	f, err := os.CreateTemp("", "lazdo-*.md")
	if err != nil {
		return statusCmd("error: " + err.Error())
	}
	_, err = f.WriteString(text)
	_ = f.Close()
	if err != nil {
		return statusCmd("error: " + err.Error())
	}
	name, args := editorCommand()
	cmd := exec.Command(name, append(args, f.Name())...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(f.Name())
		if err != nil {
			return editorDoneMsg{err: err}
		}
		b, err := os.ReadFile(f.Name())
		return editorDoneMsg{content: strings.ReplaceAll(string(b), "\r\n", "\n"), err: err}
	})
}

func (e *editorModal) editorDone(msg editorDoneMsg) tea.Cmd {
	if msg.err != nil {
		return statusCmd("error: editor: " + msg.err.Error())
	}
	e.ta.SetValue(strings.TrimRight(msg.content, "\r\n"))
	return nil
}

// editorCommand splits $VISUAL or $EDITOR, which may carry flags.
func editorCommand() (string, []string) {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(os.Getenv(env)); len(fields) > 0 {
			return fields[0], fields[1:]
		}
	}
	if runtime.GOOS == "windows" {
		return "notepad", nil
	}
	return "vi", nil
}

func (e *editorModal) view(int) string {
	return styleModal.Render(styleSection.Render(e.title) + "\n\n" + e.ta.View() + "\n\n" +
		styleDim.Render("ctrl+s post · ctrl+e open in $EDITOR · esc cancel"))
}
