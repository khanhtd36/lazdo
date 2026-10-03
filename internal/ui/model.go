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

// maxParallelDetails caps concurrent per-PR detail requests.
const maxParallelDetails = 8

type detailState struct {
	loaded bool
	d      ado.Details
	err    error
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
	details   map[int]detailState
	collapsed map[ado.SectionKind]bool
	loading   bool
	err       error
	status    string
	fetchedAt time.Time

	cursor, offset int
	width, height  int
	sem            chan struct{}
}

func New(client *ado.Client, interval time.Duration) Model {
	key, _ := actions.CurrentRepoKey()
	return Model{
		client:    client,
		interval:  interval,
		repoKey:   key,
		details:   map[int]detailState{},
		collapsed: map[ado.SectionKind]bool{},
		loading:   true,
		sem:       make(chan struct{}, maxParallelDetails),
	}
}

type (
	listMsg struct {
		me       ado.Identity
		sections []ado.Section
		err      error
	}
	detailMsg struct {
		id  int
		d   ado.Details
		err error
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

func (m Model) fetchDetails(pr ado.PullRequest) tea.Cmd {
	client, meID, sem := m.client, m.me.ID, m.sem
	return func() tea.Msg {
		sem <- struct{}{}
		defer func() { <-sem }()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		d, err := client.Details(ctx, pr, meID)
		return detailMsg{id: pr.ID, d: d, err: err}
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
	case listMsg:
		return m.onList(msg)
	case detailMsg:
		m.details[msg.id] = detailState{loaded: true, d: msg.d, err: msg.err}
	case tickMsg:
		if m.loading {
			return m, m.tick()
		}
		m.loading = true
		return m, m.fetchList()
	case statusMsg:
		m.status = string(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
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
	for _, s := range m.sections {
		for _, pr := range s.PRs {
			cmds = append(cmds, m.fetchDetails(pr))
		}
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
			return m, m.openSelected(r.pr)
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
