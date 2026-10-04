// Package ui is the bubbletea dashboard.
package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/actions"
	"github.com/khanhtd36/lazdo/internal/ado"
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
	repoKey  string // RepoKey of the cwd git repo, "" when not in an ADO clone

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
	width, height  int
	sem            chan struct{}

	detail *detailModel // non-nil while a pull request's detail is open
}

func New(client *ado.Client, interval time.Duration) Model {
	key, _ := actions.CurrentRepoKey()
	return Model{
		client:    client,
		interval:  interval,
		repoKey:   key,
		builds:    map[int]buildResult{},
		collapsed: map[ado.SectionKind]bool{},
		loading:   true,
		sem:       make(chan struct{}, maxParallelBuilds),
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

func (m Model) Init() tea.Cmd { return m.fetchList() }

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
		if m.detail != nil {
			m.detail.resize(msg.Width, msg.Height)
		}
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
		if m.detail != nil {
			m.detail.status = string(msg)
		} else {
			m.status = string(msg)
		}
	case tea.KeyMsg:
		if m.detail != nil {
			return m.onDetailKey(msg)
		}
		return m.onKey(msg)
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
	m.detail = newDetail(m.client, m.me, *pr, m.repoKey, m.width, m.height)
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
	return m, tea.Batch(cmds...)
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	rows := m.rows()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor++
	case "k", "up":
		m.cursor--
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(rows) - 1
	case "tab":
		m.cursor = m.nextHeader(rows, 1)
	case "shift+tab":
		m.cursor = m.nextHeader(rows, -1)
	case "r":
		if !m.loading {
			m.loading = true
			return m, m.fetchList()
		}
	case "enter", " ":
		if r, ok := m.selected(rows); ok && r.pr == nil {
			kind := m.sections[r.section].Kind
			m.collapsed[kind] = !m.collapsed[kind]
		} else if ok {
			return m.openDetail(r.pr)
		}
	case "o":
		if r, ok := m.selected(rows); ok && r.pr != nil {
			return m, m.openSelected(r.pr)
		}
	case "y":
		if r, ok := m.selected(rows); ok && r.pr != nil {
			u := r.pr.WebURL(m.client.Org)
			return m, func() tea.Msg { return resultMsg(actions.CopyToClipboard(u), "copied "+u) }
		}
	case "c":
		if r, ok := m.selected(rows); ok && r.pr != nil {
			return m, m.checkout(r.pr)
		}
	}
	m.clampCursor()
	return m, nil
}

func (m Model) openSelected(pr *ado.PullRequest) tea.Cmd {
	u := pr.WebURL(m.client.Org)
	return func() tea.Msg { return resultMsg(actions.OpenBrowser(u), "opened !"+fmt.Sprint(pr.ID)) }
}

func (m Model) checkout(pr *ado.PullRequest) tea.Cmd {
	prKey := actions.RepoKey(m.client.Org, pr.Repository.Project.Name, pr.Repository.Name)
	if m.repoKey != prKey {
		return func() tea.Msg {
			return statusMsg("checkout: run lazdo inside a clone of " + pr.Repository.Name)
		}
	}
	branch := pr.SourceBranch()
	return func() tea.Msg { return resultMsg(actions.Checkout(branch), "switched to "+branch) }
}

func resultMsg(err error, ok string) statusMsg {
	if err != nil {
		return statusMsg("error: " + err.Error())
	}
	return statusMsg(ok)
}

func (m Model) rows() []row {
	var rows []row
	for i, s := range m.sections {
		rows = append(rows, row{section: i})
		if m.collapsed[s.Kind] {
			continue
		}
		for j := range s.PRs {
			rows = append(rows, row{section: i, pr: &m.sections[i].PRs[j]})
		}
	}
	return rows
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
