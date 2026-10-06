package ui

import (
	"fmt"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// The dashboard's markers are symbols when the terminal draws them one
// column wide, words otherwise: East Asian width settings draw these
// symbols two wide, which would push every column after them.

// SymbolSample holds every marker symbol, to measure how wide the terminal
// draws them; each should take one column.
const SymbolSample = symPush + symComment + symVote + symUnvisited + symDraft

const (
	symPush      = "↑"
	symComment   = "“"
	symVote      = "★"
	symUnvisited = "●"
	symDraft     = "◌"
)

var useSymbols bool

// UseSymbols switches the markers between symbols and words.
func UseSymbols(on bool) { useSymbols = on }

// sinceLastVisit mirrors the "N new pushes" note of the Azure DevOps list:
// "↑3 “2 ★1" and "●" for unvisited, or as words "[+3 pushes]" and "[new]",
// shortened to "[+3p]" on narrow screens.
func sinceLastVisit(s ado.Stats, compact bool) []string {
	if !s.Visited {
		return []string{styleCyan.Render(pick2(useSymbols, symUnvisited, "[new]"))}
	}
	var out []string
	for _, c := range []struct {
		n            int
		sym          string
		one, several string
	}{
		{s.NewPushes, symPush, "push", "pushes"},
		{s.NewComments, symComment, "comment", "comments"},
		{s.NewVotes, symVote, "vote", "votes"},
	} {
		switch {
		case c.n == 0:
			continue
		case useSymbols:
			out = append(out, styleCyan.Render(fmt.Sprintf("%s%d", c.sym, c.n)))
		case compact:
			out = append(out, styleCyan.Render(fmt.Sprintf("[+%d%c]", c.n, c.one[0])))
		default:
			out = append(out, styleCyan.Render(fmt.Sprintf("[+%d %s]", c.n, plural(c.n, c.one, c.several))))
		}
	}
	return out
}

func draftBadge(compact bool) string {
	if useSymbols {
		return styleDraft.Render(symDraft)
	}
	return styleDraft.Render(pick2(compact, "[d]", "[draft]"))
}

// threadMark leads a comment thread's status line.
func threadMark() string { return pick2(useSymbols, symComment+" ", "Thread ") }

// markerLegend explains the dashboard's markers in the help.
func markerLegend() []binding {
	if useSymbols {
		return []binding{
			{keys: symPush + " " + symComment + " " + symVote, desc: "new pushes, comments, votes since my last visit"},
			{keys: symUnvisited, desc: "not opened yet"},
			{keys: symDraft, desc: "draft"},
		}
	}
	return []binding{
		{keys: "[+3 pushes]", desc: "new pushes, comments, votes since my last visit ([+3p] [+2c] [+1v] when narrow)"},
		{keys: "[new]", desc: "not opened yet"},
		{keys: "[draft]", desc: "draft ([d] when narrow)"},
	}
}
