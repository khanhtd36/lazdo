package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// accent is lazdo's one color for "here": the active tab, the cursor, the
// focused pane's frame and the focused Section. Status colors (green,
// red, yellow) and the cyan "new" markers mean something else.
var accent = lipgloss.Color("111")

var (
	styleAccent = lipgloss.NewStyle().Foreground(accent)
	styleTabOn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("234")).Background(accent)
	styleTabOff = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("60"))
)

// Tabs are blocks, as in herdr: the label centered in at least tabMinWidth
// columns, one column apart; the active one in the accent color.
const tabMinWidth = 10

// tabWidths are each tab's width: padded, or tight when the padded bar
// would not fit in width.
func tabWidths(labels []string, width int) []int {
	pad := func(extra, least int) []int {
		ws := make([]int, len(labels))
		for i, l := range labels {
			ws[i] = max(least, ansi.StringWidth(l)+extra)
		}
		return ws
	}
	ws := pad(4, tabMinWidth)
	total := len(ws) - 1
	for _, w := range ws {
		total += w
	}
	if total > width {
		return pad(2, 0) // one space either side
	}
	return ws
}

// tabBar draws the tabs, the one at active highlighted, in width columns.
func tabBar(labels []string, active, width int) string {
	ws := tabWidths(labels, width)
	cells := make([]string, len(labels))
	for i, l := range labels {
		left := (ws[i] - ansi.StringWidth(l)) / 2
		text := strings.Repeat(" ", left) + l + strings.Repeat(" ", ws[i]-left-ansi.StringWidth(l))
		if i == active {
			cells[i] = styleTabOn.Render(text)
		} else {
			cells[i] = styleTabOff.Render(text)
		}
	}
	return strings.Join(cells, " ")
}

// tabAt is the tab covering column x of a bar tabBar drew in width columns.
func tabAt(labels []string, x, width int) (int, bool) {
	start := 0
	for i, w := range tabWidths(labels, width) {
		if x >= start && x < start+w {
			return i, true
		}
		start += w + 1
	}
	return 0, false
}
