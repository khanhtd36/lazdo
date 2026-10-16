package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Mouse follows lazygit: a click selects, a click on what is already
// selected opens or activates it, and the wheel scrolls what is under the
// pointer.

const wheelStep = 3

func isClick(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
}

// isDrag reports the pointer moving with the left button held.
func isDrag(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft
}

func wheelDelta(msg tea.MouseMsg) int {
	if msg.Action != tea.MouseActionPress {
		return 0
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -wheelStep
	case tea.MouseButtonWheelDown:
		return wheelStep
	default:
		return 0
	}
}

// --- Dashboard ---

func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.modal != nil {
		return m.modalMouse(msg)
	}
	if m.detail != nil {
		return m, m.detail.onMouse(msg)
	}
	if m.project != nil {
		return m, m.project.onMouse(msg)
	}
	if isClick(msg) && msg.Y == 0 {
		if p, ok := m.pageAt(msg.X); ok {
			return m.switchPage(p)
		}
		if m.onOrgName(msg.X) {
			return m.openOrgPicker()
		}
		return m, nil
	}
	if m.page == pageProjects {
		return m.projectsMouse(msg)
	}
	if d := wheelDelta(msg); d != 0 {
		m.cursor += d
		m.clampCursor()
		return m, nil
	}
	if !isClick(msg) || msg.Y < 1 || msg.Y > m.listHeight() {
		return m, nil
	}
	rows := m.rows()
	lines := dashLines(rows)
	at := m.offset + msg.Y - 1
	if at >= len(lines) || lines[at].bottom {
		return m, nil
	}
	idx := lines[at].row
	r := rows[idx]
	switch {
	case r.pr == nil:
		m.cursor = idx
		kind := m.sections[r.section].Kind
		m.collapsed[kind] = !m.collapsed[kind]
	case idx == m.cursor:
		return m.openDetail(r.pr)
	default:
		m.cursor = idx
	}
	m.clampCursor()
	return m, nil
}

func (m Model) projectsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	l := &m.projects.list
	if d := wheelDelta(msg); d != 0 {
		l.wheel(d)
		return m, nil
	}
	y := msg.Y - 1 // below the title line
	if !isClick(msg) || y < 0 || y >= m.listHeight() || !m.projects.click(y) {
		return m, nil
	}
	return m.onProjectsKey(tea.KeyMsg{Type: tea.KeyEnter})
}

// --- Detail ---

func (d *detailModel) onMouse(msg tea.MouseMsg) tea.Cmd {
	if d.modal != nil {
		return d.modalMouse(msg)
	}
	if isClick(msg) {
		switch msg.Y {
		case 0:
			return d.clickTitle(msg.X)
		case 1:
			if d.data != nil && d.onTarget(msg.X) {
				var cmd tea.Cmd
				d.modal, cmd = d.openTargetPicker()
				return cmd
			}
			return nil
		case 2:
			if t, ok := d.tabAt(msg.X); ok && !d.standalone && d.run == nil {
				d.tab = t
				return d.onTabChange()
			}
			return nil
		}
	}
	by := msg.Y - d.bodyTop()
	if by < 0 || by >= d.bodyHeight() || d.data == nil {
		return nil
	}
	if d.run != nil {
		return d.run.onMouse(msg, by, d.width, d.bodyHeight())
	}
	switch d.tab {
	case tabOverview:
		return d.overviewMouse(msg, by)
	case tabFiles:
		return d.filesMouse(msg, by)
	case tabCommits, tabConflicts:
		return d.listMouse(msg, by)
	case tabCount:
	}
	return nil
}

func (d *detailModel) clickTitle(x int) tea.Cmd {
	if d.data == nil || d.standalone {
		return nil
	}
	vote, _, voteX, completeX := d.titleButtons()
	switch {
	case x >= voteX && x < voteX+ansi.StringWidth(vote):
		d.modal = d.voteMenu()
	case x >= completeX:
		d.modal = d.completeMenu()
	}
	return nil
}

// clickableModal is a popup whose content rows react to clicks.
type clickableModal interface {
	clickRow(row int) (modal, tea.Cmd)
}

// modalMouse routes clicks inside the popup to it; a click outside a menu
// or confirmation closes it (editors keep their text instead).
func (d *detailModel) modalMouse(msg tea.MouseMsg) tea.Cmd {
	box := d.modal.view(d.width)
	h, w := strings.Count(box, "\n")+1, ansi.StringWidth(firstLine(box))
	// lipgloss.Place centers with the smaller half of the gap before the box.
	top := d.bodyTop() + max(0, (d.bodyHeight()-h)/2)
	left := max(0, (d.width-w)/2)
	inside := msg.Y >= top && msg.Y < top+h && msg.X >= left && msg.X < left+w

	if menu, ok := d.modal.(*menuModal); ok {
		if delta := wheelDelta(msg); delta != 0 {
			menu.cursor = max(0, min(menu.cursor+sign(delta), len(menu.items)-1))
			return nil
		}
	}
	if !isClick(msg) {
		return nil
	}
	if !inside {
		switch d.modal.(type) {
		case *menuModal, *confirmModal:
			d.modal = nil
		}
		return nil
	}
	if b, ok := d.modal.(buttonModal); ok {
		next, cmd, hit := clickButtons(b, box, msg.X-left, msg.Y-top)
		if hit {
			d.modal = next
		}
		return cmd
	}
	if c, ok := d.modal.(clickableModal); ok {
		var cmd tea.Cmd
		d.modal, cmd = c.clickRow(msg.Y - top - 1) // -1 for the border
		return cmd
	}
	return nil
}

// modalMouse is a click on a dialog over the whole screen; the page under
// it takes none. Outside, a click closes a menu or a question.
func (m Model) modalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	box := m.modal.view(m.width)
	h, w := strings.Count(box, "\n")+1, ansi.StringWidth(firstLine(box))
	top, left := max(0, (m.height-h)/2), max(0, (m.width-w)/2)
	inside := msg.Y >= top && msg.Y < top+h && msg.X >= left && msg.X < left+w
	if !isClick(msg) {
		return m, nil
	}
	var cmd tea.Cmd
	switch dm := m.modal.(type) {
	case buttonModal:
		if !inside {
			if _, isConfirm := dm.(*confirmModal); isConfirm {
				m.modal = nil
			}
			return m, nil
		}
		next, c, hit := clickButtons(dm, box, msg.X-left, msg.Y-top)
		if hit {
			m.modal, cmd = next, c
		}
	case *menuModal:
		if !inside {
			m.modal = nil
			return m, nil
		}
		m.modal, cmd = dm.clickRow(msg.Y - top - 1)
	}
	return m, cmd
}

// clickRow runs the clicked item; rows 0-1 are the title and a blank line.
func (m *menuModal) clickRow(row int) (modal, tea.Cmd) {
	i := row - 2
	if i < 0 || i >= len(m.items) {
		return m, nil
	}
	m.cursor = i
	return m.update(tea.KeyMsg{Type: tea.KeyEnter})
}

func (d *detailModel) overviewMouse(msg tea.MouseMsg, by int) tea.Cmd {
	if delta := wheelDelta(msg); delta != 0 {
		if delta < 0 {
			d.vp.ScrollUp(-delta)
		} else {
			d.vp.ScrollDown(delta)
		}
		return nil
	}
	if !isClick(msg) {
		return nil
	}
	if msg.X >= d.overviewWidth() {
		// The Reviewers pane, atop the side panes, opens the reviewers.
		if d.sideBySide() && by < len(d.sidebarPanes(sidebarWidth)[0].lines)+2 {
			return d.openReviewers()
		}
		return nil
	}
	if by < 1 {
		return nil // the frame
	}
	line := d.vp.YOffset + by - 1 // below the pane's top edge
	if p, ok := d.buildLines[line]; ok {
		return d.openPolicyRun(p)
	}
	entry := -1
	for i, start := range d.entryLines {
		if line >= start {
			entry = i
		}
	}
	if entry < 0 {
		return nil
	}
	if entry == d.threadSel {
		if e, ok := d.selectedEntry(); ok && e.isHuman {
			d.inThread, d.commentSel = true, 0
		}
	} else {
		d.threadSel, d.inThread = entry, false
	}
	d.rebuildOverview()
	return nil
}

func (d *detailModel) filesMouse(msg tea.MouseMsg, by int) tea.Cmd {
	f := &d.files
	tw := d.treeWidth()
	if tw > 0 && msg.X < tw {
		return d.treeMouse(msg, by)
	}
	dx := msg.X - tw - 1 // past the tree and the diff frame's left edge
	lines := d.diffLines()
	if delta := wheelDelta(msg); delta != 0 {
		f.cursor = max(0, min(f.cursor+delta, len(lines)-1))
		f.inThread = false
		d.scrollDiffToCursor()
		return nil
	}
	if (!isClick(msg) && !isDrag(msg)) || by == 0 {
		return nil // row 0 is the diff header
	}
	ch, fd := d.currentDiff()
	if ch == nil || fd == nil || len(lines) == 0 {
		return nil
	}
	cache := d.rowCache(ch)
	row := 1
	for i := f.top; i < len(lines); i++ {
		n := d.lineRowCount(i)
		if by < row+n && isDrag(msg) {
			// Dragging over lines selects them, like V; y copies, a comments.
			if i != f.drag {
				f.anchor, f.cursor, f.inThread = f.drag, i, false
			}
			return nil
		}
		if by < row+n {
			f.drag, f.anchor = i, -1
			wasCursor := f.cursor == i && f.pane == paneDiff
			f.cursor, f.pane = i, paneDiff
			if d.isSideBySide() {
				f.side = ado.SideRight
				if dx <= (d.diffWidth()-1)/2 {
					f.side = ado.SideLeft
				}
			}
			// A click on the line's thread, or a second click on the line,
			// steps into the thread.
			above := 0 // the file's comments, atop its first line
			if i == 0 {
				above = len(d.fileThreadRows(ch))
			}
			onThread := by < row+above || by >= row+above+len(d.lineRows(fd, cache, i))
			_, hasThread := d.diffThread()
			f.inThread = hasThread && (onThread || wasCursor)
			f.commentSel = 0
			return nil
		}
		row += n
	}
	return nil
}

func (d *detailModel) treeMouse(msg tea.MouseMsg, by int) tea.Cmd {
	f := &d.files
	before := d.selectedChange()
	if delta := wheelDelta(msg); delta != 0 {
		f.tree.wheel(delta)
	} else if isClick(msg) && by > 0 && by < d.bodyHeight()-1 { // inside the frame
		f.pane = paneTree
		if f.tree.click(by - 1) {
			f.pane = paneDiff
		}
	}
	if d.selectedChange() != before {
		d.resetDiffCursor()
		return d.ensureDiff()
	}
	return nil
}

func (d *detailModel) listMouse(msg tea.MouseMsg, by int) tea.Cmd {
	l := &d.lists[d.tab]
	if delta := wheelDelta(msg); delta != 0 {
		l.wheel(delta)
		return nil
	}
	if isClick(msg) && l.click(by-1) { // below the pane's top edge
		return d.listKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}
