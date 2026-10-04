package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// projectBodyTop is the first body row: below the title, tabs and rule.
const projectBodyTop = 3

func (m *projectModel) view() string {
	var b strings.Builder
	b.WriteString(truncate(m.titleLine(), m.width-1) + "\n")
	b.WriteString(truncate(m.tabsLine(), m.width-1) + "\n")
	b.WriteString(styleDim.Render(strings.Repeat("─", max(0, m.width-1))) + "\n")
	for _, line := range m.body() {
		b.WriteString(truncate(line, m.width-1) + "\n")
	}
	status := m.status
	switch {
	case m.err != nil:
		status = styleRed.Render("error: " + m.err.Error())
	case strings.HasPrefix(status, "error"):
		status = styleRed.Render(status)
	}
	b.WriteString(truncate(status, m.width-1) + "\n")
	b.WriteString(styleDim.Render(truncate(m.help(), m.width-1)))
	return b.String()
}

func (m *projectModel) titleLine() string {
	crumbs := []string{styleTitle.Render(m.project.Name)}
	switch m.level {
	case levelBranches:
		crumbs = append(crumbs, "Repos", styleTitle.Render(m.repo.Name), "branches")
	case levelRuns:
		crumbs = append(crumbs, "Pipelines", styleTitle.Render(m.pipeline.Name), "runs")
	case levelRun:
		crumbs = append(crumbs, "Pipelines", m.pipeline.Name, styleTitle.Render("run "+m.run.run.BuildNumber))
		crumbs = append(crumbs, runGlyph(m.run.run.Status, m.run.run.Result)+" "+m.run.run.Branch())
	case levelTabs:
	}
	s := strings.Join(crumbs, styleDim.Render(" › "))
	if m.busy() {
		s += styleYellow.Render("  loading…")
	}
	return s
}

func (m *projectModel) busy() bool {
	switch m.level {
	case levelBranches:
		return m.branchesLoading
	case levelRuns:
		return m.runsLoading
	case levelRun:
		return m.run.loading
	case levelTabs:
	}
	return m.tab == projTabPipelines && m.pipelinesLoading
}

func (m *projectModel) tabLabels() []string {
	counts := [projTabCount]int{len(m.prs), len(m.repos), len(m.pipelines)}
	labels := make([]string, 0, projTabCount)
	for t := range projTabCount {
		label := fmt.Sprintf("%d %s", t+1, t.title())
		if t != projTabPipelines || m.pipelinesLoaded {
			label += fmt.Sprintf(" (%d)", counts[t])
		}
		labels = append(labels, label)
	}
	return labels
}

func (m *projectModel) tabsLine() string {
	parts := make([]string, 0, projTabCount)
	for t, label := range m.tabLabels() {
		if projectTab(t) == m.tab {
			parts = append(parts, styleTabActive.Render(label))
		} else {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, tabGap)
}

func (m *projectModel) tabAt(x int) (projectTab, bool) {
	start := 0
	for t, label := range m.tabLabels() {
		end := start + ansi.StringWidth(label)
		if x >= start && x < end {
			return projectTab(t), true
		}
		start = end + len(tabGap)
	}
	return 0, false
}

func (m *projectModel) body() []string {
	h, w := m.bodyHeight(), m.width-1
	switch m.level {
	case levelBranches:
		if m.branchesLoading && m.branchList.items == nil {
			return padLines([]string{styleDim.Render("  loading branches…")}, h)
		}
		return m.branchList.view(w, h)
	case levelRuns:
		if m.runsLoading && m.runList.items == nil {
			return padLines([]string{styleDim.Render("  loading runs…")}, h)
		}
		return m.runList.view(w, h)
	case levelRun:
		return m.run.view(w, h)
	case levelTabs:
	}
	if m.tab == projTabPipelines && !m.pipelinesLoaded {
		return padLines([]string{styleDim.Render("  loading pipelines…")}, h)
	}
	return m.lists[m.tab].view(w, h)
}

func padLines(lines []string, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

func (m *projectModel) help() string {
	common := "? help  / search  o browser  r refresh  esc back"
	switch m.level {
	case levelBranches:
		return "j/k move  y copy name  Y copy ssh URL  c checkout  " + common
	case levelRuns:
		return "j/k move  enter open run  " + common
	case levelRun:
		return "j/k move  tab/h/l steps ⇄ log  g/G top/follow  " + common
	case levelTabs:
	}
	switch m.tab {
	case projTabRepos:
		return "1-3 tabs  enter branches  y copy URL  Y copy ssh URL  " + common
	case projTabPipelines:
		return "1-3 tabs  enter runs  " + common
	case projTabPRs, projTabCount:
	}
	return "1-3 tabs  enter open PR  y copy URL  " + common
}

// currentList is the list the body shows, nil on the run view.
func (m *projectModel) currentList() *pickList {
	switch m.level {
	case levelBranches:
		return &m.branchList
	case levelRuns:
		return &m.runList
	case levelRun:
		return nil
	case levelTabs:
	}
	return &m.lists[m.tab]
}

func (m *projectModel) onMouse(msg tea.MouseMsg) tea.Cmd {
	if isClick(msg) && msg.Y == 1 && m.level == levelTabs {
		if t, ok := m.tabAt(msg.X); ok {
			m.tab = t
			return m.onTabChange()
		}
		return nil
	}
	by := msg.Y - projectBodyTop
	if by < 0 || by >= m.bodyHeight() {
		return nil
	}
	if m.level == levelRun {
		return m.run.onMouse(msg, by, m.width-1, m.bodyHeight())
	}
	l := m.currentList()
	if d := wheelDelta(msg); d != 0 {
		l.wheel(d)
		return nil
	}
	if isClick(msg) && l.click(by) {
		return m.key(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

func (v *runView) onMouse(msg tea.MouseMsg, by, width, height int) tea.Cmd {
	treeW := min(48, max(24, width*35/100))
	inTree := msg.X < treeW
	if d := wheelDelta(msg); d != 0 {
		if inTree {
			v.tree.wheel(d)
			return v.selectLog()
		}
		v.scrollLog(d, height)
		return nil
	}
	if !isClick(msg) {
		return nil
	}
	v.logPane = !inTree
	if inTree {
		v.tree.click(by)
		return v.selectLog()
	}
	return nil
}
