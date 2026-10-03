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

var styleModal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(0, 1)

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
}

func (m *menuModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "j", "down", "tab":
		m.cursor = (m.cursor + 1) % len(m.items)
	case "k", "up", "shift+tab":
		m.cursor = (m.cursor + len(m.items) - 1) % len(m.items)
	case "enter":
		it := m.items[m.cursor]
		if it.disabled != "" {
			return m, statusCmd(it.disabled)
		}
		return it.run()
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
	lines = append(lines, "", styleDim.Render("enter select · esc close"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

// --- Confirm ---

type confirmModal struct {
	prompt string
	onYes  tea.Cmd
}

func newConfirm(prompt string, onYes tea.Cmd) *confirmModal {
	return &confirmModal{prompt: prompt, onYes: onYes}
}

func (c *confirmModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return nil, c.onYes
	case "n", "N", "esc", "q":
		return nil, nil
	}
	return c, nil
}

func (c *confirmModal) view(int) string {
	return styleModal.Render(c.prompt + "\n\n" + styleDim.Render("y yes · n no"))
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
	return &editorModal{title: title, ta: ta, onSubmit: onSubmit}
}

func (e *editorModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
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

func (e *editorModal) openEditor() tea.Cmd {
	f, err := os.CreateTemp("", "lazdo-*.md")
	if err != nil {
		return statusCmd("error: " + err.Error())
	}
	_, err = f.WriteString(e.ta.Value())
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
		return editorDoneMsg{content: string(b), err: err}
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
