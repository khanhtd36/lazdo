package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func (d *detailModel) filesKey(msg tea.KeyMsg) tea.Cmd {
	f := &d.files
	if f.tree.typing {
		return d.treeKey(msg) // letters go into the filter, not commands
	}
	switch msg.String() {
	case "y":
		if f.pane == paneDiff && f.inThread {
			if t, ok := d.diffThread(); ok {
				return d.copyComment(*t, f.commentSel)
			}
		}
		if f.pane == paneDiff && f.anchor >= 0 {
			text, n := d.diffSelectionText()
			f.anchor = -1
			return copyText(text, fmt.Sprintf("copied %d %s", n, plural(n, "line", "lines")))
		}
		if ch := d.selectedChange(); ch != nil {
			return d.copyFile(ch.Item.Path)
		}
		return nil
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
	before := d.selectedChange()
	handled, activate := d.files.tree.key(msg, d.bodyHeight())
	if activate || (!handled && isAnyOf(msg.String(), "l", "right", "tab", "shift+tab")) {
		if d.selectedChange() != nil {
			d.files.pane = paneDiff
		}
	}
	if d.selectedChange() != before {
		d.resetDiffCursor()
		return d.ensureDiff()
	}
	return nil
}

func isAnyOf(s string, options ...string) bool {
	for _, o := range options {
		if s == o {
			return true
		}
	}
	return false
}

// diffSelectionText is the V range's original text: the cursor's side in
// side-by-side, each line's own side inline; rows with no line there skip.
func (d *detailModel) diffSelectionText() (string, int) {
	f := &d.files
	_, fd := d.currentDiff()
	lines := d.diffLines()
	if fd == nil {
		return "", 0
	}
	var out []string
	for i := min(f.anchor, f.cursor); i <= max(f.anchor, f.cursor) && i < len(lines); i++ {
		side := d.cursorSide(lines[i]) // inline: the line's own side
		raw := fd.rightRaw
		if side == ado.SideLeft {
			raw = fd.leftRaw
		}
		if n := lineOn(lines[i], side); n > 0 && n <= len(raw) {
			out = append(out, raw[n-1])
		}
	}
	return strings.Join(out, "\n"), len(out)
}

// copyComment offers a thread's comment text (raw markdown) to copy.
func (d *detailModel) copyComment(t ado.Thread, sel int) tea.Cmd {
	live := t.LiveComments()
	if len(live) == 0 {
		return nil
	}
	c := live[min(sel, len(live)-1)]
	var all []string
	for _, x := range live {
		all = append(all, x.Author.DisplayName+":\n"+x.Content)
	}
	return copyMenu("comment",
		copyItem{"Comment text", c.Content},
		copyItem{"Whole thread", strings.Join(all, "\n\n")},
		copyItem{"Web URL", d.pr.WebURL(d.client.Org) + fmt.Sprintf("?discussionId=%d", t.ID)},
	)
}

func (d *detailModel) copyFile(path string) tea.Cmd {
	return copyMenu("file",
		copyItem{"Path", path},
		copyItem{"Web URL", d.fileURL(path)},
		copyItem{"File name", pathBase(path)},
	)
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (d *detailModel) diffKeyPress(msg tea.KeyMsg) tea.Cmd {
	f := &d.files
	lines := d.diffLines()
	half := max(1, d.bodyHeight()/2)
	edge := f.edge
	f.edge = 0 // only n/N right after stopping at the end moves to another file
	var cmd tea.Cmd
	switch msg.String() {
	case "tab", "shift+tab":
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
	case "n", "N":
		dir := 1
		if msg.String() == "N" {
			dir = -1
		}
		next := nextChange(lines, f.cursor, dir)
		if next == f.cursor {
			// No change left this way: stop once, then go to the next file.
			if edge == dir {
				return d.jumpFile(dir)
			}
			f.edge, cmd = dir, statusCmd(d.edgeHint(dir))
			return cmd
		}
		f.cursor = next
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
	return cmd
}

// fileStep is the tree index of the next (dir 1) or previous (dir -1) file
// row, skipping folders; -1 when there is none.
func (d *detailModel) fileStep(dir int) int {
	t := &d.files.tree
	vis := t.visible()
	for i := t.cursor + dir; i >= 0 && i < len(vis); i += dir {
		if line, _ := t.items[vis[i]].value.(listLine); line.change != nil {
			return i
		}
	}
	return -1
}

func (d *detailModel) edgeHint(dir int) string {
	switch {
	case dir > 0 && d.fileStep(1) < 0:
		return "last change of the last file"
	case dir > 0:
		return "last change in this file · n again for the next file"
	case d.fileStep(-1) < 0:
		return "first change of the first file"
	}
	return "first change in this file · N again for the previous file"
}

// jumpFile opens the next or previous file, landing on its first change
// going forward and its last change going back.
func (d *detailModel) jumpFile(dir int) tea.Cmd {
	i := d.fileStep(dir)
	if i < 0 {
		return statusCmd(d.edgeHint(dir))
	}
	d.files.tree.cursor = i
	d.resetDiffCursor()
	d.files.land = dir
	d.landCursor()
	return d.ensureDiff()
}

// landCursor puts the cursor on the first or last change of a file opened
// by jumpFile, once its diff is there.
func (d *detailModel) landCursor() {
	f := &d.files
	if _, fd := d.currentDiff(); f.land == 0 || fd == nil {
		return
	}
	lines := d.diffLines()
	first, last := -1, -1
	for i, l := range lines {
		if l.kind == lineContext {
			continue
		}
		if first < 0 {
			first = i
		}
		if i == 0 || lines[i-1].kind == lineContext {
			last = i // start of the latest change run
		}
	}
	f.cursor = max(0, first)
	if f.land < 0 {
		f.cursor = max(0, last)
	}
	f.top = max(0, f.cursor-changeContextRows)
	f.land = 0
	d.scrollDiffToCursor()
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
	if dir < 0 && from > 0 && from < len(lines) && changed(from) && changed(from-1) {
		// Inside a change: go back to where it starts first.
		for from > 0 && changed(from-1) {
			from--
		}
		return from
	}
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
			d.resetTree()
			d.rebuildLists()
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
	commits := menuItem{label: "Commits…", disabled: "no commits"}
	if len(d.data.Commits) > 0 {
		commits = menuItem{label: "Commits…", run: func() (modal, tea.Cmd) {
			return d.newCommitPicker(), d.ensureParents()
		}}
	}
	items = append(items, commits)
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
			d.resetTree()
			d.rebuildLists()
			return nil, d.ensureFiles()
		}})
	}
	return &menuModal{title: "Compare to", items: items}
}

func (d *detailModel) openCommitDiff(commit string) tea.Cmd {
	i := d.commitIndex(commit)
	if i < 0 {
		return nil
	}
	d.files.pane = paneTree
	d.tab = tabFiles
	return d.showCommits(i, i)
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
