package ui

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// A pull request's pipeline runs, opened from its detail: the run view
// takes the body, and esc comes back to the Overview.

const (
	runWaitPoll = 3 * time.Second
	runWaitMax  = 2 * time.Minute
)

type (
	prRunsMsg struct {
		prID int
		runs []ado.Run
		err  error
	}
	requeuedMsg struct {
		prID         int
		evaluationID string
		name         string
		err          error
	}
	runWaitTickMsg struct {
		prID         int
		evaluationID string
	}
	runWaitMsg struct {
		prID         int
		evaluationID string
		policy       ado.Policy
		err          error
	}
)

func (m prRunsMsg) pullRequest() int      { return m.prID }
func (m requeuedMsg) pullRequest() int    { return m.prID }
func (m runWaitTickMsg) pullRequest() int { return m.prID }
func (m runWaitMsg) pullRequest() int     { return m.prID }

// prRun is a row of the runs menu: a build policy's latest run, or the
// latest run of a pipeline no policy asks for.
type prRun struct {
	name   string
	policy *ado.Policy
	run    ado.Run // ID 0: the policy hasn't queued a run
}

// runWait watches a re-queued policy for its new run, to switch to it.
type runWait struct {
	evaluationID, name string
	from               int // the run shown when it was re-queued
	until              time.Time
}

// prProject is the project the pull request and its runs live in.
func (d *detailModel) prProject() ado.ProjectInfo {
	p := d.pr.Repository.Project
	return ado.ProjectInfo{ID: p.ID, Name: p.Name}
}

// buildPolicies are the pull request's enabled build policies, one per
// pipeline: the same one set on the branch and on the repo shows once.
func (d *detailModel) buildPolicies() []ado.Policy {
	if d.data == nil {
		return nil
	}
	var out []ado.Policy
	seen := map[string]bool{}
	for _, p := range d.data.Policies {
		key := p.EvaluationID
		if id := p.BuildDefinitionID(); id != 0 {
			key = strconv.Itoa(id)
		}
		if p.Configuration.Type.ID == policyTypeBuild && !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

func (d *detailModel) policyByEvaluation(id string) (ado.Policy, bool) {
	for _, p := range d.buildPolicies() {
		if p.EvaluationID == id {
			return p, true
		}
	}
	return ado.Policy{}, false
}

func buildPolicyName(p ado.Policy) string {
	name, _ := p.Configuration.Settings["displayName"].(string)
	return cmp.Or(name, p.BuildName, "Build")
}

// prRuns lines the build policies up first, then the latest run of each
// other pipeline on the pull request's merge ref (runs come newest first).
func (d *detailModel) prRuns(runs []ado.Run) []prRun {
	byID := map[int]ado.Run{}
	for _, r := range runs {
		byID[r.ID] = r
	}
	var out []prRun
	covered := map[int]bool{}
	for _, p := range d.buildPolicies() {
		covered[p.BuildDefinitionID()] = true
		r, ok := byID[p.BuildID()]
		if !ok {
			r = ado.Run{ID: p.BuildID()}
		}
		out = append(out, prRun{name: buildPolicyName(p), policy: &p, run: r})
	}
	for _, r := range runs {
		if !covered[r.Definition.ID] {
			covered[r.Definition.ID] = true
			out = append(out, prRun{name: r.Definition.Name, run: r})
		}
	}
	return out
}

// loadRuns asks for the runs on the pull request's merge ref, for the b menu.
func (d *detailModel) loadRuns() tea.Cmd {
	if d.data == nil {
		return nil
	}
	d.status = "loading runs…"
	client, pr := d.client, d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		runs, err := client.PRRuns(ctx, pr)
		return prRunsMsg{prID: pr.ID, runs: runs, err: err}
	}
}

// onRuns opens the only run straight away, else the menu of them.
func (d *detailModel) onRuns(msg prRunsMsg) tea.Cmd {
	if d.status == "loading runs…" {
		d.status = ""
	}
	if d.modal != nil || d.run != nil || d.tab != tabOverview {
		return nil // moved on while they loaded
	}
	if msg.err != nil {
		d.status = "error: list runs: " + msg.err.Error()
	}
	runs := d.prRuns(msg.runs)
	switch {
	case len(runs) == 0 && msg.err == nil:
		d.status = "no pipeline run for this pull request yet"
	case len(runs) == 1 && runs[0].run.ID != 0:
		return d.openRun(runs[0])
	case len(runs) > 0:
		d.modal = d.runsMenu(runs)
	}
	return nil
}

func (d *detailModel) runsMenu(runs []prRun) modal {
	m := &menuModal{title: "Pipeline runs", hint: "Q re-queue"}
	for _, r := range runs {
		it := menuItem{label: d.prRunLabel(r), run: func() (modal, tea.Cmd) { return nil, d.openRun(r) }}
		if r.run.ID == 0 {
			it.disabled = r.name + " hasn't run yet"
		}
		m.items = append(m.items, it)
	}
	m.keys = map[string]func(int) (modal, tea.Cmd){"Q": func(i int) (modal, tea.Cmd) {
		r := runs[i]
		if r.policy == nil {
			return m, statusCmd(r.name + " isn't a build policy: open it in the browser (o) to run it again")
		}
		return d.confirmRequeue(*r.policy, m)
	}}
	return m
}

func (d *detailModel) prRunLabel(r prRun) string {
	if r.policy != nil {
		label := d.policyLine(*r.policy)
		if !r.policy.Configuration.IsBlocking {
			label += styleDim.Render("  optional")
		}
		return label
	}
	return runGlyph(r.run.Status, r.run.Result) + " " + r.name + styleDim.Render("  "+cmp.Or(r.run.Result, r.run.Status)+" · not a policy")
}

// openRun shows a run in place of the tabs.
func (d *detailModel) openRun(r prRun) tea.Cmd {
	d.run = newRunView(d.client, d.prProject(), r.run)
	d.runName, d.runPolicy, d.runStart, d.wait = r.name, r.policy, "", nil
	d.modal = nil
	return d.run.refresh()
}

// openPolicyRun opens a build policy's latest run, for a click on its check.
func (d *detailModel) openPolicyRun(p ado.Policy) tea.Cmd {
	if p.BuildID() == 0 {
		return statusCmd(buildPolicyName(p) + " hasn't run yet")
	}
	return d.openRun(prRun{name: buildPolicyName(p), policy: &p, run: ado.Run{ID: p.BuildID()}})
}

func runState(r ado.Run) string { return r.Status + "/" + r.Result }

// closeRun goes back to the Overview; a run that changed while shown
// refreshes the checks now rather than on the next tick.
func (d *detailModel) closeRun() tea.Cmd {
	changed := d.runStart != "" && runState(d.run.run) != d.runStart
	d.run, d.runPolicy, d.wait = nil, nil, nil
	if changed {
		return d.reload()
	}
	return nil
}

func (d *detailModel) onRunMsg(msg tea.Msg) tea.Cmd {
	if d.run == nil {
		return nil
	}
	cmd := d.run.update(msg)
	if _, ok := msg.(runLoadedMsg); ok && d.runStart == "" && d.run.records != nil {
		d.runStart = runState(d.run.run)
	}
	return cmd
}

// runKey handles keys while a run is shown.
func (d *detailModel) runKey(msg tea.KeyMsg) tea.Cmd {
	if handled, cmd := d.run.key(msg, d.bodyHeight()); handled {
		return cmd
	}
	r := d.run.run
	switch msg.String() {
	case "esc":
		return d.closeRun()
	case "o":
		return openURL(d.client.RunURL(d.prProject().Name, r.ID), "opened run "+d.runLabel())
	case "y":
		return copyRun(d.client, d.prProject().Name, r)
	case "r":
		return d.run.refresh()
	case "Q":
		if d.runPolicy == nil {
			return statusCmd(d.runName + " isn't a build policy: open it in the browser (o) to run it again")
		}
		var cmd tea.Cmd
		d.modal, cmd = d.confirmRequeue(*d.runPolicy, nil)
		return cmd
	case "1", "2", "3", "4", "[", "]":
		d.run, d.runPolicy, d.wait = nil, nil, nil
		return d.onKey(msg)
	case "v":
		d.modal = d.voteMenu()
	case "m":
		d.modal = d.completeMenu()
	case "E":
		d.modal = d.newPREditor()
	case "c":
		return prCheckout(d.client.Org, d.pr)
	}
	return nil
}

// runCrumbs takes the tabs line while a run is shown.
func (d *detailModel) runCrumbs() string {
	r := d.run.run
	s := strings.Join([]string{
		"Overview", "Pipeline runs",
		styleTitle.Render("run " + d.runLabel()),
		runGlyph(r.Status, r.Result) + " " + d.runName,
	}, styleDim.Render(" › "))
	if d.run.loading {
		s += styleYellow.Render("  loading…")
	}
	return " " + s
}

func (d *detailModel) runLabel() string {
	return cmp.Or(d.run.run.BuildNumber, "#"+strconv.Itoa(d.run.run.ID))
}

// requeueKey is Q on the Overview: the only build policy, else a menu.
func (d *detailModel) requeueKey() tea.Cmd {
	ps := d.buildPolicies()
	switch len(ps) {
	case 0:
		return statusCmd("no build policy to re-queue")
	case 1:
		var cmd tea.Cmd
		d.modal, cmd = d.confirmRequeue(ps[0], nil)
		return cmd
	}
	m := &menuModal{title: "Re-queue"}
	for _, p := range ps {
		m.items = append(m.items, menuItem{label: d.policyLine(p), run: func() (modal, tea.Cmd) {
			return d.confirmRequeue(p, m)
		}})
	}
	d.modal = m
	return nil
}

// confirmRequeue asks before re-queueing a build policy; n goes back to
// the menu it came from. A running policy is left alone.
func (d *detailModel) confirmRequeue(p ado.Policy, back modal) (modal, tea.Cmd) {
	if fresh, ok := d.policyByEvaluation(p.EvaluationID); ok {
		p = fresh
	}
	name := buildPolicyName(p)
	if p.Status == "running" {
		return back, statusCmd(name + " is already running")
	}
	c := newConfirm("Re-queue "+name+"?", d.requeue(p)).focusYes()
	c.back = back
	return c, nil
}

// requeuePolicy is the write behind Q; tests swap it.
var requeuePolicy = func(ctx context.Context, c *ado.Client, pr ado.PullRequest, evaluationID string) error {
	return c.RequeuePolicy(ctx, pr, evaluationID)
}

func (d *detailModel) requeue(p ado.Policy) tea.Cmd {
	client, pr, name := d.client, d.pr, buildPolicyName(p)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := requeuePolicy(ctx, client, pr, p.EvaluationID)
		return requeuedMsg{prID: pr.ID, evaluationID: p.EvaluationID, name: name, err: err}
	}
}

// onRequeued switches a shown run to the new one once it starts.
func (d *detailModel) onRequeued(msg requeuedMsg) tea.Cmd {
	if msg.err != nil {
		d.status = "error: re-queue " + msg.name + ": " + msg.err.Error()
		return nil
	}
	if d.run != nil && d.runPolicy != nil && d.runPolicy.EvaluationID == msg.evaluationID {
		d.wait = &runWait{evaluationID: msg.evaluationID, name: msg.name, from: d.run.run.ID, until: time.Now().Add(runWaitMax)}
		d.status = "re-queued " + msg.name + ", waiting for its run…"
		return d.waitTick()
	}
	d.status = "re-queued " + msg.name
	return d.reload()
}

func (d *detailModel) waitTick() tea.Cmd {
	prID, id := d.pr.ID, d.wait.evaluationID
	return tea.Tick(runWaitPoll, func(time.Time) tea.Msg { return runWaitTickMsg{prID: prID, evaluationID: id} })
}

func (d *detailModel) onWaitTick(msg runWaitTickMsg) tea.Cmd {
	if d.wait == nil || d.wait.evaluationID != msg.evaluationID {
		return nil
	}
	client, pr, id := d.client, d.pr, msg.evaluationID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		p, err := client.PolicyEvaluation(ctx, pr, id)
		return runWaitMsg{prID: pr.ID, evaluationID: id, policy: p, err: err}
	}
}

func (d *detailModel) onWait(msg runWaitMsg) tea.Cmd {
	w := d.wait
	if w == nil || w.evaluationID != msg.evaluationID {
		return nil
	}
	if id := msg.policy.BuildID(); msg.err == nil && id != 0 && id != w.from {
		p := msg.policy
		if old, ok := d.policyByEvaluation(p.EvaluationID); ok {
			p.BuildName = old.BuildName
		}
		cmd := d.openRun(prRun{name: w.name, policy: &p, run: ado.Run{ID: id}})
		d.status = fmt.Sprintf("%s: new run #%d", w.name, id)
		return tea.Batch(cmd, d.reload())
	}
	if time.Now().After(w.until) {
		d.wait = nil
		d.status = "no new run of " + w.name + " yet: press esc, then b to look again"
		return nil
	}
	return d.waitTick()
}
