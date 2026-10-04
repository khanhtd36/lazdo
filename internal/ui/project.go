package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

type projectTab int

const (
	projTabRepos projectTab = iota
	projTabPRs
	projTabPipelines
	projTabCount
)

func (t projectTab) title() string {
	switch t {
	case projTabPRs:
		return "Pull requests"
	case projTabRepos:
		return "Repos"
	case projTabPipelines:
		return "Pipelines"
	default:
		return "?"
	}
}

// projectLevel is how deep the user drilled: the tabs, a repo's browser, a
// pipeline's runs, or one run.
type projectLevel int

const (
	levelTabs projectLevel = iota
	levelRepo
	levelRuns
	levelRun
)

const runPollInterval = 3 * time.Second

type projectModel struct {
	client  *ado.Client
	project ado.ProjectInfo

	prs   []ado.PullRequest
	repos []ado.Repo
	tab   projectTab
	lists [projTabCount]pickList

	lastPush map[string]time.Time

	pipelines        []ado.Pipeline
	pipelinesLoading bool
	pipelinesLoaded  bool

	level projectLevel

	repo    ado.Repo
	browser *repoBrowser

	pipeline    ado.Pipeline
	runList     pickList
	runsLoading bool

	run *runView

	err           error
	status        string
	width, height int
}

type (
	repoPushesMsg struct {
		projectID string
		pushes    map[string]time.Time
	}
	pipelinesMsg struct {
		projectID string
		pipelines []ado.Pipeline
		err       error
	}
	branchesMsg struct {
		repoID   string
		branches []ado.Branch
		err      error
	}
	runsMsg struct {
		pipelineID int
		runs       []ado.Run
		err        error
	}
	// openPRMsg asks the root to open a pull request's detail view.
	openPRMsg     struct{ pr ado.PullRequest }
	projectPRsMsg struct {
		projectID string
		prs       []ado.PullRequest
		err       error
	}
)

func newProject(client *ado.Client, p ado.ProjectInfo, prs []ado.PullRequest, repos []ado.Repo) *projectModel {
	sort.Slice(prs, func(i, j int) bool { return prs[i].CreationDate.After(prs[j].CreationDate) })
	// The project's main repo (named after it, as Azure DevOps creates it)
	// comes first, then the rest A-Z.
	sort.Slice(repos, func(i, j int) bool {
		mi, mj := strings.EqualFold(repos[i].Name, p.Name), strings.EqualFold(repos[j].Name, p.Name)
		if mi != mj {
			return mi
		}
		return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
	})
	m := &projectModel{client: client, project: p, prs: prs, repos: repos, lastPush: map[string]time.Time{}}
	m.lists[projTabPRs].setItems(m.prItems())
	m.lists[projTabRepos].setItems(m.repoItems())
	return m
}

func (m *projectModel) init() tea.Cmd { return m.loadPushes() }

// bodyHeight leaves room for the title, tabs, rule, status and help lines.
func (m *projectModel) bodyHeight() int { return max(1, m.height-5) }

// --- Loading ---

func (m *projectModel) loadPushes() tea.Cmd {
	client, repos, projectID := m.client, m.repos, m.project.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, maxParallelBuilds)
		pushes := map[string]time.Time{}
		for _, r := range repos {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				if t, err := client.LastPush(ctx, r); err == nil {
					mu.Lock()
					pushes[r.ID] = t
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		return repoPushesMsg{projectID: projectID, pushes: pushes}
	}
}

// loadPRs refreshes the project's active pull requests.
func (m *projectModel) loadPRs() tea.Cmd {
	client, projectID := m.client, m.project.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		all, err := client.AllActivePRs(ctx)
		var prs []ado.PullRequest
		for _, pr := range all {
			if pr.Repository.Project.ID == projectID {
				prs = append(prs, pr)
			}
		}
		return projectPRsMsg{projectID: projectID, prs: prs, err: err}
	}
}

func (m *projectModel) loadPipelines() tea.Cmd {
	if m.pipelinesLoading {
		return nil
	}
	m.pipelinesLoading = true
	client, projectID := m.client, m.project.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ps, err := client.Pipelines(ctx, projectID)
		return pipelinesMsg{projectID: projectID, pipelines: ps, err: err}
	}
}

func (m *projectModel) openRepo(r ado.Repo) tea.Cmd {
	m.tab, m.level, m.repo = projTabRepos, levelRepo, r
	m.browser = newRepoBrowser(m.client, m.project, r)
	return m.browser.init()
}

func (m *projectModel) openPipeline(p ado.Pipeline) tea.Cmd {
	m.level, m.pipeline = levelRuns, p
	m.runList = pickList{}
	return m.loadRuns()
}

func (m *projectModel) loadRuns() tea.Cmd {
	m.runsLoading = true
	client, projectID, id := m.client, m.project.ID, m.pipeline.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		runs, err := client.Runs(ctx, projectID, id)
		return runsMsg{pipelineID: id, runs: runs, err: err}
	}
}

// --- Update ---

func (m *projectModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case projectPRsMsg:
		if msg.projectID == m.project.ID {
			m.err = msg.err
			if msg.err == nil {
				sort.Slice(msg.prs, func(i, j int) bool { return msg.prs[i].CreationDate.After(msg.prs[j].CreationDate) })
				m.prs = msg.prs
				m.lists[projTabPRs].setItems(m.prItems())
			}
		}
	case repoPushesMsg:
		if msg.projectID == m.project.ID {
			m.lastPush = msg.pushes
			m.lists[projTabRepos].setItems(m.repoItems())
		}
	case pipelinesMsg:
		if msg.projectID == m.project.ID {
			m.pipelinesLoading, m.pipelinesLoaded, m.err = false, true, msg.err
			m.pipelines = msg.pipelines
			m.lists[projTabPipelines].setItems(m.pipelineItems())
		}
	case branchesMsg, folderMsg, indexMsg, contentMsg:
		if m.browser != nil {
			return m.browser.update(msg)
		}
	case runsMsg:
		if msg.pipelineID == m.pipeline.ID {
			m.runsLoading, m.err = false, msg.err
			m.runList.setItems(m.runItems(msg.runs))
		}
	case runLoadedMsg, logMsg, runTickMsg:
		if m.run != nil {
			return m.run.update(msg)
		}
	case tea.KeyMsg:
		return m.key(msg)
	}
	return nil
}

// key handles a key; the returned bool is true when the project should close.
func (m *projectModel) key(msg tea.KeyMsg) tea.Cmd {
	m.status = ""
	switch m.level {
	case levelRepo:
		if handled, cmd := m.browser.key(msg, m.width-1, m.bodyHeight()); handled {
			return cmd
		}
		if msg.String() == "esc" {
			m.level, m.browser = levelTabs, nil
		}
		return nil
	case levelRuns:
		return m.runsKey(msg)
	case levelRun:
		if handled, cmd := m.run.key(msg, m.bodyHeight()); handled {
			return cmd
		}
		switch msg.String() {
		case "esc":
			m.level, m.run = levelRuns, nil
		case "o":
			return openURL(m.client.RunURL(m.project.Name, m.run.run.ID), "opened run "+m.run.run.BuildNumber)
		case "y":
			return copyRun(m.client, m.project.Name, m.run.run)
		case "r":
			return m.run.refresh()
		}
		return nil
	case levelTabs:
	}
	return m.tabsKey(msg)
}

// closeRequested reports whether esc at the top level should close the
// project; deeper levels and active filters take esc first.
func (m *projectModel) closeRequested(msg tea.KeyMsg) bool {
	if msg.String() != "esc" || m.level != levelTabs {
		return false
	}
	l := &m.lists[m.tab]
	return l.filter == "" && !l.typing
}

func (m *projectModel) tabsKey(msg tea.KeyMsg) tea.Cmd {
	l := &m.lists[m.tab]
	handled, activate := l.key(msg, m.bodyHeight())
	if activate {
		return m.activate()
	}
	if handled {
		return nil
	}
	it, ok := l.selected()
	switch msg.String() {
	case "1", "2", "3":
		m.tab = projectTab(msg.String()[0] - '1')
		return m.onTabChange()
	case "]":
		m.tab = (m.tab + 1) % projTabCount
		return m.onTabChange()
	case "[":
		m.tab = (m.tab + projTabCount - 1) % projTabCount
		return m.onTabChange()
	case "r":
		switch m.tab {
		case projTabRepos:
			return m.loadPushes()
		case projTabPipelines:
			return m.loadPipelines()
		case projTabPRs:
			return m.loadPRs()
		case projTabCount:
		}
	case "o":
		if !ok {
			return openURL(m.client.ProjectURL(m.project), "opened "+m.project.Name)
		}
		switch v := it.value.(type) {
		case ado.PullRequest:
			return openURL(v.WebURL(m.client.Org), fmt.Sprintf("opened !%d", v.ID))
		case ado.Repo:
			return openURL(v.WebURL, "opened "+v.Name)
		case ado.Pipeline:
			return openURL(m.client.PipelineURL(m.project.Name, v.ID), "opened "+v.Name)
		}
	case "y":
		if !ok {
			return copyProject(m.client, m.project)
		}
		switch v := it.value.(type) {
		case ado.Repo:
			return copyRepo(v)
		case ado.PullRequest:
			return copyPR(m.client.Org, v)
		case ado.Pipeline:
			return copyMenu("pipeline "+v.Name,
				copyItem{"Web URL", m.client.PipelineURL(m.project.Name, v.ID)},
				copyItem{"Name", v.Name})
		}
	}
	return nil
}

func (m *projectModel) onTabChange() tea.Cmd {
	if m.tab == projTabPipelines && !m.pipelinesLoaded {
		return m.loadPipelines()
	}
	return nil
}

func (m *projectModel) activate() tea.Cmd {
	it, ok := m.lists[m.tab].selected()
	if !ok {
		return nil
	}
	switch v := it.value.(type) {
	case ado.PullRequest:
		return func() tea.Msg { return openPRMsg{pr: v} }
	case ado.Repo:
		return m.openRepo(v)
	case ado.Pipeline:
		return m.openPipeline(v)
	}
	return nil
}

func (m *projectModel) runsKey(msg tea.KeyMsg) tea.Cmd {
	handled, activate := m.runList.key(msg, m.bodyHeight())
	it, ok := m.runList.selected()
	if activate && ok {
		m.run = newRunView(m.client, m.project, it.value.(ado.Run))
		m.level = levelRun
		return m.run.refresh()
	}
	if handled {
		return nil
	}
	switch msg.String() {
	case "esc":
		m.level = levelTabs
	case "y":
		if ok {
			return copyRun(m.client, m.project.Name, it.value.(ado.Run))
		}
	case "r":
		return m.loadRuns()
	case "o":
		if ok {
			r := it.value.(ado.Run)
			return openURL(m.client.RunURL(m.project.Name, r.ID), "opened run "+r.BuildNumber)
		}
		return openURL(m.client.PipelineURL(m.project.Name, m.pipeline.ID), "opened "+m.pipeline.Name)
	}
	return nil
}

// --- Rows ---

func (m *projectModel) prItems() []pickItem {
	items := make([]pickItem, 0, len(m.prs))
	for _, pr := range m.prs {
		items = append(items, pickItem{
			search: fmt.Sprintf("%s %s %s %d", pr.Title, pr.CreatedBy.DisplayName, pr.SourceBranch(), pr.ID),
			value:  pr,
			render: func(width int) string { return projectPRRow(pr, width) },
		})
	}
	return items
}

func projectPRRow(pr ado.PullRequest, width int) string {
	draft := ""
	if pr.IsDraft {
		draft = styleDraft.Render("[d]")
	}
	right := joinCols(
		styleDim.Render(fit(nameInitials(pr.CreatedBy.DisplayName), colAuthorCompact)),
		fit(fmt.Sprintf("!%d", pr.ID), colID),
		styleDim.Render(fit(pr.Repository.Name+" → "+pr.TargetBranch(), 28)),
		fitStyled(votes(pr.Reviewers), 14),
		styleDim.Render(fit(relTime(time.Since(pr.CreationDate), pr.CreationDate), colUpdated)),
	)
	left := pr.Title
	if draft != "" {
		left += " " + draft
	}
	titleWidth := max(20, width-ansi.StringWidth(right)-2)
	return fitStyled(left, titleWidth) + "  " + right
}

func (m *projectModel) repoItems() []pickItem {
	items := make([]pickItem, 0, len(m.repos))
	for _, r := range m.repos {
		items = append(items, pickItem{
			search: r.Name,
			value:  r,
			render: func(width int) string { return m.repoRow(r, width) },
		})
	}
	return items
}

func (m *projectModel) repoRow(r ado.Repo, width int) string {
	branch := r.DefaultBranchName()
	if branch == "" {
		branch = "empty"
	}
	pushed := ""
	if t, ok := m.lastPush[r.ID]; ok && !t.IsZero() {
		pushed = "pushed " + relTime(time.Since(t), t)
	}
	return truncate(joinCols(
		fit(styleTitle.Render(r.Name), 30),
		styleCyan.Render(fit(branch, 16)),
		styleDim.Render(fit(pushed, 16)),
		styleDim.Render(humanSize(r.Size)),
	), width)
}

func humanSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

func (m *projectModel) pipelineItems() []pickItem {
	ps := append([]ado.Pipeline(nil), m.pipelines...)
	sort.SliceStable(ps, func(i, j int) bool { return pipelineActivity(ps[i]).After(pipelineActivity(ps[j])) })
	items := make([]pickItem, 0, len(ps))
	for _, p := range ps {
		items = append(items, pickItem{
			search: p.Name + " " + p.Path,
			value:  p,
			render: func(width int) string { return pipelineRow(p, width) },
		})
	}
	return items
}

// pipelineActivity sorts pipelines by their latest run, newest first.
func pipelineActivity(p ado.Pipeline) time.Time {
	if p.LatestBuild == nil {
		return time.Time{}
	}
	return p.LatestBuild.QueueTime
}

func pipelineRow(p ado.Pipeline, width int) string {
	last := styleDim.Render("never run")
	glyph := styleDim.Render("·")
	if r := p.LatestBuild; r != nil {
		glyph = runGlyph(r.Status, r.Result)
		last = styleDim.Render(r.Branch() + " · " + relTime(time.Since(r.QueueTime), r.QueueTime))
	}
	folder := strings.Trim(p.Path, `\`)
	if folder != "" {
		folder = styleDim.Render(folder + `\`)
	}
	return truncate(joinCols(glyph+" "+folder+styleTitle.Render(p.Name), last), width)
}

func (m *projectModel) runItems(runs []ado.Run) []pickItem {
	items := make([]pickItem, 0, len(runs))
	for _, r := range runs {
		items = append(items, pickItem{
			search: fmt.Sprintf("%s %s %s %s", r.BuildNumber, r.Branch(), r.RequestedFor.DisplayName, r.Reason),
			value:  r,
			render: func(width int) string { return runRow(r, width) },
		})
	}
	return items
}

func runRow(r ado.Run, width int) string {
	return truncate(joinCols(
		runGlyph(r.Status, r.Result)+" "+fit(styleTitle.Render(r.BuildNumber), 14),
		styleCyan.Render(fit(r.Branch(), 34)),
		styleDim.Render(fit(r.Reason, 14)),
		styleDim.Render(fit(nameInitials(r.RequestedFor.DisplayName), 4)),
		styleDim.Render(fit(relTime(time.Since(r.QueueTime), r.QueueTime), 9)),
		styleDim.Render(duration(r.StartTime, r.FinishTime)),
	), width)
}

func duration(start, finish time.Time) string {
	if start.IsZero() {
		return ""
	}
	if finish.IsZero() {
		finish = time.Now()
	}
	return finish.Sub(start).Round(time.Second).String()
}

// runGlyph marks a run, stage, job or step by its state and result.
func runGlyph(state, result string) string {
	switch {
	case state == "inProgress" || state == "cancelling":
		return styleYellow.Render("●")
	case state == "notStarted" || state == "pending" || state == "postponed":
		return styleDim.Render("○")
	case result == "succeeded":
		return styleGreen.Render("✓")
	case result == "partiallySucceeded" || result == "succeededWithIssues":
		return styleYellow.Render("!")
	case result == "failed":
		return styleRed.Render("✗")
	case result == "canceled" || result == "skipped":
		return styleDim.Render("⊘")
	}
	return styleDim.Render("·")
}
