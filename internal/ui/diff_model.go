package ui

import (
	"path"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/mattn/go-runewidth"

	"github.com/khanhtd36/lazdo/internal/ado"
)

type lineKind int

const (
	lineContext lineKind = iota
	lineAdd
	lineDelete
	lineEdit
)

// diffLine is one displayed line. Side-by-side shows both numbers on one
// line; inline gives each side its own line. 0 means "no line on that side".
type diffLine struct {
	kind  lineKind
	left  int
	right int
}

// sideBySideLines aligns the two files using Azure DevOps' line blocks.
func sideBySideLines(blocks []ado.LineBlock, nLeft, nRight int) []diffLine {
	if len(blocks) == 0 {
		return wholeFileLines(nLeft, nRight)
	}
	var out []diffLine
	for _, b := range blocks {
		switch b.ChangeType {
		case "add":
			for i := range b.ModifiedCount {
				out = append(out, diffLine{kind: lineAdd, right: b.ModifiedStart + i})
			}
		case "delete":
			for i := range b.OriginalCount {
				out = append(out, diffLine{kind: lineDelete, left: b.OriginalStart + i})
			}
		case "edit":
			for i := range max(b.OriginalCount, b.ModifiedCount) {
				l := diffLine{kind: lineEdit}
				if i < b.OriginalCount {
					l.left = b.OriginalStart + i
				}
				if i < b.ModifiedCount {
					l.right = b.ModifiedStart + i
				}
				out = append(out, l)
			}
		default:
			for i := range b.ModifiedCount {
				out = append(out, diffLine{kind: lineContext, left: b.OriginalStart + i, right: b.ModifiedStart + i})
			}
		}
	}
	return out
}

// wholeFileLines covers files the diff API returns no blocks for: an added
// file is all additions, a deleted one all deletions.
func wholeFileLines(nLeft, nRight int) []diffLine {
	var out []diffLine
	for i := 1; i <= nLeft; i++ {
		out = append(out, diffLine{kind: lineDelete, left: i})
	}
	for i := 1; i <= nRight; i++ {
		out = append(out, diffLine{kind: lineAdd, right: i})
	}
	return out
}

// inlineLines flattens side-by-side lines: each changed run shows its
// removed lines first, then its added lines.
func inlineLines(sbs []diffLine) []diffLine {
	var out, dels, adds []diffLine
	flush := func() {
		out = append(out, dels...)
		out = append(out, adds...)
		dels, adds = nil, nil
	}
	for _, l := range sbs {
		if l.kind == lineContext {
			flush()
			out = append(out, l)
			continue
		}
		if l.left > 0 {
			dels = append(dels, diffLine{kind: lineDelete, left: l.left})
		}
		if l.right > 0 {
			adds = append(adds, diffLine{kind: lineAdd, right: l.right})
		}
	}
	flush()
	return out
}

// seg is a run of text in one color ("" for the default color).
type seg struct {
	text string
	fg   string
}

const tabWidth = 4

// highlight splits a file into lines of colored segments with chroma.
// displayText makes file text safe to draw: tabs become spaces, and
// characters a terminal might draw at a different width than counted, or
// act on, are dropped (a byte order mark) or shown as "?" (control
// characters such as a lone carriage return or an escape). A row drawn
// wider than counted wraps and scrolls the whole screen.
func displayText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", strings.Repeat(" ", tabWidth))
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == 0xfeff: // byte order mark
			return -1
		case r < 0x20 || r == 0x7f:
			return '?'
		}
		return r
	}, text)
}

func highlight(filename, text string) [][]seg {
	text = displayText(text)
	lexer := lexers.Match(path.Base(filename))
	if lexer == nil {
		lexer = lexers.Analyse(text)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("github-dark")
	lines := [][]seg{nil}
	it, err := lexer.Tokenise(nil, text)
	if err != nil {
		return plainLines(text)
	}
	for tok := it(); tok != chroma.EOF; tok = it() {
		fg := ""
		if e := style.Get(tok.Type); e.Colour.IsSet() {
			fg = e.Colour.String()
		}
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				lines = append(lines, nil)
			}
			if part != "" {
				lines[len(lines)-1] = append(lines[len(lines)-1], seg{text: part, fg: fg})
			}
		}
	}
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 && strings.HasSuffix(text, "\n") {
		lines = lines[:n-1]
	}
	return lines
}

func plainLines(text string) [][]seg {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	out := make([][]seg, len(parts))
	for i, p := range parts {
		out[i] = []seg{{text: p}}
	}
	return out
}

// wrapSegs breaks a line of segments into rows at most width cells wide.
func wrapSegs(segs []seg, width int) [][]seg {
	rows := [][]seg{nil}
	used := 0
	for _, s := range segs {
		var cur strings.Builder
		for _, r := range s.text {
			w := runewidth.RuneWidth(r)
			if used+w > width && used > 0 {
				if cur.Len() > 0 {
					rows[len(rows)-1] = append(rows[len(rows)-1], seg{text: cur.String(), fg: s.fg})
					cur.Reset()
				}
				rows = append(rows, nil)
				used = 0
			}
			cur.WriteRune(r)
			used += w
		}
		if cur.Len() > 0 {
			rows[len(rows)-1] = append(rows[len(rows)-1], seg{text: cur.String(), fg: s.fg})
		}
	}
	return rows
}
