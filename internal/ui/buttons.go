package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// buttonBar is a row of buttons, one focused: enter presses it. The
// focused one is blue, the others grey, like the title's vote and complete.
type buttonBar struct {
	labels []string
	focus  int
	center bool // centered in the dialog, else at its left
}

const buttonGap = 2

func (b buttonBar) widths() []int {
	ws := make([]int, len(b.labels))
	for i, l := range b.labels {
		ws[i] = ansi.StringWidth(styleButton.Render(l))
	}
	return ws
}

func (b buttonBar) width() int {
	w := 0
	for _, x := range b.widths() {
		w += x
	}
	return w + buttonGap*(len(b.labels)-1)
}

// pad is the space before the first button in a content row of width w.
func (b buttonBar) pad(w int) int {
	if !b.center {
		return 0
	}
	return max(0, (w-b.width())/2)
}

func (b buttonBar) render(w int) string {
	parts := make([]string, len(b.labels))
	for i, l := range b.labels {
		parts[i] = styleButton.Render(l)
		if i == b.focus {
			parts[i] = styleButtonCTA.Render(l)
		}
	}
	return strings.Repeat(" ", b.pad(w)) + strings.Join(parts, strings.Repeat(" ", buttonGap))
}

// at returns the button covering column x of a content row of width w.
func (b buttonBar) at(x, w int) (int, bool) {
	x -= b.pad(w)
	for i, bw := range b.widths() {
		if x >= 0 && x < bw {
			return i, true
		}
		x -= bw + buttonGap
	}
	return 0, false
}

// move handles the keys that walk the buttons; false for any other key.
func (b *buttonBar) move(k string) bool {
	n := len(b.labels)
	switch k {
	case "left", "h", "shift+tab":
		b.focus = (b.focus + n - 1) % n
	case "right", "l", "tab":
		b.focus = (b.focus + 1) % n
	default:
		return false
	}
	return true
}

// buttonModal is a dialog whose last content row is a button bar, for clicks.
type buttonModal interface {
	modal
	bar() buttonBar
	press(i int) (modal, tea.Cmd)
}

// clickButtons presses the button under a click at (x, y), both relative to
// the dialog box; ok is false off the buttons.
func clickButtons(m buttonModal, box string, x, y int) (next modal, cmd tea.Cmd, ok bool) {
	h, w := strings.Count(box, "\n")+1, ansi.StringWidth(firstLine(box))
	if y != h-2 { // the last row inside the border
		return m, nil, false
	}
	b := m.bar()
	i, hit := b.at(x-2, w-4) // border and padding on each side
	if !hit {
		return m, nil, false
	}
	next, cmd = m.press(i)
	return next, cmd, true
}
