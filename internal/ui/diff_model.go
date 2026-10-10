package ui

import (
	"path"
	"slices"
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

// hlText is a file's highlighted lines, kept small: each line is a slice
// of the displayed text, and its colors are runs over it, stored for the
// whole file in one array. A big file costs a few bytes per colored run,
// not a string and a color name for each.
type hlText struct {
	lines []hlLine
	spans []span
}

type hlLine struct {
	text     string
	from, to uint32 // the line's runs: spans[from:to]
}

// span colors a line's text up to end with palette[color]; text after the
// last span is in the default color.
type span struct {
	end   uint32
	color uint16
}

const hlStyle = "github-dark"

// palette is every color the highlight style uses; 0 is the default.
var palette, paletteIndex = func() ([]string, map[string]uint16) {
	colors, index := []string{""}, map[string]uint16{"": 0}
	style := styles.Get(hlStyle)
	for _, tt := range style.Types() {
		if e := style.Get(tt); e.Colour.IsSet() {
			c := e.Colour.String()
			if _, ok := index[c]; !ok {
				index[c] = uint16(len(colors))
				colors = append(colors, c)
			}
		}
	}
	return colors, index
}()

func (h *hlText) len() int { return len(h.lines) }

// segs is line i (from 0) as colored segments, nil out of range.
func (h *hlText) segs(i int) []seg {
	if h == nil || i < 0 || i >= len(h.lines) {
		return nil
	}
	l := h.lines[i]
	out := make([]seg, 0, l.to-l.from+1)
	at := 0
	for _, s := range h.spans[l.from:l.to] {
		end := min(int(s.end), len(l.text))
		if end > at {
			out = append(out, seg{text: l.text[at:end], fg: palette[s.color]})
			at = end
		}
	}
	if at < len(l.text) {
		out = append(out, seg{text: l.text[at:]})
	}
	return out
}

// highlight splits a file into lines and colors them with chroma.
func highlight(filename, text string) *hlText {
	text = displayText(text)
	h := plainLines(text)
	lexer := lexers.Match(path.Base(filename))
	if lexer == nil {
		lexer = lexers.Analyse(text)
	}
	if lexer == nil {
		return h
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return h
	}
	style := styles.Get(hlStyle)
	colorOf := map[chroma.TokenType]uint16{}
	line, at := 0, 0 // where the next token starts
	for tok := it(); tok != chroma.EOF && line < len(h.lines); tok = it() {
		c, ok := colorOf[tok.Type]
		if !ok {
			if e := style.Get(tok.Type); e.Colour.IsSet() {
				c = paletteIndex[e.Colour.String()]
			}
			colorOf[tok.Type] = c
		}
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				h.lines[line].to = uint32(len(h.spans))
				line, at = line+1, 0
				if line >= len(h.lines) {
					break
				}
				h.lines[line].from = uint32(len(h.spans))
			}
			at += len(part)
			if part == "" {
				continue
			}
			if n := len(h.spans); n > int(h.lines[line].from) && h.spans[n-1].color == c {
				h.spans[n-1].end = uint32(at) // same color as the run before: extend it
			} else {
				h.spans = append(h.spans, span{end: uint32(at), color: c})
			}
		}
	}
	if line < len(h.lines) {
		h.lines[line].to = uint32(len(h.spans))
	}
	for i := line + 1; i < len(h.lines); i++ { // lines the lexer never reached
		h.lines[i].from, h.lines[i].to = uint32(len(h.spans)), uint32(len(h.spans))
	}
	h.spans = slices.Clip(h.spans)
	return h
}

// plainLines splits text into uncolored lines.
func plainLines(text string) *hlText {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		parts = nil
	}
	h := &hlText{lines: make([]hlLine, len(parts))}
	for i, p := range parts {
		h.lines[i].text = p
	}
	return h
}

// wrapCount is how many rows wrapSegs makes of text, without making them.
func wrapCount(text string, width int) int {
	rows, used := 1, 0
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if used+w > width && used > 0 {
			rows, used = rows+1, 0
		}
		used += w
	}
	return rows
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
