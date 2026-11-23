package ui

import (
	"cmp"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Injectable write/list functions behind CI/CD commands; tests replace them.
var (
	requeuePolicy = func(ctx context.Context, c *ado.Client, pr ado.PullRequest, evaluationID string) error {
		return c.RequeuePolicy(ctx, pr, evaluationID)
	}
	cancelRun = func(ctx context.Context, c *ado.Client, projectID string, runID int) error {
		return c.CancelRun(ctx, projectID, runID)
	}
	retryRun = func(ctx context.Context, c *ado.Client, projectID string, runID int) error {
		return c.RetryRun(ctx, projectID, runID)
	}
	queueRun = func(ctx context.Context, c *ado.Client, projectID string, definitionID int, sourceBranch string) (ado.Run, error) {
		return c.QueueRun(ctx, projectID, definitionID, sourceBranch)
	}
	listPipelines = func(ctx context.Context, c *ado.Client, projectID string) ([]ado.Pipeline, error) {
		return c.Pipelines(ctx, projectID)
	}
)

type ciActionKind int

const (
	ciActRequeue ciActionKind = iota
	ciActRetry
	ciActQueue
	ciActCancel
)

type (
	ciActionMsg struct {
		from         *ado.Client
		prID         int
		kind         ciActionKind
		evaluationID string
		name         string
		run          ado.Run
		err          error
	}
	prPipelinesMsg struct {
		from      *ado.Client
		prID      int
		projectID string
		pipelines []ado.Pipeline
		err       error
	}
	openDashRunMsg struct {
		from  *ado.Client
		pr    ado.PullRequest
		check ado.CICheck
	}
)

func (m ciActionMsg) source() *ado.Client    { return m.from }
func (m ciActionMsg) pullRequest() int       { return m.prID }
func (m prPipelinesMsg) source() *ado.Client { return m.from }
func (m prPipelinesMsg) pullRequest() int    { return m.prID }
func (m openDashRunMsg) source() *ado.Client { return m.from }

func ciActionVerb(kind ciActionKind) string {
	switch kind {
	case ciActRequeue:
		return "re-queue"
	case ciActRetry:
		return "retry"
	case ciActQueue:
		return "trigger"
	case ciActCancel:
		return "cancel"
	}
	return "run"
}

func waitPolicyTick(prID int, evalID string) tea.Cmd {
	return tea.Tick(runWaitPoll, func(time.Time) tea.Msg { return runWaitTickMsg{prID: prID, evaluationID: evalID} })
}

func waitPolicyPoll(client *ado.Client, pr ado.PullRequest, evalID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		p, err := client.PolicyEvaluation(ctx, pr, evalID)
		return runWaitMsg{prID: pr.ID, evaluationID: evalID, policy: p, err: err}
	}
}

func runNumLabel(r ado.Run) string {
	return cmp.Or(r.BuildNumber, "#"+strconv.Itoa(r.ID))
}

func isCancellableRun(r ado.Run) bool {
	if r.ID == 0 {
		return false
	}
	return r.Status == "inProgress" || r.Status == "notStarted" || r.Status == "postponed"
}

func ciGlyph(state ado.BuildState, statusText string) string {
	switch state {
	case ado.BuildFailed:
		return styleRed.Render("×")
	case ado.BuildRunning:
		return styleYellow.Render("●")
	case ado.BuildQueued:
		return styleDim.Render("○")
	case ado.BuildPartial:
		return styleYellow.Render("!")
	case ado.BuildPassed:
		return styleGreen.Render("✓")
	case ado.BuildNone:
		if statusText == "canceled" {
			return styleDim.Render("–")
		}
	}
	return styleDim.Render("·")
}

func ciCell(b buildResult, ok, narrow bool) string {
	switch {
	case !ok:
		return styleDim.Render("…")
	case b.err != nil:
		return styleRed.Render("?")
	}
	s := b.summary
	if s.Total() == 0 {
		return styleDim.Render("·")
	}
	if narrow {
		if s.Total() == 1 {
			return ciGlyph(s.State, s.Checks[0].StatusText)
		}
		return ciGlyph(s.State, "")
	}
	if s.Total() == 1 {
		switch s.State {
		case ado.BuildFailed:
			return styleRed.Render("× err")
		case ado.BuildRunning:
			return styleYellow.Render("● run")
		case ado.BuildQueued:
			return styleDim.Render("○ queue")
		case ado.BuildPartial:
			return styleYellow.Render("! warn")
		case ado.BuildPassed:
			return styleGreen.Render("✓ pass")
		case ado.BuildNone:
			if s.Checks[0].StatusText == "canceled" {
				return styleDim.Render("– stop")
			}
			return styleDim.Render("·")
		}
	}
	switch s.State {
	case ado.BuildFailed:
		return styleRed.Render(fmt.Sprintf("× %d err", s.Failed))
	case ado.BuildRunning:
		return styleYellow.Render(fmt.Sprintf("● %d run", s.Running))
	case ado.BuildQueued:
		return styleDim.Render(fmt.Sprintf("○ %d que", s.Queued))
	case ado.BuildPartial:
		return styleYellow.Render(fmt.Sprintf("! %d wrn", s.Partial))
	case ado.BuildPassed:
		return styleGreen.Render(fmt.Sprintf("✓ %d/%d", s.Passed, s.Total()))
	case ado.BuildNone:
	}
	return styleDim.Render("·")
}

func ciCheckSourceTag(c ado.CICheck) string {
	switch c.Kind {
	case ado.CICheckPolicyBuild:
		if !c.IsBlocking {
			return "policy · optional"
		}
		return "policy"
	case ado.CICheckPRRun:
		return "PR trigger · not a policy"
	case ado.CICheckBranchRun:
		return "source branch · not a policy"
	case ado.CICheckExternal:
		if !c.IsBlocking {
			return "external · optional"
		}
		return "external"
	}
	return ""
}

func ciCheckMenuLabel(c ado.CICheck) string {
	glyph := ciGlyph(c.State, c.StatusText)
	if c.Kind == ado.CICheckPolicyBuild {
		label := glyph + " " + c.Name + ": build " + c.StatusText
		if !c.IsBlocking {
			label += styleDim.Render("  optional")
		}
		return label
	}
	return glyph + " " + c.Name + styleDim.Render("  "+c.StatusText+" · "+ciCheckSourceTag(c))
}

func ciFilterTokens(s ado.CISummary) []string {
	if s.Total() == 0 {
		return nil
	}
	var parts []string
	if s.Failed > 0 || s.State == ado.BuildFailed {
		parts = append(parts, "failed", "error", "err")
	}
	if s.Running > 0 || s.State == ado.BuildRunning {
		parts = append(parts, "running", "run")
	}
	if s.Queued > 0 || s.State == ado.BuildQueued {
		parts = append(parts, "queued", "queue")
	}
	if s.Partial > 0 || s.State == ado.BuildPartial {
		parts = append(parts, "partial", "warn")
	}
	if s.State == ado.BuildPassed {
		parts = append(parts, "passed", "pass", "succeeded")
	}
	for _, c := range s.Checks {
		parts = append(parts, c.Name, c.StatusText)
	}
	return parts
}

type ciCallbacks struct {
	client      *ado.Client
	pr          ado.PullRequest
	openRun     func(ado.CICheck) tea.Cmd
	openTrigger func(back modal) (modal, tea.Cmd)
	execRequeue func(ado.CICheck) tea.Cmd
	execRetry   func(ado.CICheck) tea.Cmd
	execQueue   func(name string, defID int, branch string) tea.Cmd
	execCancel  func(ado.CICheck) tea.Cmd
}

func newCICallbacks(client *ado.Client, pr ado.PullRequest, openRun func(ado.CICheck) tea.Cmd, openTrigger func(modal) (modal, tea.Cmd)) ciCallbacks {
	projectID := pr.Repository.Project.ID
	return ciCallbacks{
		client:      client,
		pr:          pr,
		openRun:     openRun,
		openTrigger: openTrigger,
		execRequeue: func(c ado.CICheck) tea.Cmd {
			evalID, name := c.Policy.EvaluationID, c.Name
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := requeuePolicy(ctx, client, pr, evalID)
				return ciActionMsg{from: client, prID: pr.ID, kind: ciActRequeue, evaluationID: evalID, name: name, err: err}
			}
		},
		execRetry: func(c ado.CICheck) tea.Cmd {
			r, name := *c.Run, c.Name
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := retryRun(ctx, client, projectID, r.ID)
				return ciActionMsg{from: client, prID: pr.ID, kind: ciActRetry, name: name, run: r, err: err}
			}
		},
		execQueue: func(name string, defID int, branch string) tea.Cmd {
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				run, err := queueRun(ctx, client, projectID, defID, branch)
				return ciActionMsg{from: client, prID: pr.ID, kind: ciActQueue, name: name, run: run, err: err}
			}
		},
		execCancel: func(c ado.CICheck) tea.Cmd {
			r, name := *c.Run, c.Name
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := cancelRun(ctx, client, projectID, r.ID)
				return ciActionMsg{from: client, prID: pr.ID, kind: ciActCancel, name: name, run: r, err: err}
			}
		},
	}
}

func ciOpenOrMenu(checks []ado.CICheck, cb ciCallbacks) (modal, tea.Cmd) {
	switch {
	case len(checks) == 0:
		picker, loadCmd := cb.openTrigger(nil)
		c := newConfirm(fmt.Sprintf("No pipeline runs for !%d yet. Trigger a pipeline?", cb.pr.ID), loadCmd).focusYes()
		c.then = picker
		return c, nil
	case len(checks) == 1 && checks[0].HasRun():
		return nil, cb.openRun(checks[0])
	case len(checks) == 1 && checks[0].Kind == ado.CICheckExternal && checks[0].ExternalURL != "":
		return nil, openURL(checks[0].ExternalURL, "opened "+checks[0].Name)
	}
	return ciRunsMenu(checks, cb), nil
}

func ciRunsMenu(checks []ado.CICheck, cb ciCallbacks) modal {
	m := &menuModal{title: "Pipeline runs", hint: "Q re-queue · X cancel · o browser · t trigger"}
	for _, c := range checks {
		c := c
		it := menuItem{label: ciCheckMenuLabel(c)}
		switch {
		case c.HasRun():
			it.run = func() (modal, tea.Cmd) { return nil, cb.openRun(c) }
		case c.Kind == ado.CICheckExternal && c.ExternalURL != "":
			it.run = func() (modal, tea.Cmd) { return nil, openURL(c.ExternalURL, "opened "+c.Name) }
		default:
			it.disabled = c.Name + " hasn't run yet"
		}
		m.items = append(m.items, it)
	}
	m.keys = map[string]func(int) (modal, tea.Cmd){
		"Q": func(i int) (modal, tea.Cmd) {
			return ciRerunModal(checks[i], cb, m)
		},
		"X": func(i int) (modal, tea.Cmd) {
			return ciCancelModal(checks[i], cb, m)
		},
		"o": func(i int) (modal, tea.Cmd) {
			c := checks[i]
			switch {
			case c.HasRun():
				return nil, openURL(cb.client.RunURL(cb.pr.Repository.Project.Name, c.Run.ID), "opened run "+runNumLabel(*c.Run))
			case c.ExternalURL != "":
				return nil, openURL(c.ExternalURL, "opened "+c.Name)
			default:
				return m, statusCmd(c.Name + " hasn't run yet")
			}
		},
		"t": func(int) (modal, tea.Cmd) {
			return cb.openTrigger(m)
		},
	}
	return m
}

func ciRerunKey(checks []ado.CICheck, cb ciCallbacks) (modal, tea.Cmd) {
	var runnable []ado.CICheck
	for _, c := range checks {
		if c.Kind != ado.CICheckExternal {
			runnable = append(runnable, c)
		}
	}
	switch len(runnable) {
	case 0:
		return nil, statusCmd("no pipeline to re-run")
	case 1:
		return ciRerunModal(runnable[0], cb, nil)
	}
	m := &menuModal{title: "Re-run / Re-queue"}
	for _, c := range runnable {
		c := c
		m.items = append(m.items, menuItem{
			label: ciCheckMenuLabel(c),
			run: func() (modal, tea.Cmd) {
				return ciRerunModal(c, cb, m)
			},
		})
	}
	return m, nil
}

func ciRerunModal(c ado.CICheck, cb ciCallbacks, back modal) (modal, tea.Cmd) {
	if c.Kind == ado.CICheckExternal {
		return back, statusCmd(c.Name + " is an external check")
	}
	if c.IsActive() {
		return back, statusCmd(c.Name + " is already running")
	}
	if c.Kind == ado.CICheckPolicyBuild && c.Policy != nil {
		confirm := newConfirm("Re-queue "+c.Name+"?", cb.execRequeue(c)).focusYes()
		confirm.back = back
		return confirm, nil
	}
	branch := cb.pr.SourceRefName
	if c.Run != nil && c.Run.SourceBranch != "" {
		branch = c.Run.SourceBranch
	}
	shortBranch := strings.TrimPrefix(branch, "refs/heads/")
	defID := c.DefinitionID
	if defID == 0 && c.Run != nil {
		defID = c.Run.Definition.ID
	}
	if c.HasRun() && (c.Run.Result == "failed" || c.Run.Result == "partiallySucceeded" || c.Run.Result == "succeededWithIssues" || c.Run.Result == "canceled") {
		sub := &menuModal{title: "Re-run " + c.Name, back: back}
		sub.items = []menuItem{
			{
				label: "Retry failed jobs",
				run: func() (modal, tea.Cmd) {
					cf := newConfirm("Retry failed jobs of "+c.Name+"?", cb.execRetry(c)).focusYes()
					cf.back = sub
					return cf, nil
				},
			},
			{
				label: "Run new",
				run: func() (modal, tea.Cmd) {
					if defID == 0 {
						return sub, statusCmd(c.Name + " has no pipeline definition ID")
					}
					cf := newConfirm("Queue a new run of "+c.Name+" on "+shortBranch+"?", cb.execQueue(c.Name, defID, branch)).focusYes()
					cf.back = sub
					return cf, nil
				},
			},
		}
		return sub, nil
	}
	if defID == 0 {
		return back, statusCmd(c.Name + " has no pipeline definition ID")
	}
	confirm := newConfirm("Queue a new run of "+c.Name+" on "+shortBranch+"?", cb.execQueue(c.Name, defID, branch)).focusYes()
	confirm.back = back
	return confirm, nil
}

func ciCancelKey(checks []ado.CICheck, cb ciCallbacks) (modal, tea.Cmd) {
	var active []ado.CICheck
	for _, c := range checks {
		if c.IsCancellable() {
			active = append(active, c)
		}
	}
	switch len(active) {
	case 0:
		return nil, statusCmd("no running pipeline to cancel")
	case 1:
		return ciCancelModal(active[0], cb, nil)
	}
	m := &menuModal{title: "Cancel run"}
	for _, c := range active {
		c := c
		m.items = append(m.items, menuItem{
			label: ciCheckMenuLabel(c),
			run: func() (modal, tea.Cmd) {
				return ciCancelModal(c, cb, m)
			},
		})
	}
	return m, nil
}

func ciCancelModal(c ado.CICheck, cb ciCallbacks, back modal) (modal, tea.Cmd) {
	if !c.IsCancellable() {
		return back, statusCmd(c.Name + " is not running")
	}
	confirm := newConfirm(fmt.Sprintf("Cancel %s (%s)?", c.Name, runNumLabel(*c.Run)), cb.execCancel(c))
	confirm.back = back
	return confirm, nil
}

func ciCheckFromRun(name string, policy *ado.Policy, r ado.Run, defID int, kind ado.CICheckKind, blocking bool) ado.CICheck {
	rc := r
	var runPtr *ado.Run
	if r.ID > 0 {
		runPtr = &rc
	}
	resolvedDef := defID
	if resolvedDef == 0 {
		resolvedDef = r.Definition.ID
	}
	if policy != nil {
		if resolvedDef == 0 {
			resolvedDef = policy.BuildDefinitionID()
		}
		// Keep policy status when run details haven't loaded yet, else reflect live run state.
		p := *policy
		if r.Status != "" {
			switch {
			case r.Status == "inProgress":
				p.Status = "running"
			case r.Status == "notStarted" || r.Status == "postponed":
				p.Status = "queued"
			case r.Result == "succeeded" || r.Result == "partiallySucceeded" || r.Result == "succeededWithIssues":
				p.Status = "approved"
			case r.Result == "failed":
				p.Status = "rejected"
			}
		}
		state, text := ado.BuildNone, p.Status
		if r.Status != "" {
			switch {
			case r.Status == "inProgress" || r.Status == "cancelling":
				state, text = ado.BuildRunning, r.Status
			case r.Status == "notStarted" || r.Status == "postponed":
				state, text = ado.BuildQueued, "queued"
			case r.Result == "failed":
				state, text = ado.BuildFailed, "failed"
			case r.Result == "partiallySucceeded" || r.Result == "succeededWithIssues":
				state, text = ado.BuildPartial, "partially succeeded"
			case r.Result == "succeeded":
				state, text = ado.BuildPassed, "succeeded"
			}
		}
		return ado.CICheck{
			Kind:         ado.CICheckPolicyBuild,
			Name:         cmp.Or(name, buildPolicyName(p)),
			State:        state,
			StatusText:   text,
			IsBlocking:   p.Configuration.IsBlocking,
			Policy:       &p,
			Run:          runPtr,
			DefinitionID: resolvedDef,
		}
	}
	state, text := ado.BuildNone, cmp.Or(r.Result, r.Status)
	switch {
	case r.Status == "inProgress" || r.Status == "cancelling":
		state, text = ado.BuildRunning, "in progress"
	case r.Status == "notStarted" || r.Status == "postponed":
		state, text = ado.BuildQueued, "queued"
	case r.Result == "failed":
		state, text = ado.BuildFailed, "failed"
	case r.Result == "partiallySucceeded" || r.Result == "succeededWithIssues":
		state, text = ado.BuildPartial, "partially succeeded"
	case r.Result == "succeeded":
		state, text = ado.BuildPassed, "succeeded"
	}
	return ado.CICheck{
		Kind:         kind,
		Name:         cmp.Or(name, r.Definition.Name, runNumLabel(r)),
		State:        state,
		StatusText:   text,
		IsBlocking:   blocking,
		Run:          runPtr,
		DefinitionID: resolvedDef,
	}
}

// --- Pipeline trigger picker ---

type pipelinePicker struct {
	pr      ado.PullRequest
	list    pickList
	loading bool
	back    modal
	onQueue func(p ado.Pipeline) tea.Cmd
}

func newPipelinePicker(client *ado.Client, pr ado.PullRequest, back modal, onQueue func(ado.Pipeline) tea.Cmd) (*pipelinePicker, tea.Cmd) {
	p := &pipelinePicker{pr: pr, loading: true, back: back, onQueue: onQueue}
	p.list.typing = true
	projectID := pr.Repository.Project.ID
	prID := pr.ID
	return p, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ps, err := listPipelines(ctx, client, projectID)
		return prPipelinesMsg{from: client, prID: prID, projectID: projectID, pipelines: ps, err: err}
	}
}

func (p *pipelinePicker) onPipelines(msg prPipelinesMsg) (modal, tea.Cmd) {
	p.loading = false
	if msg.err != nil {
		return p.back, statusCmd("error: list pipelines: " + msg.err.Error())
	}
	ps := append([]ado.Pipeline(nil), msg.pipelines...)
	sort.SliceStable(ps, func(i, j int) bool {
		ai, aj := pipelineActivity(ps[i]), pipelineActivity(ps[j])
		if !ai.Equal(aj) {
			return ai.After(aj)
		}
		return strings.ToLower(ps[i].Name) < strings.ToLower(ps[j].Name)
	})
	items := make([]pickItem, 0, len(ps))
	for _, pl := range ps {
		pl := pl
		items = append(items, pickItem{
			search: pl.Name + " " + pl.Path,
			value:  pl,
			render: func(width int) string { return pipelineRow(pl, width) },
		})
	}
	p.list.setItems(items)
	return p, nil
}

func (p *pipelinePicker) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	if p.loading {
		if msg.String() == "esc" {
			return p.back, nil
		}
		return p, nil
	}
	handled, activate := p.list.key(msg, pickerRows)
	switch {
	case activate:
		return p.pick()
	case !handled && msg.String() == "esc":
		return p.back, nil
	}
	return p, nil
}

func (p *pipelinePicker) pick() (modal, tea.Cmd) {
	it, ok := p.list.selected()
	if !ok {
		return p, nil
	}
	pl := it.value.(ado.Pipeline)
	confirm := newConfirm(fmt.Sprintf("Run %s on %s?", pl.Name, p.pr.SourceBranch()), p.onQueue(pl)).focusYes()
	confirm.back = p
	return confirm, nil
}

func (p *pipelinePicker) view(width int) string {
	w := max(40, min(width-8, 90))
	lines := []string{styleSection.Render("Trigger pipeline") + styleDim.Render("  on "+p.pr.SourceBranch()), ""}
	if p.loading {
		lines = append(lines, styleDim.Render("loading pipelines…"))
	} else {
		rows := min(pickerRows, max(1, len(p.list.visible())))
		if _, ok := p.list.filterLine(); ok {
			rows++
		}
		lines = append(lines, p.list.view(w, rows)...)
	}
	lines = append(lines, "", styleDim.Render("type to filter · enter pick · esc close"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

func (p *pipelinePicker) clickRow(row int) (modal, tea.Cmd) {
	if !p.loading && p.list.click(row-2) {
		return p.pick()
	}
	return p, nil
}

// --- Dashboard direct run viewer ---

type prRunScreen struct {
	pr     ado.PullRequest
	check  ado.CICheck
	run    *runView
	start  string
	wait   *runWait
	status string
}

func newPRRunScreen(client *ado.Client, pr ado.PullRequest, c ado.CICheck) (*prRunScreen, tea.Cmd) {
	if !c.HasRun() {
		return nil, statusCmd(c.Name + " hasn't run yet")
	}
	proj := ado.ProjectInfo{ID: pr.Repository.Project.ID, Name: pr.Repository.Project.Name}
	rv := newRunView(client, proj, *c.Run)
	return &prRunScreen{pr: pr, check: c, run: rv}, rv.refresh()
}

func (s *prRunScreen) currentCheck() ado.CICheck {
	return ciCheckFromRun(s.check.Name, s.check.Policy, s.run.run, s.check.DefinitionID, s.check.Kind, s.check.IsBlocking)
}

func (s *prRunScreen) bodyHeight(height int) int { return max(1, height-4) }

func (s *prRunScreen) crumbs() string {
	r := s.run.run
	crumbs := []string{
		"Pull requests",
		fmt.Sprintf("!%d", s.pr.ID),
		"Pipeline runs",
		styleTitle.Render("run " + runNumLabel(r)),
		runGlyph(r.Status, r.Result) + " " + cmp.Or(s.check.Name, r.Definition.Name),
	}
	out := strings.Join(crumbs, styleDim.Render(" › "))
	if s.run.loading {
		out += styleYellow.Render("  loading…")
	}
	return " " + out
}

func (s *prRunScreen) view(width, height int, groups []bindingGroup) string {
	var b strings.Builder
	title := fmt.Sprintf("!%d %s", s.pr.ID, s.pr.Title)
	if s.pr.IsDraft {
		title = draftBadge(false) + " " + styleTitle.Render(title)
	} else {
		title = styleTitle.Render(title)
	}
	b.WriteString(truncate(title, width) + "\n")
	b.WriteString(truncate(s.crumbs(), width) + "\n")
	for _, line := range s.run.view(width, s.bodyHeight(height)) {
		b.WriteString(truncate(line, width) + "\n")
	}
	status := s.status
	if findStatus, ok := s.run.find.status(); ok {
		status = findStatus
	}
	if strings.HasPrefix(status, "error") {
		status = styleRed.Render(status)
	}
	b.WriteString(truncate(status, width) + "\n")
	b.WriteString(styleDim.Render(truncate(footer(groups), width)))
	return b.String()
}

func (s *prRunScreen) updateRun(msg tea.Msg) tea.Cmd {
	cmd := s.run.update(msg)
	if _, ok := msg.(runLoadedMsg); ok && s.start == "" && s.run.records != nil {
		s.start = runState(s.run.run)
	}
	return cmd
}

func (s *prRunScreen) onWaitTick(client *ado.Client, msg runWaitTickMsg) tea.Cmd {
	if s.wait == nil || s.wait.evaluationID != msg.evaluationID {
		return nil
	}
	return waitPolicyPoll(client, s.pr, msg.evaluationID)
}

func (s *prRunScreen) onWait(client *ado.Client, msg runWaitMsg) tea.Cmd {
	w := s.wait
	if w == nil || w.evaluationID != msg.evaluationID {
		return nil
	}
	if id := msg.policy.BuildID(); msg.err == nil && id != 0 && id != w.from {
		p := msg.policy
		if s.check.Policy != nil && p.BuildName == "" {
			p.BuildName = s.check.Policy.BuildName
		}
		proj := ado.ProjectInfo{ID: s.pr.Repository.Project.ID, Name: s.pr.Repository.Project.Name}
		s.check.Policy = &p
		s.check.Run = &ado.Run{ID: id}
		s.run = newRunView(client, proj, ado.Run{ID: id})
		s.start, s.wait = "", nil
		s.status = fmt.Sprintf("%s: new run #%d", w.name, id)
		return s.run.refresh()
	}
	if time.Now().After(w.until) {
		s.wait = nil
		s.status = "no new run of " + w.name + " yet: press esc, then b to look again"
		return nil
	}
	return waitPolicyTick(s.pr.ID, w.evaluationID)
}

func (s *prRunScreen) key(msg tea.KeyMsg, width, height int, cb ciCallbacks) (closed, changed bool, nextModal modal, cmd tea.Cmd) {
	s.status = ""
	if handled, c := s.run.key(msg, s.bodyHeight(height)); handled {
		return false, false, nil, c
	}
	r := s.run.run
	switch msg.String() {
	case "esc":
		changed = s.start != "" && runState(r) != s.start
		return true, changed, nil, nil
	case "o":
		return false, false, nil, openURL(s.run.client.RunURL(s.pr.Repository.Project.Name, r.ID), "opened run "+runNumLabel(r))
	case "y":
		return false, false, nil, copyRun(s.run.client, s.pr.Repository.Project.Name, r)
	case "r":
		return false, false, nil, s.run.refresh()
	case "Q":
		nextModal, cmd = ciRerunModal(s.currentCheck(), cb, nil)
		return false, false, nextModal, cmd
	case "X":
		nextModal, cmd = ciCancelModal(s.currentCheck(), cb, nil)
		return false, false, nextModal, cmd
	}
	_ = width
	return false, false, nil, nil
}

// ciSummaryLine renders the 1-line CI/CD summary bar at the bottom of the PR list.
func (m Model) ciSummaryLine() string {
	hint := styleDim.Render("b runs · Q rerun · X cancel · t trigger")
	rows := m.rows()
	r, ok := m.selected(rows)
	if !ok || r.pr == nil {
		return styleDim.Render("CI: select a pull request · " + ansi.Strip(hint))
	}
	pr := r.pr
	prefix := styleHeader.Render(fmt.Sprintf("CI !%d:", pr.ID)) + " "
	b, has := m.builds[pr.ID]
	var body string
	switch {
	case !has:
		body = styleDim.Render("loading…")
	case b.err != nil:
		body = styleRed.Render("error: " + b.err.Error())
	case len(b.summary.Checks) == 0:
		body = styleDim.Render("no runs yet · press t to trigger a pipeline")
	default:
		parts := make([]string, 0, len(b.summary.Checks))
		for _, c := range b.summary.Checks {
			parts = append(parts, ciGlyph(c.State, c.StatusText)+" "+c.Name)
		}
		body = strings.Join(parts, styleDim.Render(" · "))
	}
	left := prefix + body
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(hint)
	if gap >= 3 {
		return left + strings.Repeat(" ", gap) + hint
	}
	return left
}
