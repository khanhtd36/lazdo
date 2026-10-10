package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// bodyTop is the first body row: below the title, tabs and rule, or right
// below the tabs over framed panes.
func (m *projectModel) bodyTop() int {
	if m.panes() {
		return 2
	}
	return 3
}

func (m *projectModel) view() string {
	var b strings.Builder
	b.WriteString(truncate(m.titleLine(), m.width-1) + "\n")
	b.WriteString(truncate(m.tabsLine(), m.width-1) + "\n")
	if !m.panes() { // framed panes separate themselves from the tabs
		b.WriteString(styleDim.Render(strings.Repeat("─", max(0, m.width-1))) + "\n")
	}
	for _, line := range m.body() {
		b.WriteString(truncate(line, m.width-1) + "\n")
	}
	status := m.status
	if m.level == levelRun {
		if s, ok := m.run.find.status(); ok {
			status = s
		}
	}
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
	case levelRepo:
		crumbs = append(crumbs, "Repos", styleTitle.Render(m.repo.Name), styleAccent.Render("⑂ "+m.browser.branch))
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
	case levelRepo:
		return m.browser.indexLoading[m.browser.branch]
	case levelRuns:
		return m.runsLoading
	case levelRun:
		return m.run.loading
	case levelTabs:
	}
	return m.tab == projTabPipelines && m.pipelinesLoading
}

func (m *projectModel) tabLabels() []string {
	counts := [projTabCount]int{projTabRepos: len(m.repos), projTabPRs: len(m.prs), projTabPipelines: len(m.pipelines)}
	labels := make([]string, 0, projTabCount)
	for t := range projTabCount {
		label := fmt.Sprintf("%d %s", t+1, t.title())
		if t != projTabSettings && (t != projTabPipelines || m.pipelinesLoaded) {
			label += fmt.Sprintf(" (%d)", counts[t])
		}
		labels = append(labels, label)
	}
	return labels
}

func (m *projectModel) tabsLine() string {
	if m.level == levelRepo {
		return m.browser.tabsLine(m.width - 1)
	}
	return tabBar(m.tabLabels(), int(m.tab), m.width-1)
}

func (m *projectModel) tabAt(x int) (projectTab, bool) {
	t, ok := tabAt(m.tabLabels(), x, m.width-1)
	return projectTab(t), ok
}

func (m *projectModel) body() []string {
	h, w := m.bodyHeight(), m.width-1
	switch m.level {
	case levelRepo:
		if m.browser.branch == "" {
			return padLines([]string{styleDim.Render("  empty repo")}, h)
		}
		return m.browser.view(w, m.browserHeight())
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
	if m.tab == projTabSettings {
		return m.settings.view(w, h)
	}
	return m.lists[m.tab].view(w, h)
}

func padLines(lines []string, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

func (m *projectModel) help() string { return footer(m.helpGroups()) }

// currentList is the list the body shows, nil on the repo and run views.
func (m *projectModel) currentList() *pickList {
	switch m.level {
	case levelRuns:
		return &m.runList
	case levelRepo, levelRun:
		return nil
	case levelTabs:
	}
	return &m.lists[m.tab]
}

// typing reports whether keys are going into a filter or search box.
func (m *projectModel) typing() bool {
	switch m.level {
	case levelRepo:
		return m.browser.typing()
	case levelRun:
		return m.run.tree.typing || m.run.find.typing
	case levelTabs:
		if m.tab == projTabSettings {
			return m.settings.typing()
		}
	case levelRuns:
	}
	return m.currentList().typing
}

func (m *projectModel) onMouse(msg tea.MouseMsg) tea.Cmd {
	if isClick(msg) && msg.Y == 1 && m.level == levelRepo {
		if t, ok := m.browser.tabAt(msg.X, m.width-1); ok {
			m.browser.tab, m.browser.pane = t, paneFiles
			return m.browser.ensureHistory()
		}
		return nil
	}
	if isClick(msg) && msg.Y == 1 && m.level != levelRepo {
		if t, ok := m.tabAt(msg.X); ok {
			m.tab, m.level, m.run = t, levelTabs, nil
			return m.onTabChange()
		}
		return nil
	}
	by := msg.Y - m.bodyTop()
	if by < 0 || by >= m.bodyHeight() {
		return nil
	}
	switch m.level {
	case levelRun:
		return m.run.onMouse(msg, by, m.width-1, m.bodyHeight())
	case levelRepo:
		return m.browser.onMouse(msg, by, m.width-1, m.browserHeight())
	case levelTabs, levelRuns:
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
	inTree := msg.X < runTreeWidth(width)
	height = paneInnerHeight(height)
	by-- // below the panes' top border
	if d := wheelDelta(msg); d != 0 {
		if inTree {
			v.tree.wheel(d)
			return v.selectLog()
		}
		v.scrollLog(d, height)
		return nil
	}
	if by < 0 || by >= height {
		return nil // on a frame
	}
	// Press and drag over log lines selects them; y copies.
	if !inTree && (isClick(msg) || isDrag(msg)) {
		line := min(v.logTop+by, len(v.lines)-1)
		if line < 0 {
			return nil
		}
		if isClick(msg) {
			v.logPane, v.cur, v.anchor, v.drag, v.follow = true, line, -1, line, false
		} else if line != v.drag {
			v.anchor, v.cur = v.drag, line
		}
		return nil
	}
	if !isClick(msg) {
		return nil
	}
	v.logPane = false
	v.tree.click(by)
	return v.selectLog()
}
