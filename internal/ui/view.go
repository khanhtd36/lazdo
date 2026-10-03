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
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleCursor   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleGreen    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleYellow   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleRed      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleCyan     = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleDraft    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	styleRequired = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// Fixed widths of the right-hand columns; the title takes what's left.
const (
	colAuthor   = 16
	colID       = 7
	colRepo     = 28
	colVotes    = 18
	colComments = 6
	colBuild    = 2
	colUpdated  = 9
)

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.titleLine() + "\n")

	rows := m.rows()
	end := min(len(rows), m.offset+m.listHeight())
	for i := m.offset; i < end; i++ {
		prefix := "  "
		if i == m.cursor {
			prefix = styleCursor.Render("▌ ")
		}
		b.WriteString(truncate(prefix+m.renderRow(rows[i]), m.width-1) + "\n")
	}
	for i := end - m.offset; i < m.listHeight(); i++ {
		b.WriteString("\n")
	}
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(styleDim.Render(truncate("j/k move  tab section  enter toggle/open  o open  y copy URL  c checkout  r refresh  q quit", m.width-1)))
	return b.String()
}

func (m Model) titleLine() string {
	s := styleTitle.Render("lazdo") + styleDim.Render(" · "+m.client.Org)
	if m.me.DisplayName != "" {
		s += styleDim.Render(" · " + m.me.DisplayName)
	}
	switch {
	case m.loading:
		s += styleYellow.Render("  refreshing…")
	case !m.fetchedAt.IsZero():
		s += styleDim.Render("  updated " + m.fetchedAt.Format("15:04:05"))
	}
	return truncate(s, m.width-1)
}

func (m Model) statusLine() string {
	switch {
	case m.err != nil:
		return styleRed.Render(truncate("error: "+m.err.Error(), m.width))
	case m.status != "":
		return truncate(m.status, m.width)
	}
	return ""
}

func (m Model) renderRow(r row) string {
	s := m.sections[r.section]
	if r.pr == nil {
		arrow := "▾"
		if m.collapsed[s.Kind] {
			arrow = "▸"
		}
		return styleHeader.Render(fmt.Sprintf("%s %s (%d)", arrow, s.Kind.Title(), len(s.PRs)))
	}
	return m.renderPR(r.pr)
}

func (m Model) renderPR(pr *ado.PullRequest) string {
	det := m.details[pr.ID]

	var badges []string
	if pr.IsDraft {
		badges = append(badges, styleDraft.Render("[draft]"))
	}
	if me, ok := pr.ReviewerFor(m.me.ID); ok && me.IsRequired {
		badges = append(badges, styleRequired.Render("[required]"))
	}
	if det.d.PushesSinceMyVote > 0 {
		badges = append(badges, styleCyan.Render(fmt.Sprintf("[+%d push]", det.d.PushesSinceMyVote)))
	}
	badgeText := strings.Join(badges, " ")

	right := strings.Join([]string{
		styleDim.Render(fit(pr.CreatedBy.DisplayName, colAuthor)),
		fit(fmt.Sprintf("!%d", pr.ID), colID),
		styleDim.Render(fit(pr.Repository.Name+" → "+pr.TargetBranch(), colRepo)),
		fitStyled(votes(pr.Reviewers), colVotes),
		fitStyled(comments(det), colComments),
		fitStyled(build(det), colBuild),
		styleDim.Render(fit(updated(det, pr), colUpdated)),
	}, " ")

	// 2 cursor + 2 indent + 1 gap before right + 1 spare so the line never wraps.
	titleWidth := m.width - 6 - ansi.StringWidth(right) - ansi.StringWidth(badgeText)
	if badgeText != "" {
		titleWidth--
	}
	title := fit(pr.Title, max(titleWidth, 10))
	if badgeText != "" {
		title += " " + badgeText
	}
	return "  " + title + " " + right // indent under the section header
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

func comments(det detailState) string {
	switch {
	case !det.loaded:
		return styleDim.Render("…")
	case det.err != nil:
		return styleRed.Render("err")
	case det.d.CommentsTotal == 0:
		return styleDim.Render("0")
	}
	s := fmt.Sprintf("%d/%d", det.d.CommentsResolved, det.d.CommentsTotal)
	if det.d.CommentsResolved < det.d.CommentsTotal {
		return styleYellow.Render(s)
	}
	return styleGreen.Render(s)
}

func build(det detailState) string {
	if !det.loaded {
		return styleDim.Render("…")
	}
	switch det.d.Build {
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

func updated(det detailState, pr *ado.PullRequest) string {
	t := pr.CreationDate
	if det.loaded && det.d.LastUpdated.After(t) {
		t = det.d.LastUpdated
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
