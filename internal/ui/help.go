package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	tea "github.com/charmbracelet/bubbletea"
)

// helpRow is one line of the help list: a group title or a binding.
type helpRow struct {
	group   string
	binding *binding // nil for a group title
	matches []int    // matched character positions in keys+"  "+desc
}

// helpModal lists the shortcuts for the current screen. / filters them
// fuzzily; enter runs the selected one.
type helpModal struct {
	groups    []bindingGroup
	filter    string
	typing    bool
	cursor    int // index into rows(), always on a binding
	offset    int
	maxHeight int
	width     int
}

func newHelp(groups []bindingGroup, screenWidth, maxHeight int) *helpModal {
	h := &helpModal{groups: groups, maxHeight: maxHeight, width: max(40, min(96, screenWidth-4))}
	h.cursor = h.firstBinding(0, 1)
	return h
}

func searchText(b binding) string { return b.keys + "  " + b.desc }

func (h *helpModal) rows() []helpRow {
	if h.filter == "" {
		var rows []helpRow
		for _, g := range h.groups {
			rows = append(rows, helpRow{group: g.title})
			for i := range g.bindings {
				rows = append(rows, helpRow{group: g.title, binding: &g.bindings[i]})
			}
		}
		return rows
	}
	var all []helpRow
	var texts []string
	for _, g := range h.groups {
		for i := range g.bindings {
			all = append(all, helpRow{group: g.title, binding: &g.bindings[i]})
			texts = append(texts, searchText(g.bindings[i]))
		}
	}
	matches := fuzzy.Find(h.filter, texts)
	// Rank tight matches first: "cmpl" should find "complete" before
	// "new comment on the pull request", which fuzzy scores higher for
	// hitting word starts.
	span := func(m fuzzy.Match) int {
		return m.MatchedIndexes[len(m.MatchedIndexes)-1] - m.MatchedIndexes[0]
	}
	sort.SliceStable(matches, func(i, j int) bool { return span(matches[i]) < span(matches[j]) })
	rows := make([]helpRow, 0, len(matches))
	for _, mt := range matches {
		r := all[mt.Index]
		r.matches = mt.MatchedIndexes
		rows = append(rows, r)
	}
	return rows
}

// firstBinding finds the nearest binding row from i in direction dir.
func (h *helpModal) firstBinding(i, dir int) int {
	rows := h.rows()
	for ; i >= 0 && i < len(rows); i += dir {
		if rows[i].binding != nil {
			return i
		}
	}
	return h.cursor
}

func (h *helpModal) listHeight() int { return max(3, h.maxHeight-6) }

func (h *helpModal) move(delta int) {
	rows := h.rows()
	if len(rows) == 0 {
		return
	}
	target := max(0, min(h.cursor+delta, len(rows)-1))
	if rows[target].binding == nil {
		target = h.firstBinding(target, sign(delta))
	}
	if rows[target].binding == nil {
		return
	}
	h.cursor = target
	top := h.cursor
	if top > 0 && rows[top-1].binding == nil {
		top-- // keep a group's title in view with its first binding
	}
	h.offset = min(h.offset, top)
	if h.cursor >= h.offset+h.listHeight() {
		h.offset = h.cursor - h.listHeight() + 1
	}
}

func (h *helpModal) resetCursor() {
	h.cursor, h.offset = 0, 0
	h.cursor = h.firstBinding(0, 1)
}

// update returns closed=true when the popup should go away, plus the key
// to replay when the user ran a binding.
func (h *helpModal) update(msg tea.KeyMsg) (closed bool, run *tea.KeyMsg) {
	key := msg.String()
	if h.typing {
		switch key {
		case "esc":
			h.filter, h.typing = "", false
			h.resetCursor()
		case "enter":
			h.typing = false
		case "backspace":
			if r := []rune(h.filter); len(r) > 0 {
				h.filter = string(r[:len(r)-1])
				h.resetCursor()
			}
		case "up", "ctrl+k", "ctrl+p":
			h.move(-1)
		case "down", "ctrl+j", "ctrl+n":
			h.move(1)
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				s := string(msg.Runes)
				if s == "" {
					s = " "
				}
				h.filter += s
				h.resetCursor()
			}
		}
		return false, nil
	}
	switch key {
	case "esc", "q", "?":
		if h.filter != "" && key == "esc" {
			h.filter = ""
			h.resetCursor()
			return false, nil
		}
		return true, nil
	case "/":
		h.typing = true
	case "j", "down":
		h.move(1)
	case "k", "up":
		h.move(-1)
	case "ctrl+d", "pgdown":
		h.move(h.listHeight() / 2)
	case "ctrl+u", "pgup":
		h.move(-h.listHeight() / 2)
	case "g", "home":
		h.resetCursor()
	case "G", "end":
		h.move(len(h.rows()))
	case "enter":
		rows := h.rows()
		if h.cursor < len(rows) && rows[h.cursor].binding != nil && rows[h.cursor].binding.press != "" {
			k := keyMsg(rows[h.cursor].binding.press)
			return true, &k
		}
	}
	return false, nil
}

func (h *helpModal) view() string {
	rows := h.rows()
	lines := make([]string, 0, h.listHeight()+4)
	title := styleSection.Render("Keyboard shortcuts")
	switch {
	case h.typing:
		title += "  " + styleSelected.Render("/"+h.filter+"▏")
	case h.filter != "":
		title += "  " + styleDim.Render("/"+h.filter)
	}
	lines = append(lines, title, "")
	end := min(len(rows), h.offset+h.listHeight())
	for i := h.offset; i < end; i++ {
		lines = append(lines, h.renderRow(rows[i], i == h.cursor))
	}
	if len(rows) == 0 {
		lines = append(lines, styleDim.Render("  no match"))
	}
	for len(lines) < h.listHeight()+2 {
		lines = append(lines, "")
	}
	hint := "j/k scroll · / search · enter run · esc close"
	if h.typing {
		hint = "type to filter · ↑/↓ move · enter keep filter · esc clear"
	}
	lines = append(lines, "", styleDim.Render(hint))
	return styleModal.Width(h.width).Render(strings.Join(lines, "\n"))
}

func (h *helpModal) renderRow(r helpRow, selected bool) string {
	if r.binding == nil {
		return styleHeader.Render(r.group)
	}
	text := searchText(*r.binding)
	keysLen := len([]rune(r.binding.keys))
	matched := map[int]bool{}
	for _, i := range r.matches {
		matched[i] = true
	}
	var b strings.Builder
	for i, ch := range []rune(text) {
		s := string(ch)
		switch {
		case matched[i]:
			s = styleYellow.Bold(true).Render(s)
		case i < keysLen:
			s = styleCyan.Render(s)
		}
		b.WriteString(s)
		if i == keysLen-1 {
			// Pad the keys column so descriptions line up.
			b.WriteString(strings.Repeat(" ", max(0, 16-keysLen)))
		}
	}
	line := b.String()
	if h.filter != "" {
		line += styleDim.Render("  · " + r.group)
	}
	prefix := "  "
	if selected {
		prefix = styleSelected.Render("› ")
	}
	return truncate(prefix+line, h.width-4)
}

// place centers the help popup over a screen of the given size.
func (h *helpModal) place(width, height int) string {
	return lipgloss.Place(width-1, height, lipgloss.Center, lipgloss.Center, h.view())
}
