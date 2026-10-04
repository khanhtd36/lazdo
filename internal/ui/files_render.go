package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/khanhtd36/lazdo/internal/ado"
)

var (
	bgAdd     = lipgloss.Color("#173d24")
	bgDelete  = lipgloss.Color("#4b1d22")
	bgMissing = lipgloss.Color("#262626")

	styleCursorLine = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleRangeLine  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleGutter     = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

// rowPair is one screen row of a diff line before the cursor marker is
// added: left and right cells in side-by-side, only right inline.
type rowPair struct{ left, right string }

// diffRows caches the rendered rows of every line for one layout.
type diffRows struct {
	key  string
	rows [][]rowPair
}

var segStyles = map[string]lipgloss.Style{}

func segStyle(fg string, bg lipgloss.TerminalColor) lipgloss.Style {
	key := fmt.Sprintf("%s|%v", fg, bg)
	if s, ok := segStyles[key]; ok {
		return s
	}
	s := lipgloss.NewStyle()
	if fg != "" {
		s = s.Foreground(lipgloss.Color(fg))
	}
	if bg != nil {
		s = s.Background(bg)
	}
	segStyles[key] = s
	return s
}

// cell renders one side of a line, wrapped to width; bg nil = no tint.
func cell(segs []seg, width int, bg lipgloss.TerminalColor) []string {
	rows := wrapSegs(segs, width)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		var b strings.Builder
		used := 0
		for _, s := range row {
			b.WriteString(segStyle(s.fg, bg).Render(s.text))
			used += lipgloss.Width(s.text)
		}
		if used < width {
			b.WriteString(segStyle("", bg).Render(strings.Repeat(" ", width-used)))
		}
		out = append(out, b.String())
	}
	return out
}

func (fd *fileDiff) numberWidth() int {
	return max(3, len(strconv.Itoa(max(len(fd.leftRaw), len(fd.rightRaw)))))
}

func lineSegs(hl [][]seg, n int) []seg {
	if n <= 0 || n > len(hl) {
		return nil
	}
	return hl[n-1]
}

func num(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	return styleGutter.Render(fmt.Sprintf("%*d", width, n))
}

// layoutRows renders (and caches) the rows of every line for the current
// width and mode.
func (d *detailModel) layoutRows(fd *fileDiff, cache *diffRows) [][]rowPair {
	sbs := d.isSideBySide()
	dw := d.diffWidth()
	key := fmt.Sprintf("%d|%v", dw, sbs)
	if cache.key == key {
		return cache.rows
	}
	nw := fd.numberWidth()
	lines := fd.inline
	if sbs {
		lines = fd.sbs
	}
	rows := make([][]rowPair, len(lines))
	for i, l := range lines {
		if sbs {
			half := (dw - 1) / 2
			cw := max(5, half-nw-2)
			leftBg, rightBg := tints(l)
			left := cell(lineSegs(fd.leftHL, l.left), cw, leftBg)
			right := cell(lineSegs(fd.rightHL, l.right), max(5, dw-1-half-nw-2), rightBg)
			for j := range max(len(left), len(right)) {
				lnum, rnum := strings.Repeat(" ", nw), strings.Repeat(" ", nw)
				if j == 0 {
					lnum, rnum = num(l.left, nw), num(l.right, nw)
				}
				rows[i] = append(rows[i], rowPair{
					left:  lnum + " " + pick(left, j, cw, leftBg),
					right: rnum + " " + pick(right, j, max(5, dw-1-half-nw-2), rightBg),
				})
			}
			continue
		}
		cw := max(5, dw-1-2*nw-4)
		segs, bg, mark := lineSegs(fd.rightHL, l.right), lipgloss.TerminalColor(nil), " "
		switch l.kind {
		case lineAdd:
			bg, mark = bgAdd, styleGreen.Render("+")
		case lineDelete:
			segs, bg, mark = lineSegs(fd.leftHL, l.left), bgDelete, styleRed.Render("-")
		case lineContext, lineEdit:
		}
		for j, r := range cell(segs, cw, bg) {
			prefix := strings.Repeat(" ", 2*nw+3)
			if j == 0 {
				prefix = num(l.left, nw) + " " + num(l.right, nw) + " " + mark
			}
			rows[i] = append(rows[i], rowPair{right: prefix + " " + r})
		}
	}
	cache.key, cache.rows = key, rows
	return rows
}

// tints picks each side's background: changed lines are tinted, and a side
// with no line in a changed row is greyed out like the web's hatching.
func tints(l diffLine) (left, right lipgloss.TerminalColor) {
	if l.kind == lineContext {
		return nil, nil
	}
	left, right = bgMissing, bgMissing
	if l.left > 0 {
		left = bgDelete
	}
	if l.right > 0 {
		right = bgAdd
	}
	return left, right
}

func pick(rows []string, j, width int, bg lipgloss.TerminalColor) string {
	if j < len(rows) {
		return rows[j]
	}
	return segStyle("", bg).Render(strings.Repeat(" ", width))
}

// lineRowCount is how many screen rows line i takes, threads included.
func (d *detailModel) lineRowCount(i int) int {
	ch, fd := d.currentDiff()
	if fd == nil || ch == nil {
		return 1
	}
	rows := d.layoutRows(fd, d.rowCache(ch))
	if i >= len(rows) {
		return 1
	}
	return len(rows[i]) + len(d.threadRows(ch, i))
}

func (d *detailModel) rowCache(ch *ado.Change) *diffRows {
	key := d.diffKey(ch)
	c, ok := d.rowCaches[key]
	if !ok {
		c = &diffRows{}
		d.rowCaches[key] = c
	}
	return c
}

func (d *detailModel) threadRows(ch *ado.Change, i int) []string {
	lines := d.diffLines()
	if i >= len(lines) {
		return nil
	}
	width := d.diffWidth() - 6
	var out []string
	for ti, t := range d.lineThreads(ch.Item.Path, lines[i]) {
		selected := i == d.files.cursor && ti == 0 && d.files.inThread
		status := styleYellow.Render(ado.ThreadStatusTitle(t.Status))
		if t.IsResolved() {
			status = styleGreen.Render(ado.ThreadStatusTitle(t.Status))
		}
		out = append(out, "    💬 "+status)
		for ci, c := range t.LiveComments() {
			mark := "      "
			if selected && ci == d.files.commentSel {
				mark = "    " + styleSelected.Render("› ")
			}
			out = append(out, mark+styleSection.Render(c.Author.DisplayName)+"  "+
				styleDim.Render(relTime(time.Since(c.PublishedDate), c.PublishedDate)))
			for _, l := range strings.Split(d.md.render(d.resolveMentions(c.Content), max(20, width)), "\n") {
				out = append(out, "      "+l)
			}
		}
	}
	return out
}

// --- Whole tab ---

func (d *detailModel) renderFiles() string {
	h := d.bodyHeight()
	diff := d.renderDiffPane(h)
	tw := d.treeWidth()
	if tw == 0 {
		return strings.Join(diff, "\n")
	}
	tree := d.renderTree(h, tw)
	out := make([]string, h)
	for i := range h {
		out[i] = fit(tree[i], tw) + styleDim.Render("│") + diff[i]
	}
	return strings.Join(out, "\n")
}

func (d *detailModel) renderTree(h, width int) []string {
	f := &d.files
	out := make([]string, h)
	switch {
	case f.err != nil && f.changes == nil:
		out[0] = styleRed.Render(truncate("error: "+f.err.Error(), width))
		return out
	case f.loading && f.changes == nil:
		out[0] = styleDim.Render("loading files…")
		return out
	}
	lines := d.treeLines()
	offset := max(0, f.treeCursor-h+1)
	for i := range h {
		n := offset + i
		if n >= len(lines) {
			break
		}
		prefix := "  "
		if n == f.treeCursor {
			prefix = styleDim.Render("▌ ")
			if f.pane == paneTree {
				prefix = styleSelected.Render("▌ ")
			}
		}
		out[i] = truncate(prefix+lines[n].text, width)
	}
	return out
}

func (d *detailModel) renderDiffPane(h int) []string {
	f := &d.files
	dw := d.diffWidth()
	out := make([]string, h)
	ch, fd := d.currentDiff()
	mode := "inline"
	if d.isSideBySide() {
		mode = "side-by-side"
	}
	header := styleHeader.Render(f.cmp.label)
	if ch != nil {
		header += "  " + ch.Item.Path
	}
	out[0] = truncate(header+styleDim.Render("  · "+mode+" (S) · u compare · z tree"), dw)

	msg := ""
	switch {
	case ch == nil && f.loading:
		msg = "loading…"
	case ch == nil:
		msg = "no file selected"
	case fd == nil:
		msg = "loading diff…"
	case fd.err != nil:
		msg = styleRed.Render("error: " + fd.err.Error())
	case fd.binary:
		msg = "Binary file, open in browser (o)"
	case fd.tooLarge:
		msg = fmt.Sprintf("Too large to render (over %d lines), open in browser (o)", maxDiffLines)
	case len(d.diffLines()) == 0:
		msg = "No content changes"
	}
	if msg != "" {
		out[1] = "  " + styleDim.Render(msg)
		return out
	}

	rows := d.layoutRows(fd, d.rowCache(ch))
	lines := d.diffLines()
	row := 1
	for i := f.top; i < len(lines) && row < h; i++ {
		for _, rp := range rows[i] {
			if row >= h {
				break
			}
			out[row] = d.composeRow(i, lines[i], rp)
			row++
		}
		for _, tr := range d.threadRows(ch, i) {
			if row >= h {
				break
			}
			out[row] = truncate(tr, dw)
			row++
		}
	}
	return out
}

// composeRow adds the cursor and range markers to a rendered row.
func (d *detailModel) composeRow(i int, l diffLine, rp rowPair) string {
	f := &d.files
	marker := func(side ado.Side) string {
		if d.cursorSide(l) != side {
			return " "
		}
		inRange := f.anchor >= 0 && i >= min(f.anchor, f.cursor) && i <= max(f.anchor, f.cursor)
		switch {
		case i == f.cursor && f.pane == paneDiff:
			return styleCursorLine.Render("▌")
		case i == f.cursor:
			return styleDim.Render("▌")
		case inRange:
			return styleRangeLine.Render("┃")
		}
		return " "
	}
	if d.isSideBySide() {
		return marker(ado.SideLeft) + rp.left + " " + marker(ado.SideRight) + rp.right
	}
	return marker(d.cursorSide(l)) + rp.right
}
