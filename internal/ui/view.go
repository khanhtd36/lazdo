package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

var (
	styleTitle  = lipgloss.NewStyle().Bold(true)
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleCursor = lipgloss.NewStyle().Bold(true).Foreground(accent)
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleCyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleDraft  = lipgloss.NewStyle().Foreground(lipgloss.Color("244")) // grayed: a draft isn't asking for review yet
)

// Fixed widths of the right-hand columns; the title takes what's left.
const (
	colAuthor        = 16
	colAuthorCompact = 4
	colID            = 7
	colRepo          = 28
	colComments      = 6
	colBuild         = 2
	colUpdated       = 9
)

// compactWidth is the width below which badges and author names shorten.
const compactWidth = 150

type column int

const (
	columnAuthor column = iota
	columnID
	columnRepo
	columnComments
	columnBuild
	columnUpdated
	columnCount
)

// columnPriority is which columns get room first when the title would
// otherwise get squeezed; one that doesn't fit gives way to smaller ones
// after it. The ID always stays.
var columnPriority = []column{columnID, columnBuild, columnRepo, columnComments, columnAuthor, columnUpdated}

// rowLayout is how dashboard rows fit the current width.
type rowLayout struct {
	compact bool
	shown   [columnCount]bool
}

func (l rowLayout) width(c column) int {
	switch c {
	case columnAuthor:
		if l.compact {
			return colAuthorCompact
		}
		return colAuthor
	case columnID:
		return colID
	case columnRepo:
		return colRepo
	case columnComments:
		return colComments
	case columnBuild:
		return colBuild
	case columnUpdated:
		return colUpdated
	case columnCount:
	}
	return 0
}

func (m Model) rowLayout() rowLayout {
	l := rowLayout{compact: m.width < compactWidth}
	badges := 22 // room for typical badges like "[draft] [2 new pushes]"
	if l.compact {
		badges = 9 // "[d] [+8p]"
	}
	// What's left once the title has its third, after cursor, indent, gaps.
	room := m.width - 6 - badges - max(24, m.width/3)
	for _, c := range columnPriority {
		if need := l.width(c) + 1; need <= room || c == columnID {
			l.shown[c] = true
			room -= need
		}
	}
	return l
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.help != nil {
		return m.help.place(m.width, m.height)
	}
	if m.modal != nil {
		return placeModal(m.modal, m.width, m.height)
	}
	if m.detail != nil {
		return m.detail.view()
	}
	if m.project != nil {
		return m.project.view()
	}
	var b strings.Builder
	b.WriteString(m.titleLine() + "\n")
	if m.page == pageProjects {
		for _, line := range padLines(m.projects.view(m.width-1, m.listHeight()), m.listHeight()) {
			b.WriteString(truncate(line, m.width-1) + "\n")
		}
		b.WriteString(m.statusLine() + "\n")
		b.WriteString(styleDim.Render(truncate(footer(m.helpGroups()), m.width-1)))
		return b.String()
	}

	rows := m.rows()
	focused := -1 // the Section the cursor is in
	if m.cursor >= 0 && m.cursor < len(rows) {
		focused = rows[m.cursor].section
	}
	lines := dashLines(rows)
	end := min(len(lines), m.offset+m.listHeight())
	for i := m.offset; i < end; i++ {
		l := lines[i]
		b.WriteString(truncate(m.dashboardLine(l, rows[l.row], l.row == m.cursor && !l.bottom, l.section == focused), m.width-1) + "\n")
	}
	for i := end - m.offset; i < m.listHeight(); i++ {
		b.WriteString("\n")
	}
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(styleDim.Render(truncate(footer(m.helpGroups()), m.width-1)))
	return b.String()
}

const titlePrefix = "lazdo  "

// pageLabels are the page switchers after the "lazdo" prefix.
func (m Model) pageLabels() []string {
	labels := make([]string, 0, pageCount)
	for p := range pageCount {
		labels = append(labels, fmt.Sprintf("%d %s", p+1, p.title()))
	}
	return labels
}

func (m Model) pageAt(x int) (page, bool) {
	p, ok := tabAt(m.pageLabels(), x-len(titlePrefix), m.width-1-len(titlePrefix))
	return page(p), ok
}

func (m Model) titleLine() string {
	s := styleTitle.Render(strings.TrimSpace(titlePrefix)) + "  "
	s += tabBar(m.pageLabels(), int(m.page), m.width-1-len(titlePrefix)) + styleDim.Render("   "+m.client.Org)
	if m.me.DisplayName != "" {
		s += styleDim.Render(" · " + m.me.DisplayName)
	}
	if m.page == pageProjects {
		if m.projects.loading {
			s += styleYellow.Render("  refreshing…")
		}
		return truncate(s+m.updateNote(), m.width-1)
	}
	switch {
	case m.loading:
		s += styleYellow.Render("  refreshing…")
	case !m.fetchedAt.IsZero():
		s += styleDim.Render("  updated " + m.fetchedAt.Format("15:04:05"))
	}
	return truncate(s+m.updateNote(), m.width-1)
}

func (m Model) statusLine() string {
	switch {
	case m.filterTyping:
		return styleSelected.Render("/"+m.filter+"▏") + styleDim.Render("  enter open · esc clear")
	case m.filter != "":
		return styleDim.Render("/" + m.filter + "  · / edit · esc clear")
	case m.err != nil:
		return styleRed.Render(truncate("error: "+m.err.Error(), m.width))
	case m.status != "":
		return truncate(m.status, m.width)
	}
	return ""
}

// dashLine is a screen line of the Pull requests page: a row, or the
// bottom edge that closes its Section's pane.
type dashLine struct {
	row, section int
	bottom       bool
}

// dashLines lays the rows out as stacked panes, one per Section, all under
// one scroll: each Section's last row is followed by its pane's bottom edge.
func dashLines(rows []row) []dashLine {
	lines := make([]dashLine, 0, len(rows)+8)
	for i, r := range rows {
		lines = append(lines, dashLine{row: i, section: r.section})
		if i+1 == len(rows) || rows[i+1].section != r.section {
			lines = append(lines, dashLine{row: i, section: r.section, bottom: true})
		}
	}
	return lines
}

// dashboardLine draws a line of the Pull requests page. Each Section is a
// framed pane, its header the top edge; the focused Section, the one the
// cursor is in, is framed in the accent color like any focused pane.
func (m Model) dashboardLine(l dashLine, r row, cursor, focused bool) string {
	edge, title := styleFrame, styleDim
	if focused {
		edge, title = styleAccent, styleCursor
	}
	inner := max(1, m.width-3)
	switch {
	case l.bottom:
		return edge.Render("└" + strings.Repeat("─", inner) + "┘")
	case r.pr == nil:
		s := m.sections[r.section]
		arrow := "▾"
		if m.collapsed[s.Kind] {
			arrow = "▸"
		}
		lead := edge.Render("─")
		if cursor {
			lead = styleCursor.Render("▌")
		}
		name := title.Render(truncate(fmt.Sprintf(" %s %s (%d) ", arrow, s.Kind.Title(), len(s.PRs)), max(1, inner-2)))
		fill := max(0, inner-1-ansi.StringWidth(name))
		return edge.Render("┌") + lead + name + edge.Render(strings.Repeat("─", fill)+"┐")
	}
	mark := " "
	if cursor {
		mark = styleCursor.Render("▌")
	}
	return edge.Render("│") + padRight(truncate(mark+m.renderPR(r.pr), inner), inner) + edge.Render("│")
}

func (m Model) renderPR(pr *ado.PullRequest) string {
	stats, hasStats := m.stats[pr.ID]
	buildRes, hasBuild := m.builds[pr.ID]

	layout := m.rowLayout()

	var badges []string
	if pr.IsDraft {
		badges = append(badges, draftBadge(layout.compact))
	}
	if hasStats {
		badges = append(badges, sinceLastVisit(stats, layout.compact)...)
	}
	badgeText := strings.Join(badges, " ")

	author := pr.CreatedBy.DisplayName
	if layout.compact {
		author = nameInitials(author)
	}
	cells := [columnCount]string{
		columnAuthor:   styleDim.Render(fit(author, layout.width(columnAuthor))),
		columnID:       fit(fmt.Sprintf("!%d", pr.ID), colID),
		columnRepo:     styleDim.Render(fit(pr.Repository.Name+" → "+pr.TargetBranch(), colRepo)),
		columnComments: fitStyled(m.comments(stats, hasStats), colComments),
		columnBuild:    fitStyled(build(buildRes, hasBuild), colBuild),
		columnUpdated:  styleDim.Render(fit(updated(stats, pr), colUpdated)),
	}
	var shown []string
	for c, s := range cells {
		if layout.shown[c] {
			shown = append(shown, s)
		}
	}
	right := strings.Join(shown, " ")

	// 2 frame + 1 cursor + 1 indent + 1 gap before right + 1 spare so the
	// line never wraps.
	titleWidth := m.width - 6 - ansi.StringWidth(right) - ansi.StringWidth(badgeText)
	if badgeText != "" {
		titleWidth--
	}
	// Badges follow the title text, like the web list; the padding goes
	// after them so the right-hand columns still line up.
	title := truncate(pr.Title, max(titleWidth, 10))
	style := lipgloss.NewStyle()
	if pr.IsDraft {
		style = style.Foreground(lipgloss.Color("240")) // grayer than the dim columns: not up for review yet
	}
	if hasStats && hasNews(stats, pr.IsDraft) {
		style = style.Bold(true)
	}
	title = style.Render(title)
	cellWidth := max(titleWidth, 10)
	if badgeText != "" {
		title += " " + badgeText
		cellWidth += 1 + ansi.StringWidth(badgeText)
	}
	return " " + padRight(title, cellWidth) + " " + right
}

// hasNews is whether a PR has something to look at: pushes or comments since
// my last visit, or, when it's ready for review, no visit at all yet.
func hasNews(s ado.Stats, draft bool) bool {
	if !s.Visited {
		return !draft
	}
	return s.NewPushes > 0 || s.NewComments > 0
}

func votes(reviewers []ado.Reviewer) string {
	parts := make([]string, 0, len(reviewers))
	for _, r := range reviewers {
		in := initials(r.DisplayName)
		switch {
		case r.Vote >= ado.VoteApproved:
			parts = append(parts, styleGreen.Render(in+"✓"))
		case r.Vote >= ado.VoteApprovedWithSuggests:
			parts = append(parts, styleGreen.Render(in+"~"))
		case r.Vote <= ado.VoteRejected:
			parts = append(parts, styleRed.Render(in+"✗"))
		case r.Vote <= ado.VoteWaitingForAuthor:
			parts = append(parts, styleYellow.Render(in+"!"))
		default:
			parts = append(parts, styleDim.Render(in))
		}
	}
	return strings.Join(parts, " ")
}

func pick2(compact bool, short, long string) string {
	if compact {
		return short
	}
	return long
}

// nameInitials turns "Truong Duy Khanh" into "TDK" (at most four letters).
func nameInitials(name string) string {
	var b strings.Builder
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if b.Len() < 4 {
			b.WriteRune(unicode.ToUpper([]rune(w)[0]))
		}
	}
	return b.String()
}

func (m Model) comments(s ado.Stats, ok bool) string {
	switch {
	case m.statsErr != nil:
		return styleRed.Render("err")
	case !ok:
		return styleDim.Render("…")
	case s.Comments == 0:
		return styleDim.Render("0")
	case s.ActiveComments > 0:
		// Like Azure DevOps: unresolved/total while any thread is open.
		return styleYellow.Render(fmt.Sprintf("%d/%d", s.ActiveComments, s.Comments))
	}
	return fmt.Sprint(s.Comments)
}

func build(b buildResult, ok bool) string {
	switch {
	case !ok:
		return styleDim.Render("…")
	case b.err != nil:
		return styleRed.Render("?")
	}
	switch b.state {
	case ado.BuildPassed:
		return styleGreen.Render("✓")
	case ado.BuildFailed:
		return styleRed.Render("✗")
	case ado.BuildRunning:
		return styleYellow.Render("●")
	case ado.BuildNone:
	}
	return styleDim.Render("·")
}

func updated(s ado.Stats, pr *ado.PullRequest) string {
	t := pr.CreationDate
	if s.LastUpdated.After(t) {
		t = s.LastUpdated
	}
	return relTime(time.Since(t), t)
}

func relTime(d time.Duration, t time.Time) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Local().Format("Jan 2")
}

// initials turns "Truong Duy Khanh" into "TK"; a single word gives its first
// two letters. Group names like "[MITS11]\Reviewers" lose their brackets.
func initials(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	switch len(words) {
	case 0:
		return "?"
	case 1:
		r := []rune(words[0])
		return strings.ToUpper(string(r[:min(2, len(r))]))
	}
	first, last := []rune(words[0]), []rune(words[len(words)-1])
	return strings.ToUpper(string(first[0]) + string(last[0]))
}

// fit truncates or pads plain text to exactly w cells.
func fit(s string, w int) string { return padRight(truncate(s, w), w) }

// fitStyled is fit for strings that already carry ANSI styling.
func fitStyled(s string, w int) string { return padRight(ansi.Truncate(s, w, "…"), w) }

func truncate(s string, w int) string { return ansi.Truncate(s, w, "…") }

func padRight(s string, w int) string {
	if n := ansi.StringWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}
