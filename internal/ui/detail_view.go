package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

var (
	styleTabActive = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("14"))
	styleButton    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238")).Padding(0, 1)
	styleButtonCTA = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("25")).Padding(0, 1)
	styleBadge     = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("25")).Padding(0, 1)
	styleBadgeDim  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("240")).Padding(0, 1)
)

func (d *detailModel) view() string {
	var b strings.Builder
	b.WriteString(d.titleLine() + "\n")
	b.WriteString(d.subtitleLine() + "\n")
	b.WriteString(d.tabsLine() + "\n")
	b.WriteString(styleDim.Render(strings.Repeat("─", max(0, d.width-1))) + "\n")

	body := d.body()
	if d.modal != nil {
		body = lipgloss.Place(d.width-1, d.bodyHeight(), lipgloss.Center, lipgloss.Center, d.modal.view(d.width))
	}
	lines := strings.Split(body, "\n")
	for i := range d.bodyHeight() {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(truncate(line, d.width-1) + "\n")
	}
	b.WriteString(d.statusLine() + "\n")
	b.WriteString(styleDim.Render(truncate(d.help(), d.width-1)))
	return b.String()
}

func (d *detailModel) body() string {
	if d.tab == tabOverview {
		return d.vp.View()
	}
	if d.data == nil {
		return d.renderOverview() // loading or error text
	}
	return d.renderList()
}

func (d *detailModel) titleLine() string {
	vote := "Approve"
	if d.data != nil {
		if me, ok := d.data.ReviewerFor(d.me.ID); ok && me.Vote != ado.VoteNone {
			vote = voteText(me.Vote)
		}
	}
	complete := "Set auto-complete"
	if d.data != nil && d.data.AutoCompleteSetBy != nil && d.data.AutoCompleteSetBy.ID != "" {
		complete = "Auto-complete set"
	}
	buttons := styleButton.Render("v "+vote+" ▾") + " " + styleButtonCTA.Render("m "+complete+" ▾")
	title := truncate(styleTitle.Render(d.pr.Title), d.width-ansi.StringWidth(buttons)-3)
	gap := max(1, d.width-1-ansi.StringWidth(title)-ansi.StringWidth(buttons))
	return title + strings.Repeat(" ", gap) + buttons
}

func (d *detailModel) subtitleLine() string {
	badge := styleBadge.Render("Active")
	if d.pr.IsDraft {
		badge = styleBadgeDim.Render("Draft")
	}
	s := fmt.Sprintf("%s !%d %s proposes to merge %s into %s", badge, d.pr.ID, d.pr.CreatedBy.DisplayName,
		styleCyan.Render(d.pr.SourceBranch()), styleCyan.Render(d.pr.TargetBranch()))
	if d.loading {
		s += styleYellow.Render("  refreshing…")
	}
	return truncate(s, d.width-1)
}

func (d *detailModel) tabsLine() string {
	parts := make([]string, 0, tabCount)
	for t := range tabCount {
		label := fmt.Sprintf("%d %s", t+1, t.title())
		if d.data != nil {
			switch t {
			case tabFiles:
				label += fmt.Sprintf(" (%d)", len(d.data.Changes))
			case tabCommits:
				label += fmt.Sprintf(" (%d)", len(d.data.Commits))
			case tabConflicts:
				label += fmt.Sprintf(" (%d)", len(d.data.Conflicts))
			case tabOverview, tabCount:
			}
		}
		if t == d.tab {
			parts = append(parts, styleTabActive.Render(label))
		} else {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, "   ")
}

func (d *detailModel) statusLine() string {
	switch {
	case d.status != "" && strings.HasPrefix(d.status, "error"):
		return styleRed.Render(truncate(d.status, d.width-1))
	case d.status != "":
		return truncate(d.status, d.width-1)
	case d.err != nil && d.data != nil:
		return styleRed.Render(truncate("error: "+d.err.Error(), d.width-1))
	}
	return ""
}

func (d *detailModel) help() string {
	common := "v vote  m complete  o browser  y copy  c checkout  r refresh  1-4/[ ] tabs  esc back"
	if d.tab != tabOverview {
		return "j/k move  enter open  " + common
	}
	if d.inThread {
		return "j/k comment  R reply  s status  e edit  d delete  esc leave thread  " + common
	}
	return "j/k scroll  J/K thread  enter open thread  f filter  n comment  R reply  s status  " + common
}
