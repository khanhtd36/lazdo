// Package ui is the bubbletea dashboard.
package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/actions"
	"github.com/khanhtd36/lazdo/internal/ado"
	"github.com/khanhtd36/lazdo/internal/update"
)

// maxParallelBuilds caps concurrent per-PR build policy requests.
const maxParallelBuilds = 8

type buildResult struct {
	state ado.BuildState
	err   error
}

// row is one visible line: a section header or a PR inside a section.
type row struct {
	section int
	pr      *ado.PullRequest // nil for a header row
}

type Model struct {
	client   *ado.Client
	interval time.Duration

	me        ado.Identity
	sections  []ado.Section
	stats     map[int]ado.Stats // nil until the stats batch returns
	statsErr  error
	builds    map[int]buildResult
	collapsed map[ado.SectionKind]bool
	loading   bool
	err       error
	status    string
	fetchedAt time.Time

	cursor, offset int
	dashboardPane  int
	paneCursors    [4]int
	paneOffsets    [4]int
	width, height  int
	sem            chan struct{}

	page     page
	projects projectsPage
	project  *projectModel // non-nil while a project is open

	detail *detailModel // non-nil while a pull request's detail is open
	help   *helpModal   // non-nil while the shortcut help is open
	modal  modal        // a dialog over any screen, such as checkout

	lastCheckout map[string]string // repo key → path, this session only
	updates      []update.Release  // releases newer than this build, newest first

	filter       string // dashboard / filter
	filterTyping bool
}

func New(client *ado.Client, interval time.Duration) Model {
	return Model{
		client:    client,
		interval:  interval,
		builds:    map[int]buildResult{},
		collapsed: map[ado.SectionKind]bool{},
		loading:   true,
		sem:       make(chan struct{}, maxParallelBuilds),

		lastCheckout: map[string]string{},
	}
}

type (
	listMsg struct {
		me       ado.Identity
		sections []ado.Section
		err      error
	}
	statsMsg struct {
		stats map[int]ado.Stats
		err   error
	}
	buildMsg struct {
		id     int
		result buildResult
	}
	tickMsg   struct{}
	statusMsg string
)

func (m Model) Init() tea.Cmd { return tea.Batch(m.fetchList(), checkUpdates()) }

func (m Model) fetchList() tea.Cmd {
	client, me := m.client, m.me
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if me.ID == "" {
			var err error
			if me, err = client.Me(ctx); err != nil {
				return listMsg{err: err}
			}
		}
		reviewing, err := client.ActivePRs(ctx, "reviewerId", me.ID)
		if err != nil {
			return listMsg{err: err}
		}
		created, err := client.ActivePRs(ctx, "creatorId", me.ID)
		if err != nil {
			return listMsg{err: err}
		}
		return listMsg{me: me, sections: ado.Classify(me.ID, reviewing, created)}
	}
}

func (m Model) fetchStats(prs []ado.PullRequest) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		stats, err := client.Stats(ctx, prs)
		return statsMsg{stats: stats, err: err}
	}
}

func (m Model) fetchBuild(pr ado.PullRequest) tea.Cmd {
	client, sem := m.client, m.sem
	return func() tea.Msg {
		sem <- struct{}{}
		defer func() { <-sem }()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		state, err := client.Build(ctx, pr)
		return buildMsg{id: pr.ID, result: buildResult{state: state, err: err}}
	}
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()
		m.clampDashboard()
		if m.detail != nil {
			m.detail.resize(msg.Width, msg.Height)
		}
		if m.project != nil {
			m.project.width, m.project.height = msg.Width, msg.Height
		}
	case projectsLoadedMsg:
		m.projects.onLoaded(msg)
	case repoPushesMsg, pipelinesMsg, branchesMsg, runsMsg, runLoadedMsg, logMsg, runTickMsg,
		folderMsg, indexMsg, contentMsg, projectPRsMsg,
		commitsMsg, releaseMsg, tagsMsg, tagInfoMsg, refsChangedMsg:
		if m.project != nil {
			return m, m.project.update(msg)
		}
	case openPRMsg:
		return m.openDetail(&msg.pr)
	case openRepoDiffMsg:
		d, cmd := newRepoDiff(m.client, msg.repo, msg.label, msg.from, msg.commit, m.width, m.height)
		m.detail = d
		return m, cmd
	case showModalMsg:
		m.modal = msg.modal
	case copyMenuMsg:
		if len(msg.items) > 0 {
			m.modal = newCopyMenu(msg)
		}
	case checkoutRequestMsg:
		key := actions.RepoKey(msg.org, msg.project, msg.repo)
		m.modal = newCheckoutModal(msg, checkoutSuggestions(key, msg.repo, m.lastCheckout), m.width)
	case checkoutDoneMsg:
		m.modal = nil
		if msg.err == nil {
			m.lastCheckout[msg.repoKey] = msg.path
		}
		return m.Update(resultMsg(msg.err, msg.text))
	case detailLoadedMsg, visitMsg, editorDoneMsg, filesLoadedMsg, fileDiffMsg:
		if m.detail != nil {
			return m, m.detail.update(msg)
		}
	case actionDoneMsg:
		if m.detail == nil {
			return m, nil
		}
		if msg.leave && msg.err == nil {
			m.detail = nil
			m.status = msg.text
			return m, m.refresh()
		}
		return m, m.detail.update(msg)
	case updatesMsg:
		m.updates = msg.releases
	case updateDoneMsg:
		if msg.err == nil {
			m.updates = nil
		}
		return m.Update(resultMsg(msg.err, msg.text))
	case listMsg:
		return m.onList(msg)
	case statsMsg:
		m.statsErr = msg.err
		if msg.err == nil {
			m.stats = msg.stats
		}
	case buildMsg:
		m.builds[msg.id] = msg.result
	case tickMsg:
		var detailCmd tea.Cmd
		if m.detail != nil {
			detailCmd = m.detail.reload()
		}
		if m.loading {
			return m, tea.Batch(m.tick(), detailCmd)
		}
		m.loading = true
		return m, tea.Batch(m.fetchList(), detailCmd)
	case statusMsg:
		switch {
		case m.detail != nil:
			m.detail.status = string(msg)
		case m.project != nil:
			m.project.status = string(msg)
		default:
			m.status = string(msg)
		}
	case tea.KeyMsg:
		if msg.String() == "q" && !m.inTextInput() {
			return m, tea.Quit
		}
		if m.help != nil {
			return m.onHelpKey(msg)
		}
		if m.modal != nil {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			var cmd tea.Cmd
			m.modal, cmd = m.modal.update(msg)
			return m, cmd
		}
		if msg.String() == "?" && !m.typing() {
			m.help = newHelp(m.helpGroups(), m.width, m.height-2)
			return m, nil
		}
		if m.detail != nil {
			return m.onDetailKey(msg)
		}
		if m.project != nil {
			return m.onProjectKey(msg)
		}
		if !m.typing() {
			if p, ok := m.pageKey(msg.String()); ok {
				return m.switchPage(p)
			}
			if msg.String() == "U" {
				return m.openUpdate()
			}
		}
		if m.page == pageProjects {
			return m.onProjectsKey(msg)
		}
		return m.onKey(msg)
	case tea.MouseMsg:
		if m.help != nil {
			if d := wheelDelta(msg); d != 0 {
				m.help.move(d)
			}
			return m, nil
		}
		return m.onMouse(msg)
	}
	return m, nil
}

// inTextInput reports whether keys are being typed as text (a / filter, a
// find box, an editor, a path), where q is a character, not "quit".
func (m Model) inTextInput() bool {
	switch {
	case m.help != nil:
		return m.help.typing
	case m.modal != nil:
		switch m.modal.(type) {
		case *checkoutModal, *tagDialog:
			return true
		}
		return false
	case m.detail != nil && m.detail.modal != nil:
		switch m.detail.modal.(type) {
		case *editorModal, *completeDialog, *prEditor:
			return true
		}
		return false
	}
	return m.typing()
}

// typing reports whether keys are going into a text field or a / filter,
// where ? and page keys are just characters.
func (m Model) typing() bool {
	switch {
	case m.detail != nil:
		return m.detail.modal != nil || m.detail.typing()
	case m.project != nil:
		return m.project.typing()
	case m.page == pageProjects:
		return m.projects.list.typing
	}
	return m.filterTyping
}

func (m Model) pageKey(key string) (page, bool) {
	switch key {
	case "1":
		return pagePRs, true
	case "2":
		return pageProjects, true
	case "]":
		return (m.page + 1) % pageCount, true
	case "[":
		return (m.page + pageCount - 1) % pageCount, true
	}
	return 0, false
}

func (m Model) switchPage(p page) (tea.Model, tea.Cmd) {
	m.page = p
	if p == pageProjects && !m.projects.loaded {
		return m, m.projects.load(m.client)
	}
	return m, nil
}

func (m Model) onProjectsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if !m.projects.list.typing {
			return m, tea.Quit
		}
	}
	m.status = ""
	cmd, open := m.projects.key(msg, m.listHeight(), m.client)
	if open != nil {
		open.width, open.height = m.width, m.height
		m.project = open
	}
	return m, cmd
}

func (m Model) onProjectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.project.closeRequested(msg) {
		m.project = nil
		return m, nil
	}
	return m, m.project.update(msg)
}

func (m Model) onHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	closed, run := m.help.update(msg)
	if closed {
		m.help = nil
	}
	if run != nil {
		return m.Update(*run)
	}
	return m, nil
}

func (m Model) onDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.detail.closeRequested(msg) {
		m.detail = nil
		// The visit cleared this PR's "new since last visit" counts.
		return m, m.refresh()
	}
	return m, m.detail.update(msg)
}

func (m *Model) refresh() tea.Cmd {
	if m.loading {
		return nil
	}
	m.loading = true
	return m.fetchList()
}

func (m Model) openDetail(pr *ado.PullRequest) (tea.Model, tea.Cmd) {
	m.detail = newDetail(m.client, m.me, *pr, m.width, m.height)
	return m, m.detail.init()
}

func (m Model) onList(msg listMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	cmds := []tea.Cmd{m.tick()}
	if msg.err != nil {
		m.err = msg.err
		return m, tea.Batch(cmds...)
	}
	m.err = nil
	m.me = msg.me
	m.sections = msg.sections
	m.fetchedAt = time.Now()
	var all []ado.PullRequest
	for _, s := range m.sections {
		all = append(all, s.PRs...)
	}
	if len(all) > 0 {
		cmds = append(cmds, m.fetchStats(all))
	}
	for _, pr := range all {
		cmds = append(cmds, m.fetchBuild(pr))
	}
	m.clampCursor()
	m.clampDashboard()
	return m, tea.Batch(cmds...)
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	if m.filterTyping {
		return m.onFilterKey(msg)
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "tab":
		if len(m.sections) > 0 {
			m.dashboardPane = (m.dashboardPane + 1) % len(m.sections)
		}
	case "shift+tab":
		if len(m.sections) > 0 {
			m.dashboardPane = (m.dashboardPane + len(m.sections) - 1) % len(m.sections)
		}
	case "j", "down":
		m.paneCursors[m.dashboardPane]++
	case "k", "up":
		m.paneCursors[m.dashboardPane]--
	case "ctrl+d", "pgdown":
		m.paneCursors[m.dashboardPane] += max(1, m.paneHeight()/2)
	case "ctrl+u", "pgup":
		m.paneCursors[m.dashboardPane] -= max(1, m.paneHeight()/2)
	case "g", "home":
		m.paneCursors[m.dashboardPane] = 0
	case "G", "end":
		m.paneCursors[m.dashboardPane] = len(m.dashboardItems(m.dashboardPane)) - 1
	case "/":
		m.filterTyping = true
	case "esc":
		m.filter = ""
	case "r":
		if !m.loading {
			m.loading = true
			return m, m.fetchList()
		}
	case "enter", " ":
		if pr, ok := m.selectedDashboardPR(); ok {
			return m.openDetail(pr)
		}
	case "o":
		if pr, ok := m.selectedDashboardPR(); ok {
			return m, m.openSelected(pr)
		}
	case "y":
		if pr, ok := m.selectedDashboardPR(); ok {
			return m, copyPR(m.client.Org, *pr)
		}
	case "c":
		if pr, ok := m.selectedDashboardPR(); ok {
			return m, prCheckout(m.client.Org, *pr)
		}
	}
	m.clampDashboard()
	return m, nil
}

// onFilterKey edits the dashboard's / filter; enter opens the selected
// match, like every other / filter.
func (m Model) onFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filter, m.filterTyping = "", false
	case "enter":
		m.filterTyping = false
		if pr, ok := m.selectedDashboardPR(); ok {
			return m.openDetail(pr)
		}
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	case "up", "ctrl+k", "ctrl+p":
		m.paneCursors[m.dashboardPane]--
	case "down", "ctrl+j", "ctrl+n":
		m.paneCursors[m.dashboardPane]++
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			s := string(msg.Runes)
			if s == "" {
				s = " "
			}
			m.filter += s
			m.paneCursors[m.dashboardPane] = 0
		}
	}
	m.clampDashboard()
	return m, nil
}

func (m Model) dashboardItems(section int) []*ado.PullRequest {
	if section < 0 || section >= len(m.sections) {
		return nil
	}
	matched := m.filterMatches()
	items := make([]*ado.PullRequest, 0, len(m.sections[section].PRs))
	for i := range m.sections[section].PRs {
		pr := &m.sections[section].PRs[i]
		if matched == nil || matched[pr.ID] {
			items = append(items, pr)
		}
	}
	return items
}

func (m Model) selectedDashboardPR() (*ado.PullRequest, bool) {
	items := m.dashboardItems(m.dashboardPane)
	i := m.paneCursors[m.dashboardPane]
	if i < 0 || i >= len(items) {
		return nil, false
	}
	return items[i], true
}

func (m Model) paneHeight() int { return max(1, (m.height-3)/2-2) }

func (m *Model) clampDashboard() {
	if len(m.sections) == 0 {
		return
	}
	m.dashboardPane = max(0, min(m.dashboardPane, len(m.sections)-1))
	for i := range m.sections {
		items := m.dashboardItems(i)
		m.paneCursors[i] = max(0, min(m.paneCursors[i], len(items)-1))
		visible := m.paneHeight()
		if m.paneCursors[i] < m.paneOffsets[i] {
			m.paneOffsets[i] = m.paneCursors[i]
		}
		if m.paneCursors[i] >= m.paneOffsets[i]+visible {
			m.paneOffsets[i] = m.paneCursors[i] - visible + 1
		}
		m.paneOffsets[i] = max(0, min(m.paneOffsets[i], max(0, len(items)-visible)))
	}
	m.cursor = m.paneCursors[m.dashboardPane]
}

func (m Model) openSelected(pr *ado.PullRequest) tea.Cmd {
	u := pr.WebURL(m.client.Org)
	return func() tea.Msg { return resultMsg(actions.OpenBrowser(u), "opened !"+fmt.Sprint(pr.ID)) }
}

func resultMsg(err error, ok string) statusMsg {
	if err != nil {
		return statusMsg("error: " + err.Error())
	}
	return statusMsg(ok)
}

func (m Model) rows() []row {
	var rows []row
	matched := m.filterMatches()
	for i, s := range m.sections {
		rows = append(rows, row{section: i})
		if m.collapsed[s.Kind] && matched == nil {
			continue
		}
		for j := range s.PRs {
			if matched == nil || matched[s.PRs[j].ID] {
				rows = append(rows, row{section: i, pr: &m.sections[i].PRs[j]})
			}
		}
	}
	return rows
}

// filterMatches returns the IDs of PRs matching the / filter; nil when no
// filter is set (everything shows).
func (m Model) filterMatches() map[int]bool {
	if m.filter == "" {
		return nil
	}
	var texts []string
	var ids []int
	for _, s := range m.sections {
		for _, pr := range s.PRs {
			texts = append(texts, fmt.Sprintf("%s %s !%d %s", pr.Title, pr.CreatedBy.DisplayName, pr.ID, pr.SourceBranch()))
			ids = append(ids, pr.ID)
		}
	}
	matched := map[int]bool{}
	for _, mt := range rankFuzzy(m.filter, texts) {
		matched[ids[mt.Index]] = true
	}
	return matched
}

func (m Model) selected(rows []row) (row, bool) {
	if m.cursor < 0 || m.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.cursor], true
}

func (m Model) nextHeader(rows []row, dir int) int {
	for i := m.cursor + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].pr == nil {
			return i
		}
	}
	return m.cursor
}

func (m *Model) clampCursor() {
	n := len(m.rows())
	m.cursor = max(0, min(m.cursor, n-1))
	visible := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if visible > 0 && m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	m.offset = max(0, min(m.offset, n-1))
}

// listHeight is the number of rows left after the title and footer lines.
func (m Model) listHeight() int { return m.height - 3 }
