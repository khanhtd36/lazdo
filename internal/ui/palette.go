package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// command is one action on one thing, offered by the command palette (:).
// It runs as its key or menu would; key names that key, if any.
type command struct {
	id       string // stable within its group, to run once a pull request loads
	label    string
	key      string
	disabled string // why it can't run now; empty when it can
	local    bool   // needs no more than the dashboard knows, such as a copy
	// run acts once the palette has closed, so what it opens stays open.
	run func(m *Model) tea.Cmd
}

// commandGroup is the commands for one thing: the focused item, a child of
// it, or an ancestor, titled by what it is.
type commandGroup struct {
	title    string
	commands []command
}

// paletteRunMsg runs a command after the palette closed.
type paletteRunMsg struct{ run func(m *Model) tea.Cmd }

// paletteRow is a group title or a command.
type paletteRow struct {
	group string
	cmd   *command // nil for a group title
}

// paletteModal lists commands nearest first; typing filters them all, the
// group's title included, so "repo clone" finds the repo's clone URL.
type paletteModal struct {
	groups []commandGroup
	filter string
	cursor int // index into rows(), always on a command
	offset int
	width  int
	height int // rows the list may take
}

func newPalette(groups []commandGroup, width, height int) *paletteModal {
	var kept []commandGroup
	for _, g := range groups {
		if len(g.commands) > 0 {
			kept = append(kept, g)
		}
	}
	// The box's size follows the screen only, never what is listed, so it
	// keeps still while typing filters it.
	p := &paletteModal{groups: kept, width: max(40, min(90, width*3/4)), height: max(5, min(height-9, height*2/3))}
	p.clamp(1)
	return p
}

func (p *paletteModal) rows() []paletteRow {
	var rows []paletteRow
	if p.filter == "" {
		for _, g := range p.groups {
			rows = append(rows, paletteRow{group: g.title})
			for i := range g.commands {
				rows = append(rows, paletteRow{group: g.title, cmd: &g.commands[i]})
			}
		}
		return rows
	}
	var all []paletteRow
	var texts []string
	for _, g := range p.groups {
		for i := range g.commands {
			c := &g.commands[i]
			all = append(all, paletteRow{group: g.title, cmd: c})
			texts = append(texts, ansi.Strip(c.label)+"  "+ansi.Strip(g.title))
		}
	}
	// What can run comes first; the rest keep their rank below it.
	var ready, blocked []paletteRow
	for _, m := range rankFuzzy(p.filter, texts) {
		if r := all[m.Index]; r.cmd.disabled == "" {
			ready = append(ready, r)
		} else {
			blocked = append(blocked, r)
		}
	}
	return append(ready, blocked...)
}

// clamp keeps the cursor on a command, moving in dir past group titles.
func (p *paletteModal) clamp(dir int) {
	rows := p.rows()
	p.cursor = max(0, min(p.cursor, len(rows)-1))
	for p.cursor >= 0 && p.cursor < len(rows) && rows[p.cursor].cmd == nil {
		p.cursor += dir
	}
	if p.cursor < 0 || p.cursor >= len(rows) {
		p.cursor = max(0, min(p.cursor, len(rows)-1))
		for p.cursor >= 0 && p.cursor < len(rows) && rows[p.cursor].cmd == nil {
			p.cursor -= dir
		}
		p.cursor = max(0, min(p.cursor, len(rows)-1))
	}
}

func (p *paletteModal) selected() (*command, bool) {
	rows := p.rows()
	if p.cursor < 0 || p.cursor >= len(rows) || rows[p.cursor].cmd == nil {
		return nil, false
	}
	return rows[p.cursor].cmd, true
}

func (p *paletteModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "enter":
		c, ok := p.selected()
		switch {
		case !ok:
			return p, nil
		case c.disabled != "":
			return nil, statusCmd(c.disabled)
		}
		run := c.run
		return nil, func() tea.Msg { return paletteRunMsg{run: run} }
	case "up", "ctrl+p", "ctrl+k":
		p.cursor--
		p.clamp(-1)
	case "down", "ctrl+n", "ctrl+j", "tab":
		p.cursor++
		p.clamp(1)
	case "pgup", "ctrl+u":
		p.cursor -= p.height / 2
		p.clamp(-1)
	case "pgdown", "ctrl+d":
		p.cursor += p.height / 2
		p.clamp(1)
	case "backspace":
		if r := []rune(p.filter); len(r) > 0 {
			p.filter, p.cursor = string(r[:len(r)-1]), 0
			p.clamp(1)
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			s := string(msg.Runes)
			if msg.Type == tea.KeySpace {
				s = " "
			}
			p.filter, p.cursor = p.filter+s, 0
			p.clamp(1)
		}
	}
	return p, nil
}

func (p *paletteModal) view(int) string {
	rows := p.rows()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+p.height {
		p.offset = p.cursor - p.height + 1
	}
	p.offset = max(0, min(p.offset, max(0, len(rows)-p.height)))
	w := p.width
	lines := []string{styleSection.Render("Commands") + "  " + styleSelected.Render(":"+p.filter+"▏"), ""}
	body := make([]string, 0, p.height)
	if len(rows) == 0 {
		body = append(body, styleDim.Render("  no command matches"))
	}
	for i := p.offset; i < len(rows) && i < p.offset+p.height; i++ {
		r := rows[i]
		if r.cmd == nil {
			body = append(body, styleHeader.Render(truncate(r.group, w)))
			continue
		}
		body = append(body, p.commandLine(r, i == p.cursor, w))
	}
	for len(body) < p.height {
		body = append(body, "")
	}
	where := ""
	if n := len(rows); n > p.height {
		where = styleDim.Render(fmt.Sprintf("  %d–%d of %d", p.offset+1, min(n, p.offset+p.height), n))
	}
	lines = append(append(lines, body...), where, styleDim.Render("type to filter · enter run · ↑/↓ move · esc close"))
	for i, l := range lines {
		lines[i] = fitStyled(l, w)
	}
	return styleModal.Render(strings.Join(lines, "\n"))
}

// commandLine is a command's row: its label, why it can't run (dimmed),
// its key on the right, and its group while a filter mixes groups.
func (p *paletteModal) commandLine(r paletteRow, selected bool, w int) string {
	c := r.cmd
	prefix := "  "
	if selected {
		prefix = styleSelected.Render("› ")
	}
	label := c.label
	if c.disabled != "" {
		label = styleDim.Render(ansi.Strip(c.label) + " · " + c.disabled)
	}
	right := c.key
	if p.filter != "" {
		right = strings.TrimSpace(ansi.Strip(r.group) + "  " + c.key)
	}
	room := w - 2 - ansi.StringWidth(right) - 2
	label = truncate(label, max(10, room))
	gap := max(2, w-2-ansi.StringWidth(label)-ansi.StringWidth(right))
	return prefix + label + strings.Repeat(" ", gap) + styleDim.Render(right)
}

// openPalette is ":": the commands for the focused item, nearest first.
func (m Model) openPalette() (tea.Model, tea.Cmd) {
	m.modal = newPalette(m.commandGroups(), m.width, m.height)
	return m, nil
}
