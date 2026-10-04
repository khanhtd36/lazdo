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

// detailBodyTop is the first screen row of the detail body, below the title,
// subtitle, tabs and rule.
const detailBodyTop = 4

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
	idx := m.offset + msg.Y - 1
	if idx >= len(rows) {
		return m, nil
	}
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
	if !isClick(msg) || y < 0 || y >= m.listHeight() || !l.click(y) {
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
		case 2:
			if t, ok := d.tabAt(msg.X); ok && !d.standalone {
				d.tab = t
				return d.onTabChange()
			}
			return nil
		}
	}
	by := msg.Y - detailBodyTop
	if by < 0 || by >= d.bodyHeight() || d.data == nil {
		return nil
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
	top := detailBodyTop + max(0, (d.bodyHeight()-h)/2)
	left := max(0, (d.width-1-w)/2)
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
	if c, ok := d.modal.(clickableModal); ok {
		var cmd tea.Cmd
		d.modal, cmd = c.clickRow(msg.Y - top - 1) // -1 for the border
		return cmd
	}
	return nil
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
	if d.width >= twoColumnsWidth && msg.X >= d.width-sidebarWidth-3 {
		return nil // the sidebar has nothing to click
	}
	line := d.vp.YOffset + by
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
	dx := msg.X
	if tw > 0 {
		dx -= tw + 1
	}
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
	rows := d.layoutRows(fd, d.rowCache(ch))
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
			onThread := by >= row+len(rows[i])
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
	} else if isClick(msg) {
		f.pane = paneTree
		if f.tree.click(by) {
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
	if isClick(msg) && l.click(by) {
		return d.listKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}
