package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func (d *detailModel) filesKey(msg tea.KeyMsg) tea.Cmd {
	f := &d.files
	switch msg.String() {
	case "z":
		f.hideTree = !f.hideTree
		if f.hideTree {
			f.pane = paneDiff
		}
		return nil
	case "S":
		f.sideBySide, f.modeChosen = !d.isSideBySide(), true
		f.cursor, f.top, f.anchor, f.inThread = 0, 0, -1, false
		return nil
	case "u":
		if d.data != nil {
			d.modal = d.comparisonMenu()
		}
		return nil
	}
	if f.pane == paneTree {
		return d.treeKey(msg)
	}
	return d.diffKeyPress(msg)
}

func (d *detailModel) treeKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		d.moveTree(1)
	case "k", "up":
		d.moveTree(-1)
	case "g", "home":
		d.files.treeCursor = 0
		d.moveTree(1)
	case "G", "end":
		d.files.treeCursor = len(d.treeLines())
		d.moveTree(-1)
	case "enter", "l", "right", "tab":
		if d.selectedChange() != nil {
			d.files.pane = paneDiff
		}
		return nil
	default:
		return nil
	}
	return d.ensureDiff()
}

func (d *detailModel) diffKeyPress(msg tea.KeyMsg) tea.Cmd {
	f := &d.files
	lines := d.diffLines()
	half := max(1, d.bodyHeight()/2)
	switch msg.String() {
	case "tab":
		if !f.hideTree {
			f.pane = paneTree
		}
	case "h", "left":
		if d.isSideBySide() && f.side == ado.SideRight {
			f.side = ado.SideLeft
		} else if !f.hideTree {
			f.pane = paneTree
		}
	case "l", "right":
		f.side = ado.SideRight
	case "j", "down":
		if f.inThread {
			return d.moveDiffComment(1)
		}
		f.cursor++
	case "k", "up":
		if f.inThread {
			return d.moveDiffComment(-1)
		}
		f.cursor--
	case "ctrl+d", "pgdown":
		f.cursor += half
	case "ctrl+u", "pgup":
		f.cursor -= half
	case "g", "home":
		f.cursor = 0
	case "G", "end":
		f.cursor = len(lines) - 1
	case "n", "p":
		dir := 1
		if msg.String() == "p" {
			dir = -1
		}
		f.cursor = nextChange(lines, f.cursor, dir)
		f.top = max(0, f.cursor-changeContextRows) // show a few lines above the change
	case "V":
		if f.anchor >= 0 {
			f.anchor = -1
		} else {
			f.anchor = f.cursor
		}
	case "a":
		return d.lineComment()
	case "enter":
		if _, ok := d.diffThread(); ok {
			f.inThread, f.commentSel = true, 0
		}
		return nil
	case "R", "s", "e", "d":
		return d.threadAction(msg.String())
	default:
		return nil
	}
	f.inThread = false // any cursor move leaves the thread
	f.cursor = max(0, min(f.cursor, len(lines)-1))
	d.scrollDiffToCursor()
	return nil
}

func (d *detailModel) moveDiffComment(delta int) tea.Cmd {
	if t, ok := d.diffThread(); ok {
		d.files.commentSel = max(0, min(d.files.commentSel+delta, len(t.LiveComments())-1))
	}
	return nil
}

const changeContextRows = 5

// nextChange finds the start of the next (dir 1) or previous (dir -1) run of
// changed lines.
func nextChange(lines []diffLine, from, dir int) int {
	changed := func(i int) bool { return lines[i].kind != lineContext }
	i := from
	// Leave the run the cursor is in first.
	for i >= 0 && i < len(lines) && changed(i) {
		i += dir
	}
	for i >= 0 && i < len(lines) && !changed(i) {
		i += dir
	}
	if i < 0 || i >= len(lines) {
		return from
	}
	if dir < 0 {
		for i > 0 && changed(i-1) {
			i--
		}
	}
	return i
}

// --- Comparison picker ---

func (d *detailModel) comparisonMenu() modal {
	last := d.lastPush()
	pick := func(c comparison) func() (modal, tea.Cmd) {
		return func() (modal, tea.Cmd) {
			d.files.cmp = c
			d.files.treeCursor, d.files.cursor, d.files.top, d.files.anchor = 0, 0, 0, -1
			return nil, d.ensureFiles()
		}
	}
	items := []menuItem{{label: "All changes", run: pick(d.allChanges())}}

	since := menuItem{label: "Since my last visit", disabled: "no previous visit recorded"}
	if !d.prevVisit.IsZero() {
		base := 0
		for _, p := range d.data.Pushes {
			if !p.CreatedDate.After(d.prevVisit) {
				base = p.ID
			}
		}
		switch base {
		case last:
			since.disabled = "nothing pushed since your last visit"
		default:
			since = menuItem{label: "Since my last visit", run: pick(comparison{
				label: "Since my last visit", target: last, base: base,
			})}
		}
	}
	items = append(items, since)

	for i := len(d.data.Pushes) - 1; i >= 0; i-- {
		p := d.data.Pushes[i]
		label := fmt.Sprintf("Update %d · %s · %s", p.ID, p.Author.DisplayName, relTime(time.Since(p.CreatedDate), p.CreatedDate))
		items = append(items, menuItem{label: label, run: pick(comparison{
			label: fmt.Sprintf("Update %d", p.ID), target: p.ID, base: p.ID - 1,
		})})
	}
	items = append(items, menuItem{label: "Compare two updates…", run: func() (modal, tea.Cmd) {
		return d.compareBaseMenu(), nil
	}})
	return &menuModal{title: "Compare", items: items}
}

func (d *detailModel) compareBaseMenu() modal {
	items := make([]menuItem, 0, len(d.data.Pushes))
	items = append(items, menuItem{label: "Merge base (before update 1)", run: func() (modal, tea.Cmd) { return d.compareTargetMenu(0), nil }})
	for _, p := range d.data.Pushes[:max(0, len(d.data.Pushes)-1)] {
		items = append(items, menuItem{label: fmt.Sprintf("Update %d", p.ID), run: func() (modal, tea.Cmd) {
			return d.compareTargetMenu(p.ID), nil
		}})
	}
	return &menuModal{title: "Compare from", items: items}
}

func (d *detailModel) compareTargetMenu(base int) modal {
	var items []menuItem
	for i := len(d.data.Pushes) - 1; i >= 0; i-- {
		p := d.data.Pushes[i]
		if p.ID <= base {
			continue
		}
		label := fmt.Sprintf("Update %d vs %d", p.ID, base)
		if base == 0 {
			label = fmt.Sprintf("Update %d vs merge base", p.ID)
		}
		items = append(items, menuItem{label: fmt.Sprintf("Update %d", p.ID), run: func() (modal, tea.Cmd) {
			d.files.cmp = comparison{label: label, target: p.ID, base: base}
			d.files.treeCursor, d.files.cursor, d.files.top, d.files.anchor = 0, 0, 0, -1
			return nil, d.ensureFiles()
		}})
	}
	return &menuModal{title: "Compare to", items: items}
}

func (d *detailModel) openCommitDiff(commit string) tea.Cmd {
	label := "Commit " + shortSHA(commit)
	for _, c := range d.data.Commits {
		if c.ID == commit {
			label += " · " + firstLine(c.Comment)
		}
	}
	d.files.cmp = comparison{label: label, commit: commit}
	d.files.treeCursor, d.files.cursor, d.files.top, d.files.anchor = 0, 0, 0, -1
	d.files.pane = paneTree
	d.tab = tabFiles
	return d.ensureFiles()
}

// --- Line comments ---

func (d *detailModel) lineComment() tea.Cmd {
	f := &d.files
	if f.cmp.commit != "" {
		return statusCmd("comments need a comparison of updates; press u and pick one")
	}
	ch, fd := d.currentDiff()
	lines := d.diffLines()
	if ch == nil || fd == nil || f.cursor >= len(lines) {
		return statusCmd("select a file first")
	}
	side := d.cursorSide(lines[f.cursor])
	from, to := f.cursor, f.cursor
	if f.anchor >= 0 {
		from, to = min(f.anchor, f.cursor), max(f.anchor, f.cursor)
	}
	start, end := 0, 0
	for i := from; i <= to; i++ {
		if n := lineOn(lines[i], side); n > 0 {
			if start == 0 {
				start = n
			}
			end = n
		}
	}
	if start == 0 {
		return statusCmd("no line on this side here; h/l switches side")
	}
	raw := fd.rightRaw
	if side == ado.SideLeft {
		raw = fd.leftRaw
	}
	first := f.cmp.base
	if first == 0 {
		first = f.cmp.target
	}
	lc := ado.LineComment{
		Path: ch.Item.Path, Side: side, StartLine: start, EndLine: end,
		EndLineLength:    len([]rune(raw[end-1])),
		ChangeTrackingID: ch.ChangeTrackingID,
		FirstIteration:   first, SecondIteration: f.cmp.target,
	}
	title := fmt.Sprintf("Comment on %s line %d", ch.Item.Path, start)
	if end != start {
		title = fmt.Sprintf("Comment on %s lines %d–%d", ch.Item.Path, start, end)
	}
	d.modal = newEditor(title, "", d.width, func(content string) tea.Cmd {
		f.anchor = -1
		return d.act("comment posted", false, func(ctx context.Context) error {
			return d.client.NewLineThread(ctx, d.pr, lc, content)
		})
	})
	return nil
}

// threadAction runs reply/status/edit/delete on the thread at the cursor.
func (d *detailModel) threadAction(key string) tea.Cmd {
	t, ok := d.diffThread()
	if !ok {
		return statusCmd("no comment thread on this line")
	}
	return d.threadKey(key, *t, d.files.inThread, d.files.commentSel)
}

func (d *detailModel) scrollDiffToCursor() {
	f := &d.files
	if f.cursor < f.top {
		f.top = f.cursor
		return
	}
	height := d.bodyHeight() - 1 // one line for the diff header
	for f.top < f.cursor {
		rows := 0
		for i := f.top; i <= f.cursor; i++ {
			rows += d.lineRowCount(i)
		}
		if rows <= height {
			return
		}
		f.top++
	}
}
