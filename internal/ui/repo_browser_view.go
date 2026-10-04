package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// columns returns the widths of the branch, tree and content panes; the
// branch pane is 0 when folded.
func (b *repoBrowser) columns(width int) (branches, tree, content int) {
	if !b.foldBranches(width) {
		branches = browserBranchesWidth
	}
	tree = min(browserTreeWidth, max(20, width/3))
	content = width - tree - 1
	if branches > 0 {
		content -= branches + 1
	}
	return branches, tree, max(20, content)
}

func (b *repoBrowser) contentWidth(width int) int {
	_, _, c := b.columns(width)
	return c
}

func (b *repoBrowser) currentContent() *fileContent {
	if b.file == "" {
		return nil
	}
	return b.contents[folderKey(b.branch, b.file)]
}

// contentRows renders the file into screen rows for the given width: the
// markdown view, or numbered, highlighted and wrapped source.
func (b *repoBrowser) contentRows(width int) []string {
	fc := b.currentContent()
	if fc == nil || fc.err != nil || fc.binary || fc.tooLarge {
		return nil
	}
	key := fmt.Sprintf("%s|%s|%d|%v", b.branch, b.file, width, b.markdownRaw)
	if b.rendered.key == key {
		return b.rendered.rows
	}
	var rows []string
	if fc.markdown && !b.markdownRaw {
		rows = strings.Split(b.md.render(strings.Join(fc.raw, "\n"), max(20, width-1)), "\n")
	} else {
		nw := max(3, len(fmt.Sprint(len(fc.hl))))
		for i, line := range fc.hl {
			for j, r := range cell(line, max(10, width-nw-2), nil) {
				num := strings.Repeat(" ", nw)
				if j == 0 {
					num = styleGutter.Render(fmt.Sprintf("%*d", nw, i+1))
				}
				rows = append(rows, num+" "+r)
			}
		}
	}
	b.rendered.key, b.rendered.rows = key, rows
	b.findMatches(rows) // rows moved: re-find the search hits
	return rows
}

func (b *repoBrowser) view(width, height int) []string {
	bw, tw, cw := b.columns(width)
	var branchCol []string
	if bw > 0 {
		branchCol = b.paneList(&b.branches, bw, height, paneBranches, "branches", !b.branchesLoaded)
	}
	treeCol := b.paneList(&b.tree, tw, height, paneFiles, b.branch, len(b.tree.items) == 0)
	contentCol := b.contentView(cw, height)

	out := make([]string, height)
	sep := styleDim.Render("│")
	for i := range height {
		line := ""
		if bw > 0 {
			line = fit(branchCol[i], bw) + sep
		}
		out[i] = line + fit(treeCol[i], tw) + sep + contentCol[i]
	}
	return out
}

// paneList renders a list with a one-line heading; the heading is
// highlighted on the focused pane.
func (b *repoBrowser) paneList(l *pickList, width, height int, pane browserPane, title string, loading bool) []string {
	head := styleDim.Render(title)
	if b.pane == pane {
		head = styleSelected.Render(title)
	}
	out := []string{truncate(head, width)}
	if loading {
		return padLines(append(out, styleDim.Render("  loading…")), height)
	}
	return append(out, l.view(width, height-1)...)
}

func (b *repoBrowser) contentView(width, height int) []string {
	out := make([]string, height)
	head := b.file
	if head == "" {
		head = "no file selected"
	}
	style := styleDim
	if b.pane == paneContent {
		style = styleSelected
	}
	fc := b.currentContent()
	if fc != nil && fc.markdown {
		if b.markdownRaw {
			head += "  (raw · M rendered)"
		} else {
			head += "  (rendered · M raw)"
		}
	}
	out[0] = truncate(style.Render(head), width)
	if line, ok := b.searchLine(); ok {
		out[0] = truncate(line, width)
	}

	msg := ""
	switch {
	case b.file == "":
		msg = "pick a file in the tree"
	case fc == nil:
		msg = "loading…"
	case fc.err != nil:
		msg = styleRed.Render("error: " + fc.err.Error())
	case fc.binary:
		msg = "Binary file, open in browser (o)"
	case fc.tooLarge:
		msg = fmt.Sprintf("Too large to show (over %d lines), open in browser (o)", maxDiffLines)
	}
	if msg != "" {
		out[1] = " " + styleDim.Render(msg)
		return out
	}

	rows := b.contentRows(width)
	matched := map[int]bool{}
	for _, m := range b.matches {
		matched[m] = true
	}
	for i := 1; i < height; i++ {
		n := b.top + i - 1
		if n >= len(rows) {
			break
		}
		mark := " "
		if matched[n] {
			mark = styleYellow.Render("▌")
		}
		out[i] = mark + truncate(rows[n], width-1)
	}
	return out
}

func (b *repoBrowser) searchLine() (string, bool) {
	switch {
	case b.searching:
		return styleSelected.Render("/"+b.search+"▏") + styleDim.Render(fmt.Sprintf("  %d lines · enter keep · esc clear", len(b.matches))), true
	case b.search != "":
		return styleDim.Render(fmt.Sprintf("/%s  %d/%d · n/N next/previous · esc clear", b.search, min(b.match+1, len(b.matches)), len(b.matches))), true
	}
	return "", false
}

func (b *repoBrowser) onMouse(msg tea.MouseMsg, by, width, height int) tea.Cmd {
	bw, tw, _ := b.columns(width)
	x := msg.X
	pane := paneContent
	switch {
	case bw > 0 && x < bw:
		pane = paneBranches
	case x < bw+tw+1 || (bw == 0 && x < tw):
		pane = paneFiles
	}
	if d := wheelDelta(msg); d != 0 {
		switch pane {
		case paneBranches:
			b.branches.wheel(d)
		case paneFiles:
			b.tree.wheel(d)
		case paneContent:
			b.top = max(0, min(b.top+d, len(b.contentRows(b.contentWidth(width)))-1))
		}
		return nil
	}
	if !isClick(msg) || by == 0 {
		return nil // row 0 holds the pane headings
	}
	b.pane = pane
	var l *pickList
	switch pane {
	case paneBranches:
		l = &b.branches
	case paneFiles:
		l = &b.tree
	case paneContent:
		return nil
	}
	if l.click(by - 1) {
		_, cmd := b.key(tea.KeyMsg{Type: tea.KeyEnter}, width, height)
		return cmd
	}
	return nil
}
