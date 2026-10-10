package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// columns returns the widths of the branch, tree and content panes, frames
// included; the branch pane is 0 when folded.
func (b *repoBrowser) columns(width int) (branches, tree, content int) {
	if !b.foldBranches(width) {
		branches = browserBranchesWidth + 2
	}
	tree = min(browserTreeWidth, max(20, width/3)) + 2
	return branches, tree, max(22, width-tree-branches)
}

// contentWidth is the file content's width inside its frame.
func (b *repoBrowser) contentWidth(width int) int {
	_, _, c := b.columns(width)
	return c - 2
}

// framed turns a pane's rendering, whose first line is its heading, into a
// framed pane titled by that heading: the frame's top edge takes the
// heading's row, so the rows below keep their places.
func framed(lines []string, width int, focused bool) pane {
	title, rest := "", lines
	if len(lines) > 0 {
		title, rest = ansi.Strip(lines[0]), lines[1:]
	}
	return pane{title: strings.TrimSpace(title), width: width, lines: rest, focused: focused}
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
	var src []int
	if fc.markdown && !b.markdownRaw {
		rows = strings.Split(b.md.render(strings.Join(fc.raw, "\n"), max(20, width-1)), "\n")
		for range rows {
			src = append(src, -1)
		}
	} else {
		nw := max(3, len(fmt.Sprint(len(fc.hl))))
		for i, line := range fc.hl {
			for j, r := range cell(line, max(10, width-nw-2), nil) {
				num := strings.Repeat(" ", nw)
				if j == 0 {
					num = styleGutter.Render(fmt.Sprintf("%*d", nw, i+1))
				}
				rows = append(rows, num+" "+r)
				src = append(src, i)
			}
		}
	}
	b.rendered.key, b.rendered.rows, b.rendered.src = key, rows, src
	b.findMatches(rows) // rows moved: re-find the search hits
	return rows
}

func (b *repoBrowser) view(width, height int) []string {
	switch b.tab {
	case repoTabCommits:
		return b.commitsView(width, height)
	case repoTabTags:
		return b.tagsView(width, height)
	case repoTabFiles, repoTabCount:
	}
	// height is a row short of the screen's: the frames' bottom edge.
	bw, tw, cw := b.columns(width)
	var panes []pane
	if bw > 0 {
		panes = append(panes, framed(b.paneList(&b.branches, bw-2, height, paneBranches, "Branches", !b.branchesLoaded), bw, b.pane == paneBranches))
	}
	panes = append(panes,
		framed(b.paneList(&b.tree, tw-2, height, paneFiles, b.branch, len(b.tree.items) == 0), tw, b.pane == paneFiles),
		framed(b.contentView(cw-2, height), cw, b.pane == paneContent))
	return framePanes(height+1, panes...)
}

func repoTabLabels() []string {
	labels := make([]string, 0, repoTabCount)
	for t := range repoTabCount {
		labels = append(labels, fmt.Sprintf("%d %s", t+1, t.title()))
	}
	return labels
}

// tabsLine is the repo's Files / Commits / Tags switcher.
func (b *repoBrowser) tabsLine(width int) string {
	return tabBar(repoTabLabels(), int(b.tab), width)
}

func (b *repoBrowser) tabAt(x, width int) (repoTab, bool) {
	t, ok := tabAt(repoTabLabels(), x, width)
	return repoTab(t), ok
}

// commitsView is the branch pane beside the branch's commits, or a tag's
// release changes.
func (b *repoBrowser) commitsView(width, height int) []string {
	h := &b.hist
	bw := 0
	if !b.foldBranches(width) {
		bw = browserBranchesWidth + 2
	}
	cw := width - bw
	head := "commits on " + b.branch
	switch {
	case h.release != nil && h.release.hasPrev:
		head = fmt.Sprintf("%s · release changes since %s (%d)", h.release.tag.Name, h.release.prev.Name, len(h.commitList.items))
	case h.release != nil:
		head = h.release.tag.Name + " · no earlier version tag"
	}
	loading := h.commitList.items == nil && (h.commitsLoading || h.release != nil)
	list := framed(b.paneList(&h.commitList, cw-2, height, paneFiles, head, loading), cw, b.pane == paneFiles)
	if bw == 0 {
		return framePanes(height+1, list)
	}
	branches := framed(b.paneList(&b.branches, bw-2, height, paneBranches, "Branches", !b.branchesLoaded), bw, b.pane == paneBranches)
	return framePanes(height+1, branches, list)
}

// tagsView lists the tags with the selected annotated tag's message below.
func (b *repoBrowser) tagsView(width, height int) []string {
	h := &b.hist
	head := fmt.Sprintf("Tags (%d) · newest version first", len(h.tags))
	inner := width - 2
	list := b.paneList(&h.tagList, inner, height-2, paneFiles, head, !h.tagsLoaded)
	detail := ""
	if t, ok := b.selectedTag(); ok {
		switch info, seen := h.tagInfo[t.Name]; {
		case !t.Annotated():
			detail = "lightweight tag on " + shortSHA(t.CommitID)
		case !seen || info == nil:
			detail = "loading…"
		default:
			detail = info.Tagger + " · " + relTime(time.Since(info.Date), info.Date) + " · " + strings.ReplaceAll(info.Message, "\n", " ")
		}
	}
	lines := append(list, styleDim.Render(strings.Repeat("─", max(0, inner))), truncate(styleDim.Render(detail), inner))
	return framePanes(height+1, framed(lines, width, true))
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
	lo, hi := b.selection()
	for i := 1; i < height; i++ {
		n := b.top + i - 1
		if n >= len(rows) {
			break
		}
		mark := " "
		switch {
		case n == b.cur && b.pane == paneContent:
			mark = styleCursorLine.Render("▌")
		case n >= lo && n <= hi:
			mark = styleRangeLine.Render("┃")
		case matched[n]:
			mark = styleYellow.Render("▌")
		}
		out[i] = mark + truncate(rows[n], width-1)
	}
	return out
}

// selection is the selected row range; lo > hi when nothing is selected.
func (b *repoBrowser) selection() (lo, hi int) {
	if b.anchor < 0 {
		return 1, 0
	}
	return min(b.anchor, b.cur), max(b.anchor, b.cur)
}

// selectedText is the source of the selected rows: whole source lines for
// code (no numbers, no wrap breaks), the plain text for rendered markdown.
func (b *repoBrowser) selectedText(width int) string {
	fc := b.currentContent()
	rows := b.contentRows(b.contentWidth(width))
	lo, hi := b.selection()
	if fc == nil || lo > hi {
		return ""
	}
	var out []string
	last := -1
	for i := lo; i <= hi && i < len(rows); i++ {
		switch src := b.rendered.src[i]; {
		case src < 0:
			out = append(out, strings.TrimRight(ansi.Strip(rows[i]), " "))
		case src != last:
			out = append(out, fc.raw[src])
			last = src
		}
	}
	return strings.Join(out, "\n")
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
	case x < bw+tw:
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
	// Press and drag over content rows selects them; y copies.
	if pane == paneContent && by > 0 && (isClick(msg) || isDrag(msg)) {
		row := min(b.top+by-1, len(b.contentRows(b.contentWidth(width)))-1)
		if row < 0 {
			return nil
		}
		if isClick(msg) {
			b.pane, b.cur, b.anchor, b.dragFrom = paneContent, row, -1, row
		} else if row != b.dragFrom {
			b.anchor, b.cur = b.dragFrom, row
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
