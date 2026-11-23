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
		prID       int
		runs       []ado.Run
		branchRuns []ado.Run
		err        error
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

func (d *detailModel) ciChecks() []ado.CICheck {
	if d.data == nil {
		return nil
	}
	return ado.SummarizeCI(d.data.Policies, d.data.PRRuns, d.data.BranchRuns, d.data.Statuses).Checks
}

func (d *detailModel) ciCallbacks() ciCallbacks {
	return newCICallbacks(
		d.client,
		d.pr,
		func(c ado.CICheck) tea.Cmd { return d.openCICheck(c) },
		func(back modal) (modal, tea.Cmd) { return d.openTriggerPicker(back) },
	)
}

func (d *detailModel) openTriggerPicker(back modal) (modal, tea.Cmd) {
	return newPipelinePicker(d.client, d.pr, back, func(pl ado.Pipeline) tea.Cmd {
		return d.ciCallbacks().execQueue(pl.Name, pl.ID, d.pr.SourceRefName)
	})
}

// loadRuns asks for PR and source-branch runs for the b menu.
func (d *detailModel) loadRuns() tea.Cmd {
	if d.data == nil {
		return nil
	}
	d.status = "loading runs…"
	client, pr := d.client, d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		runs, branchRuns, err := client.PRAllRuns(ctx, pr)
		return prRunsMsg{prID: pr.ID, runs: runs, branchRuns: branchRuns, err: err}
	}
}

// onRuns opens the only run straight away, asks to trigger if none, else shows the menu.
func (d *detailModel) onRuns(msg prRunsMsg) tea.Cmd {
	if d.status == "loading runs…" {
		d.status = ""
	}
	if d.modal != nil || d.run != nil {
		return nil // moved on while they loaded
	}
	if msg.err != nil {
		d.status = "error: list runs: " + msg.err.Error()
	}
	if d.data != nil {
		if msg.err == nil {
			d.data.PRRuns = msg.runs
			d.data.BranchRuns = msg.branchRuns
		} else {
			if msg.runs != nil {
				d.data.PRRuns = msg.runs
			}
			if msg.branchRuns != nil {
				d.data.BranchRuns = msg.branchRuns
			}
		}
	}
	var cmd tea.Cmd
	d.modal, cmd = ciOpenOrMenu(d.ciChecks(), d.ciCallbacks())
	return cmd
}

func (d *detailModel) openCICheck(c ado.CICheck) tea.Cmd {
	if !c.HasRun() {
		return statusCmd(c.Name + " hasn't run yet")
	}
	d.run = newRunView(d.client, d.prProject(), *c.Run)
	d.runName, d.runPolicy, d.runCheck, d.runStart, d.wait = c.Name, c.Policy, c, "", nil
	d.modal = nil
	return d.run.refresh()
}

// openPolicyRun opens a build policy's latest run, for a click on its check.
func (d *detailModel) openPolicyRun(p ado.Policy) tea.Cmd {
	if p.BuildID() == 0 {
		return statusCmd(buildPolicyName(p) + " hasn't run yet")
	}
	run := ado.Run{ID: p.BuildID()}
	if d.data != nil {
		for _, r := range append(append([]ado.Run(nil), d.data.PRRuns...), d.data.BranchRuns...) {
			if r.ID == p.BuildID() {
				run = r
				break
			}
		}
	}
	pc := p
	return d.openCICheck(ciCheckFromRun(buildPolicyName(p), &pc, run, p.BuildDefinitionID(), ado.CICheckPolicyBuild, p.Configuration.IsBlocking))
}

func (d *detailModel) currentRunCheck() ado.CICheck {
	if d.run == nil {
		return ado.CICheck{}
	}
	return ciCheckFromRun(d.runName, d.runPolicy, d.run.run, d.runCheck.DefinitionID, d.runCheck.Kind, d.runCheck.IsBlocking)
}

func runState(r ado.Run) string { return r.Status + "/" + r.Result }

// closeRun goes back to the Overview; a run that changed while shown
// refreshes the checks now rather than on the next tick.
func (d *detailModel) closeRun() tea.Cmd {
	changed := d.runStart != "" && runState(d.run.run) != d.runStart
	d.run, d.runPolicy, d.runCheck, d.wait = nil, nil, ado.CICheck{}, nil
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
		return d.withPRID(copyRun(d.client, d.prProject().Name, r))
	case "r":
		return d.run.refresh()
	case "Q":
		var cmd tea.Cmd
		d.modal, cmd = ciRerunModal(d.currentRunCheck(), d.ciCallbacks(), nil)
		return cmd
	case "X":
		var cmd tea.Cmd
		d.modal, cmd = ciCancelModal(d.currentRunCheck(), d.ciCallbacks(), nil)
		return cmd
	case "1", "2", "3", "4", "[", "]":
		d.run, d.runPolicy, d.runCheck, d.wait = nil, nil, ado.CICheck{}, nil
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
		d.tab.title(), "Pipeline runs",
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

func (d *detailModel) requeueKey() tea.Cmd {
	var cmd tea.Cmd
	d.modal, cmd = ciRerunKey(d.ciChecks(), d.ciCallbacks())
	return cmd
}

func (d *detailModel) cancelKey() tea.Cmd {
	var cmd tea.Cmd
	d.modal, cmd = ciCancelKey(d.ciChecks(), d.ciCallbacks())
	return cmd
}

func (d *detailModel) triggerKey() tea.Cmd {
	var cmd tea.Cmd
	d.modal, cmd = d.openTriggerPicker(nil)
	return cmd
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

func (d *detailModel) onCIAction(msg ciActionMsg) tea.Cmd {
	if msg.err != nil {
		d.status = fmt.Sprintf("error: %s %s: %s", ciActionVerb(msg.kind), msg.name, msg.err.Error())
		return nil
	}
	switch msg.kind {
	case ciActRequeue:
		return d.onRequeued(requeuedMsg{prID: msg.prID, evaluationID: msg.evaluationID, name: msg.name, err: msg.err})
	case ciActRetry:
		d.status = "retrying failed jobs of " + msg.name
		if d.run != nil && d.run.run.ID == msg.run.ID {
			return tea.Batch(d.run.refresh(), d.reload())
		}
		return d.reload()
	case ciActQueue:
		label := "queued " + msg.name
		if msg.run.ID > 0 {
			label = fmt.Sprintf("queued %s (%s)", msg.name, runNumLabel(msg.run))
			if d.run != nil {
				cmd := d.openCICheck(ciCheckFromRun(msg.name, nil, msg.run, msg.run.Definition.ID, ado.CICheckBranchRun, false))
				d.status = label
				return tea.Batch(cmd, d.reload())
			}
		}
		d.status = label
		return d.reload()
	case ciActCancel:
		d.status = fmt.Sprintf("cancelling %s (%s)", msg.name, runNumLabel(msg.run))
		if d.run != nil && d.run.run.ID == msg.run.ID {
			return tea.Batch(d.run.refresh(), d.reload())
		}
		return d.reload()
	}
	return nil
}

func (d *detailModel) waitTick() tea.Cmd {
	return waitPolicyTick(d.pr.ID, d.wait.evaluationID)
}

func (d *detailModel) onWaitTick(msg runWaitTickMsg) tea.Cmd {
	if d.wait == nil || d.wait.evaluationID != msg.evaluationID {
		return nil
	}
	return waitPolicyPoll(d.client, d.pr, msg.evaluationID)
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
		cmd := d.openCICheck(ciCheckFromRun(w.name, &p, ado.Run{ID: id}, p.BuildDefinitionID(), ado.CICheckPolicyBuild, p.Configuration.IsBlocking))
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
