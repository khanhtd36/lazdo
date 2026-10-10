package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A split view's panes sit side by side, each framed with its name in the
// top border; the focused pane's frame is in the accent color, the others gray.

var styleFrame = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// pane is one framed area: width counts its frame, and lines are its
// content, drawn inside at width-2 by height-2.
type pane struct {
	title   string
	width   int
	lines   []string
	focused bool
}

// paneInnerHeight is the content height inside a pane's frame; the width
// inside is the pane's width less 2.
func paneInnerHeight(height int) int { return max(1, height-2) }

// framePanes draws the panes side by side, height rows tall in all.
func framePanes(height int, panes ...pane) []string {
	out := make([]string, max(2, height))
	for _, p := range panes {
		for i, line := range framePane(p, len(out)) {
			out[i] += line
		}
	}
	return out
}

func framePane(p pane, height int) []string {
	edge := styleFrame
	title := styleDim
	if p.focused {
		edge, title = styleAccent, styleCursor
	}
	inner := max(1, p.width-2)
	name := ""
	if p.title != "" {
		name = title.Render(" " + truncate(p.title, max(1, inner-3)) + " ")
	}
	fill := max(0, inner-1-ansi.StringWidth(name))
	out := make([]string, 0, height)
	out = append(out, edge.Render("┌─")+name+edge.Render(strings.Repeat("─", fill)+"┐"))
	for i := range max(0, height-2) {
		line := ""
		if i < len(p.lines) {
			line = p.lines[i]
		}
		out = append(out, edge.Render("│")+padRight(truncate(line, inner), inner)+edge.Render("│"))
	}
	return append(out, edge.Render("└"+strings.Repeat("─", inner)+"┘"))
}
