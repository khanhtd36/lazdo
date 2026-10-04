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
	if d.tab == tabFiles {
		return d.renderFiles()
	}
	return d.renderList()
}

// titleButtons renders the vote and complete buttons and returns the column
// each starts at, for mouse clicks.
func (d *detailModel) titleButtons() (vote, complete string, voteX, completeX int) {
	voteLabel := "Approve"
	if d.data != nil {
		if me, ok := d.data.ReviewerFor(d.me.ID); ok && me.Vote != ado.VoteNone {
			voteLabel = voteText(me.Vote)
		}
	}
	completeLabel := "Set auto-complete"
	if d.data != nil && d.data.AutoCompleteSetBy != nil && d.data.AutoCompleteSetBy.ID != "" {
		completeLabel = "Auto-complete set"
	}
	vote = styleButton.Render("v " + voteLabel + " ▾")
	complete = styleButtonCTA.Render("m " + completeLabel + " ▾")
	voteX = d.width - 1 - ansi.StringWidth(vote) - 1 - ansi.StringWidth(complete)
	return vote, complete, voteX, voteX + ansi.StringWidth(vote) + 1
}

func (d *detailModel) titleLine() string {
	vote, complete, voteX, _ := d.titleButtons()
	title := truncate(styleTitle.Render(d.pr.Title), voteX-2)
	return fit(title, voteX) + vote + " " + complete
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

const tabGap = "   "

// tabAt returns the tab whose label covers column x of the tabs line.
func (d *detailModel) tabAt(x int) (detailTab, bool) {
	start := 0
	for t, label := range d.tabLabels() {
		end := start + ansi.StringWidth(label)
		if x >= start && x < end {
			return detailTab(t), true
		}
		start = end + len(tabGap)
	}
	return 0, false
}

func (d *detailModel) tabsLine() string {
	parts := make([]string, 0, tabCount)
	for t, label := range d.tabLabels() {
		if detailTab(t) == d.tab {
			parts = append(parts, styleTabActive.Render(label))
		} else {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, tabGap)
}

func (d *detailModel) tabLabels() []string {
	labels := make([]string, 0, tabCount)
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
		labels = append(labels, label)
	}
	return labels
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
	common := "? help  v vote  m complete  o browser  y copy  c checkout  r refresh  1-4/[ ] tabs  esc back"
	switch {
	case d.tab == tabFiles && d.files.inThread:
		return "j/k comment  R reply  s status  e edit  d delete  esc leave thread  " + common
	case d.tab == tabFiles && d.files.pane == paneDiff:
		return "j/k line  n/N change  h/l side  V range  a comment  enter thread  R/s on thread  S mode  u compare  z tree  tab files  " + common
	case d.tab == tabFiles:
		return "j/k file  enter diff  S mode  u compare  z hide tree  " + common
	case d.tab == tabCommits:
		return "j/k move  enter diff of commit  " + common
	case d.tab != tabOverview:
		return "j/k move  enter open  " + common
	}
	if d.inThread {
		return "j/k comment  R reply  s status  e edit  d delete  esc leave thread  " + common
	}
	return "j/k scroll  J/K thread  enter open thread  f filter  n comment  R reply  s status  " + common
}
