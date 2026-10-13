package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func buildPolicy(t *testing.T, eval, status, name string, definition, build int, blocking bool) ado.Policy {
	t.Helper()
	var p ado.Policy
	raw := fmt.Sprintf(`{"evaluationId":%q,"status":%q,"configuration":{"isBlocking":%t,"isEnabled":true,
		"type":{"id":%q,"displayName":"Build"},
		"settings":{"buildDefinitionId":%d,"displayName":%q}},
		"context":{"buildId":%d}}`, eval, status, blocking, ado.PolicyTypeBuild, definition, name, build)
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeRun(id, definition int, name, status, result string) ado.Run {
	r := ado.Run{ID: id, BuildNumber: fmt.Sprintf("2026.%d", id), Status: status, Result: result}
	r.Definition.ID, r.Definition.Name = definition, name
	return r
}

// fakeRuns is !7 with a required build that passed, an optional one still
// running and a third that has never run.
func fakeRuns(t *testing.T) *detailModel {
	t.Helper()
	d := fakeDetail(t, 160)
	d.data.Policies = []ado.Policy{
		buildPolicy(t, "e-web", "approved", "web", 10, 101, true),
		buildPolicy(t, "e-e2e", "running", "web-e2e", 11, 102, false),
		buildPolicy(t, "e-docs", "queued", "docs", 12, 0, true),
	}
	d.rebuildOverview()
	return d
}

// prRunsAnswer is what the merge ref holds: the policies' runs, an older
// web run, and a pipeline no policy asks for.
func prRunsAnswer() prRunsMsg {
	return prRunsMsg{prID: 7, runs: []ado.Run{
		fakeRun(103, 20, "lint", "completed", "failed"),
		fakeRun(102, 11, "web-e2e", "inProgress", ""),
		fakeRun(101, 10, "web", "completed", "succeeded"),
		fakeRun(90, 10, "web", "completed", "failed"),
	}}
}

func modalText(d *detailModel) string {
	if d.modal == nil {
		return ""
	}
	return ansi.Strip(d.modal.view(d.width))
}

func TestRunsMenuListsPoliciesThenOtherPipelines(t *testing.T) {
	d := fakeRuns(t)
	press(d, "b")
	if d.status != "loading runs…" {
		t.Fatalf("status %q", d.status)
	}
	d.update(prRunsAnswer())
	text := modalText(d)
	for _, want := range []string{"web: build succeeded", "web-e2e: build", "optional", "docs: build", "lint", "not a policy", "Q re-queue"} {
		if !strings.Contains(text, want) {
			t.Errorf("menu should show %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "web: build") != 1 {
		t.Errorf("the older web run should not be listed:\n%s", text)
	}
	press(d, "j", "j") // docs has never run
	_, cmd := d.modal.update(tea.KeyMsg{Type: tea.KeyEnter})
	if s, ok := cmd().(statusMsg); d.run != nil || !ok || string(s) != "docs hasn't run yet" {
		t.Fatalf("a policy without a run can't open: run %v, status %v", d.run, s)
	}
	press(d, "j", "enter") // lint
	if d.run == nil || d.run.run.ID != 103 || d.runPolicy != nil {
		t.Fatalf("lint's run should open, got %+v", d.run)
	}
	if d.run.project.Name != "proj" {
		t.Fatalf("the run is in the pull request's project, got %q", d.run.project.Name)
	}
}

func TestTheOnlyRunOpensStraightAway(t *testing.T) {
	d := fakeDetail(t, 160)
	d.data.Policies = []ado.Policy{buildPolicy(t, "e-web", "approved", "web", 10, 101, true)}
	press(d, "b")
	d.update(prRunsMsg{prID: 7, runs: []ado.Run{fakeRun(101, 10, "web", "completed", "succeeded")}})
	if d.modal != nil || d.run == nil || d.run.run.ID != 101 {
		t.Fatalf("modal %v, run %+v", d.modal, d.run)
	}
	d.modal, d.run = nil, nil
	d.data.Policies = nil
	press(d, "b")
	d.update(prRunsMsg{prID: 7})
	if d.run != nil || d.status != "no pipeline run for this pull request yet" {
		t.Fatalf("status %q", d.status)
	}
}

func TestRunShowsInThePRAndEscComesBack(t *testing.T) {
	d := fakeRuns(t)
	d.openPolicyRun(d.data.Policies[1]) // web-e2e, running
	d.update(runLoadedMsg{runID: 102, run: fakeRun(102, 11, "web-e2e", "inProgress", ""), records: []ado.TimelineRecord{
		{ID: "s", Type: "Task", Name: "Run tests", State: "inProgress", Log: &struct {
			ID int `json:"id"`
		}{ID: 1}},
	}})
	view := ansi.Strip(d.view())
	for _, want := range []string{"run 2026.102", "web-e2e", "Steps", "Run tests"} {
		if !strings.Contains(view, want) {
			t.Errorf("the run view should show %q:\n%s", want, view)
		}
	}
	if !strings.Contains(ansi.Strip(d.help()), "re-queue") {
		t.Errorf("footer should offer Q: %s", d.help())
	}
	if d.closeRequested(tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("esc closes the run first, not the pull request")
	}
	// It finished while shown: back on the Overview, the checks refresh.
	d.update(runLoadedMsg{runID: 102, run: fakeRun(102, 11, "web-e2e", "completed", "succeeded"), records: d.run.records})
	d.loading = false
	press(d, "esc")
	if d.run != nil || d.tab != tabOverview || !d.loading {
		t.Fatalf("esc should go back to the Overview and reload: run %v, loading %v", d.run, d.loading)
	}
}

func TestEscFromAnUnchangedRunDoesNotReload(t *testing.T) {
	d := fakeRuns(t)
	d.openPolicyRun(d.data.Policies[0])
	d.update(runLoadedMsg{runID: 101, run: fakeRun(101, 10, "web", "completed", "succeeded"), records: []ado.TimelineRecord{}})
	d.loading = false
	press(d, "esc")
	if d.run != nil || d.loading {
		t.Fatalf("run %v, loading %v", d.run, d.loading)
	}
}

func TestClickOnABuildCheckOpensItsRun(t *testing.T) {
	d := fakeRuns(t)
	line := -1
	for l, p := range d.buildLines {
		if p.EvaluationID == "e-web" {
			line = l
		}
	}
	if line < 0 {
		t.Fatal("the web check has no line")
	}
	if got := ansi.Strip(strings.Split(d.renderOverview(), "\n")[line]); !strings.Contains(got, "web: build succeeded") {
		t.Fatalf("line %d is %q", line, got)
	}
	d.onMouse(tea.MouseMsg{X: 5, Y: d.bodyTop() + 1 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if d.run == nil || d.run.run.ID != 101 {
		t.Fatalf("got %+v", d.run)
	}
}

func TestRequeueAsksAndLeavesARunningPolicyAlone(t *testing.T) {
	var got []string
	defer func(f func(context.Context, *ado.Client, ado.PullRequest, string) error) { requeuePolicy = f }(requeuePolicy)
	requeuePolicy = func(_ context.Context, _ *ado.Client, pr ado.PullRequest, id string) error {
		got = append(got, fmt.Sprintf("!%d %s", pr.ID, id))
		return nil
	}
	d := fakeRuns(t)
	press(d, "Q")
	if !strings.Contains(modalText(d), "Re-queue") {
		t.Fatalf("several build policies: a menu\n%s", modalText(d))
	}
	press(d, "j", "enter") // web-e2e is running
	if !strings.Contains(modalText(d), "Re-queue") {
		t.Fatalf("a running policy is refused, back on the menu\n%s", modalText(d))
	}
	press(d, "k", "enter")
	if !strings.Contains(modalText(d), "Re-queue web?") {
		t.Fatalf("should ask: %s", modalText(d))
	}
	press(d, "n")
	if !strings.Contains(modalText(d), "web-e2e") {
		t.Fatalf("n goes back to the menu: %s", modalText(d))
	}
	press(d, "enter")
	var cmd tea.Cmd
	d.modal, cmd = d.modal.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	msg := cmd()
	d.loading = false
	d.update(msg)
	if strings.Join(got, ",") != "!7 e-web" || d.status != "re-queued web" || !d.loading {
		t.Fatalf("requeued %v, status %q, loading %v", got, d.status, d.loading)
	}
}

func TestRequeueFromARunSwitchesToTheNewRun(t *testing.T) {
	d := fakeRuns(t)
	d.openPolicyRun(d.data.Policies[0]) // web, run 101
	press(d, "Q")
	if !strings.Contains(modalText(d), "Re-queue web?") {
		t.Fatalf("Q in a run asks for its policy: %s", modalText(d))
	}
	d.modal = nil
	d.update(requeuedMsg{prID: 7, evaluationID: "e-web", name: "web"})
	if d.wait == nil || !strings.Contains(d.status, "waiting for its run") {
		t.Fatalf("wait %+v, status %q", d.wait, d.status)
	}
	// Not yet: the policy still names the old run.
	d.update(runWaitMsg{prID: 7, evaluationID: "e-web", policy: buildPolicy(t, "e-web", "queued", "web", 10, 101, true)})
	if d.run.run.ID != 101 || d.wait == nil {
		t.Fatal("should keep waiting")
	}
	d.update(runWaitMsg{prID: 7, evaluationID: "e-web", policy: buildPolicy(t, "e-web", "running", "web", 10, 104, true)})
	if d.run == nil || d.run.run.ID != 104 || d.wait != nil || d.runPolicy.BuildID() != 104 {
		t.Fatalf("should switch to run 104: %+v", d.run)
	}
	if !strings.Contains(d.status, "new run #104") {
		t.Fatalf("status %q", d.status)
	}
}

func TestRequeueWaitGivesUp(t *testing.T) {
	d := fakeRuns(t)
	d.openPolicyRun(d.data.Policies[0])
	d.update(requeuedMsg{prID: 7, evaluationID: "e-web", name: "web"})
	d.wait.until = time.Now().Add(-time.Second)
	d.update(runWaitMsg{prID: 7, evaluationID: "e-web", policy: d.data.Policies[0]})
	if d.wait != nil || !strings.Contains(d.status, "no new run of web yet") {
		t.Fatalf("wait %+v, status %q", d.wait, d.status)
	}
}

func TestOtherPipelinesCantBeRequeued(t *testing.T) {
	d := fakeRuns(t)
	press(d, "b")
	d.update(prRunsAnswer())
	_, cmd := d.modal.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	_ = cmd
	d.modal, cmd = d.modal.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	if s, ok := cmd().(statusMsg); !ok || !strings.Contains(string(s), "isn't a build policy") {
		t.Fatalf("got %v", s)
	}
}

func TestRunMessagesReachTheRunOpenFromAPR(t *testing.T) {
	m := dashboardWith(t, 7)
	m.detail = fakeRuns(t)
	m.detail.openPolicyRun(m.detail.data.Policies[0])
	next, _ := m.Update(runLoadedMsg{runID: 101, run: fakeRun(101, 10, "web", "completed", "succeeded"), records: []ado.TimelineRecord{}})
	if got := next.(Model).detail.run.run.BuildNumber; got != "2026.101" {
		t.Fatalf("the run didn't load: %q", got)
	}
}
