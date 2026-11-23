package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func dashCIModel(t *testing.T, width, height int, prs []ado.PullRequest, builds map[int]buildResult) Model {
	t.Helper()
	me := ado.Identity{ID: "me", DisplayName: "Me"}
	m := New(ado.NewClient("org"), time.Minute)
	m.me = me
	m.loading = false
	m.sections = ado.Classify("me", prs, nil)
	if builds != nil {
		m.builds = builds
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = next.(Model)
	return m
}

func makeDashPR(id int, title string, draft bool) ado.PullRequest {
	return ado.PullRequest{
		ID:            id,
		Title:         title,
		IsDraft:       draft,
		CreatedBy:     ado.Identity{ID: "author", DisplayName: "Sam Rivera"},
		SourceRefName: fmt.Sprintf("refs/heads/feat/%d", id),
		TargetRefName: "refs/heads/main",
		Repository:    ado.Repository{ID: "r1", Name: "app", Project: ado.Project{ID: "p1", Name: "Proj"}},
		Reviewers:     []ado.Reviewer{{Identity: ado.Identity{ID: "me"}}},
	}
}

func TestDashboardStatusAndCIRendering(t *testing.T) {
	prs := []ado.PullRequest{
		makeDashPR(101, "pr failed", false),
		makeDashPR(102, "pr running", false),
		makeDashPR(103, "pr queued", true),
		makeDashPR(104, "pr partial", false),
		makeDashPR(105, "pr single pass", false),
		makeDashPR(106, "pr multi pass", false),
		makeDashPR(107, "pr multi err", false),
		makeDashPR(108, "pr none", false),
	}
	builds := map[int]buildResult{
		101: {summary: ado.CISummary{State: ado.BuildFailed, Failed: 1, Checks: []ado.CICheck{{Name: "build", State: ado.BuildFailed, StatusText: "failed"}}}},
		102: {summary: ado.CISummary{State: ado.BuildRunning, Running: 1, Checks: []ado.CICheck{{Name: "test", State: ado.BuildRunning, StatusText: "in progress"}}}},
		103: {summary: ado.CISummary{State: ado.BuildQueued, Queued: 1, Checks: []ado.CICheck{{Name: "queue", State: ado.BuildQueued, StatusText: "queued"}}}},
		104: {summary: ado.CISummary{State: ado.BuildPartial, Partial: 1, Checks: []ado.CICheck{{Name: "warn", State: ado.BuildPartial, StatusText: "partially succeeded"}}}},
		105: {summary: ado.CISummary{State: ado.BuildPassed, Passed: 1, Checks: []ado.CICheck{{Name: "ok", State: ado.BuildPassed, StatusText: "succeeded"}}}},
		106: {summary: ado.CISummary{State: ado.BuildPassed, Passed: 2, Checks: []ado.CICheck{
			{Name: "a", State: ado.BuildPassed, StatusText: "succeeded"},
			{Name: "b", State: ado.BuildPassed, StatusText: "succeeded"},
		}}},
		107: {summary: ado.CISummary{State: ado.BuildFailed, Failed: 1, Passed: 1, Checks: []ado.CICheck{
			{Name: "a", State: ado.BuildPassed, StatusText: "succeeded"},
			{Name: "b", State: ado.BuildFailed, StatusText: "failed"},
		}}},
		108: {summary: ado.CISummary{State: ado.BuildNone}},
	}

	m := dashCIModel(t, 160, 20, prs, builds)
	view := ansi.Strip(m.View())
	for _, want := range []string{"Active", "Draft", "× err", "● run", "○ queue", "! warn", "✓ pass", "✓ 2/2", "× 1 err"} {
		if !strings.Contains(view, want) {
			t.Errorf("wide dashboard should contain %q:\n%s", want, view)
		}
	}

	narrow := dashCIModel(t, 95, 20, prs[:3], builds)
	narrowView := ansi.Strip(narrow.View())
	for _, want := range []string{"ACT", "DRF"} {
		if !strings.Contains(narrowView, want) {
			t.Errorf("narrow dashboard should contain %q:\n%s", want, narrowView)
		}
	}
	for _, unwanted := range []string{"× err", "● run", "○ queue"} {
		if strings.Contains(narrowView, unwanted) {
			t.Errorf("narrow dashboard should shrink CI column to single glyph, found %q:\n%s", unwanted, narrowView)
		}
	}
}

func TestDashboardCISummaryBarUpdatesAndClickOpensRuns(t *testing.T) {
	prs := []ado.PullRequest{
		makeDashPR(10, "first pr", false),
		makeDashPR(20, "second pr", true),
	}
	builds := map[int]buildResult{
		10: {summary: ado.CISummary{State: ado.BuildRunning, Running: 1, Passed: 1, Checks: []ado.CICheck{
			{Name: "build-api", State: ado.BuildPassed, StatusText: "succeeded", Run: &ado.Run{ID: 1}},
			{Name: "deploy-dev", State: ado.BuildRunning, StatusText: "in progress", Run: &ado.Run{ID: 2, Status: "inProgress"}},
		}}},
		20: {summary: ado.CISummary{State: ado.BuildFailed, Failed: 1, Checks: []ado.CICheck{
			{Name: "e2e-suite", State: ado.BuildFailed, StatusText: "failed", Run: &ado.Run{ID: 3, Status: "completed", Result: "failed"}},
		}}},
	}
	m := dashCIModel(t, 160, 14, prs, builds)
	m, _ = sendKeys(m, "j") // move onto PR !10
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "CI !10: ✓ build-api · ● deploy-dev") {
		t.Fatalf("CI summary bar missing !10 checks:\n%s", view)
	}
	// Click on the CI summary bar row (1 + prListHeight) -> opens menu for !10 (2 checks).
	_, y := findRow(t, m.View(), "CI !10:")
	next, _ := m.Update(click(5, y))
	if _, ok := next.(Model).modal.(*menuModal); !ok {
		t.Fatalf("clicking CI bar should open runs menu, got %T", next.(Model).modal)
	}

	m, _ = sendKeys(m, "j", "j", "j") // move onto PR !20 (in Assigned to me)
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "CI !20: × e2e-suite") {
		t.Fatalf("CI summary bar should update to !20:\n%s", view)
	}
}

func TestDashboardFilterMatchesStatusCIAndPipelineNames(t *testing.T) {
	prs := []ado.PullRequest{
		makeDashPR(1, "alpha", false),
		makeDashPR(2, "beta", true),
		makeDashPR(3, "gamma", false),
	}
	builds := map[int]buildResult{
		1: {summary: ado.CISummary{State: ado.BuildPassed, Passed: 1, Checks: []ado.CICheck{{Name: "api-ci", State: ado.BuildPassed, StatusText: "succeeded"}}}},
		2: {summary: ado.CISummary{State: ado.BuildRunning, Running: 1, Checks: []ado.CICheck{{Name: "web-e2e", State: ado.BuildRunning, StatusText: "in progress"}}}},
		3: {summary: ado.CISummary{State: ado.BuildFailed, Failed: 1, Checks: []ado.CICheck{{Name: "mobile-lint", State: ado.BuildFailed, StatusText: "failed"}}}},
	}
	cases := []struct {
		query string
		want  int
	}{
		{"draft", 2},
		{"running", 2},
		{"err", 3},
		{"mobile-lint", 3},
		{"api-ci", 1},
	}
	for _, tc := range cases {
		m := dashCIModel(t, 160, 20, prs, builds)
		m.filter = tc.query
		var got []int
		for _, r := range m.rows() {
			if r.pr != nil {
				got = append(got, r.pr.ID)
			}
		}
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("filter %q matched PRs %v, want [%d]", tc.query, got, tc.want)
		}
	}
}

func TestDashboardBKeyOpensRunAndEscReturnsToDashboard(t *testing.T) {
	prs := []ado.PullRequest{
		makeDashPR(10, "single run", false),
		makeDashPR(20, "multi runs", false),
		makeDashPR(30, "no runs", false),
	}
	r10 := fakeRun(501, 11, "ci", "completed", "succeeded")
	r20a := fakeRun(502, 12, "build", "inProgress", "")
	r20b := fakeRun(503, 13, "lint", "completed", "failed")
	builds := map[int]buildResult{
		10: {summary: ado.CISummary{State: ado.BuildPassed, Passed: 1, Checks: []ado.CICheck{{Kind: ado.CICheckPRRun, Name: "ci", State: ado.BuildPassed, StatusText: "succeeded", Run: &r10, DefinitionID: 11}}}},
		20: {summary: ado.CISummary{State: ado.BuildRunning, Running: 1, Failed: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPRRun, Name: "build", State: ado.BuildRunning, StatusText: "in progress", Run: &r20a, DefinitionID: 12},
			{Kind: ado.CICheckPRRun, Name: "lint", State: ado.BuildFailed, StatusText: "failed", Run: &r20b, DefinitionID: 13},
		}}},
		30: {summary: ado.CISummary{State: ado.BuildNone}},
	}
	m := dashCIModel(t, 160, 20, prs, builds)

	// 1. Single run opens m.dashRun directly (not m.detail), esc returns to dashboard.
	m, _ = sendKeys(m, "j", "b")
	if m.detail != nil || m.dashRun == nil || m.dashRun.run.run.ID != 501 {
		t.Fatalf("expected direct dashRun 501 without detail, detail=%v dashRun=%+v", m.detail, m.dashRun)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "!10 single run") || !strings.Contains(view, "Pull requests") {
		t.Fatalf("dashRun view missing expected header:\n%s", view)
	}
	m, _ = sendKeys(m, "esc")
	if m.dashRun != nil || m.detail != nil {
		t.Fatalf("esc should return directly to dashboard, dashRun=%v detail=%v", m.dashRun, m.detail)
	}

	// 2. Multiple runs open menu with Q/X/o/t hints.
	m, _ = sendKeys(m, "j", "b")
	menu, ok := m.modal.(*menuModal)
	if !ok || !strings.Contains(menu.hint, "Q re-queue") || !strings.Contains(menu.hint, "X cancel") {
		t.Fatalf("expected runs menu on multi-run PR, got %T", m.modal)
	}
	m, _ = sendKeys(m, "esc")

	// 3. Zero runs prompts to trigger a pipeline.
	m, _ = sendKeys(m, "j", "b")
	cf, ok := m.modal.(*confirmModal)
	if !ok || !strings.Contains(cf.prompt, "Trigger a pipeline?") {
		t.Fatalf("expected trigger confirm on 0-run PR, got %T", m.modal)
	}
}

func TestDashboardQAndXAndTCommands(t *testing.T) {
	savedReq, savedRetry, savedQueue, savedCancel, savedList := requeuePolicy, retryRun, queueRun, cancelRun, listPipelines
	defer func() {
		requeuePolicy, retryRun, queueRun, cancelRun, listPipelines = savedReq, savedRetry, savedQueue, savedCancel, savedList
	}()

	var requeued []string
	var retried []int
	var queued []string
	var cancelled []int
	requeuePolicy = func(_ context.Context, _ *ado.Client, pr ado.PullRequest, evalID string) error {
		requeued = append(requeued, fmt.Sprintf("!%d:%s", pr.ID, evalID))
		return nil
	}
	retryRun = func(_ context.Context, _ *ado.Client, _ string, runID int) error {
		retried = append(retried, runID)
		return nil
	}
	queueRun = func(_ context.Context, _ *ado.Client, _ string, defID int, branch string) (ado.Run, error) {
		queued = append(queued, fmt.Sprintf("%d:%s", defID, branch))
		return fakeRun(999, defID, "triggered", "notStarted", ""), nil
	}
	cancelRun = func(_ context.Context, _ *ado.Client, _ string, runID int) error {
		cancelled = append(cancelled, runID)
		return nil
	}
	listPipelines = func(_ context.Context, _ *ado.Client, _ string) ([]ado.Pipeline, error) {
		return []ado.Pipeline{{ID: 44, Name: "qa-smoke"}, {ID: 45, Name: "release"}}, nil
	}

	polApproved := ado.Policy{EvaluationID: "eval-1"}
	polApproved.Status = "approved"
	polApproved.Configuration.IsEnabled = true
	polApproved.Configuration.IsBlocking = true
	polApproved.Configuration.Type.ID = ado.PolicyTypeBuild
	polApproved.Configuration.Settings = map[string]any{"buildDefinitionId": float64(10), "displayName": "pol-build"}

	runFail := fakeRun(601, 21, "unit-fail", "completed", "failed")
	runPass := fakeRun(602, 22, "unit-pass", "completed", "succeeded")
	runActive1 := fakeRun(603, 23, "active-1", "inProgress", "")
	runActive2 := fakeRun(604, 24, "active-2", "notStarted", "")

	prs := []ado.PullRequest{
		makeDashPR(1, "policy pr", false),
		makeDashPR(2, "failed non-policy pr", false),
		makeDashPR(3, "passed non-policy pr", false),
		makeDashPR(4, "multi active pr", false),
	}
	builds := map[int]buildResult{
		1: {summary: ado.CISummary{State: ado.BuildPassed, Passed: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPolicyBuild, Name: "pol-build", State: ado.BuildPassed, StatusText: "succeeded", Policy: &polApproved, DefinitionID: 10},
		}}},
		2: {summary: ado.CISummary{State: ado.BuildFailed, Failed: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPRRun, Name: "unit-fail", State: ado.BuildFailed, StatusText: "failed", Run: &runFail, DefinitionID: 21},
		}}},
		3: {summary: ado.CISummary{State: ado.BuildPassed, Passed: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckBranchRun, Name: "unit-pass", State: ado.BuildPassed, StatusText: "succeeded", Run: &runPass, DefinitionID: 22},
		}}},
		4: {summary: ado.CISummary{State: ado.BuildRunning, Running: 1, Queued: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPRRun, Name: "active-1", State: ado.BuildRunning, StatusText: "in progress", Run: &runActive1, DefinitionID: 23},
			{Kind: ado.CICheckBranchRun, Name: "active-2", State: ado.BuildQueued, StatusText: "queued", Run: &runActive2, DefinitionID: 24},
		}}},
	}

	m := dashCIModel(t, 160, 20, prs, builds)

	// PR 1: Q re-queues build policy.
	m, _ = sendKeys(m, "j", "Q")
	if !strings.Contains(ansi.Strip(m.modal.view(m.width)), "Re-queue pol-build?") {
		t.Fatalf("expected Re-queue confirm, got %T", m.modal)
	}
	var cmd tea.Cmd
	m, cmd = sendKeys(m, "y")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if strings.Join(requeued, ",") != "!1:eval-1" {
		t.Fatalf("requeued = %v, want [!1:eval-1]", requeued)
	}

	// PR 2: Q offers Retry failed jobs vs Run new.
	m, _ = sendKeys(m, "j", "Q")
	if view := ansi.Strip(m.modal.view(m.width)); !strings.Contains(view, "Retry failed jobs") || !strings.Contains(view, "Run new") {
		t.Fatalf("expected Retry vs Run new menu:\n%s", view)
	}
	m, _ = sendKeys(m, "enter")
	m, cmd = sendKeys(m, "y")
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(retried) != 1 || retried[0] != 601 {
		t.Fatalf("retried = %v, want [601]", retried)
	}

	// PR 3: Q confirms Run new directly; X reports none running.
	m, _ = sendKeys(m, "j", "Q")
	if view := ansi.Strip(m.modal.view(m.width)); !strings.Contains(view, "Queue a new run of unit-pass on feat/3?") {
		t.Fatalf("expected queue new run confirm:\n%s", view)
	}
	m, cmd = sendKeys(m, "y")
	next, _ = m.Update(cmd())
	m = next.(Model)
	if strings.Join(queued, ",") != "22:refs/heads/feat/3" {
		t.Fatalf("queued = %v, want [22:refs/heads/feat/3]", queued)
	}
	m, cmd = sendKeys(m, "X")
	if s, ok := cmd().(statusMsg); !ok || string(s) != "no running pipeline to cancel" {
		t.Fatalf("expected no running pipeline status, got %#v", cmd())
	}

	// PR 4: Q refuses active run if picked; X opens menu for 2 active runs and cancels with No default.
	m, _ = sendKeys(m, "j", "Q")
	_, cmd = sendKeys(m, "enter")
	if s, ok := cmd().(statusMsg); !ok || !strings.Contains(string(s), "already running") {
		t.Fatalf("expected already running refusal, got %#v", cmd())
	}
	m.modal = nil
	m, _ = sendKeys(m, "X")
	if _, ok := m.modal.(*menuModal); !ok {
		t.Fatalf("expected Cancel menu for multiple active runs, got %T", m.modal)
	}
	m, _ = sendKeys(m, "j", "enter") // active-2 (604)
	cf, ok := m.modal.(*confirmModal)
	if !ok || cf.yes || !strings.Contains(cf.prompt, "Cancel active-2") {
		t.Fatalf("expected cancel confirm with No focused, got %+v", m.modal)
	}
	m, cmd = sendKeys(m, "y")
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(cancelled) != 1 || cancelled[0] != 604 {
		t.Fatalf("cancelled = %v, want [604]", cancelled)
	}

	// t: pipelinePicker lets user type 'q' without quitting, filters, and queues on source branch.
	m, _ = sendKeys(m, "t")
	if !m.inTextInput() {
		t.Fatal("pipelinePicker must report inTextInput() == true while typing")
	}
	next, _ = m.Update(prPipelinesMsg{from: m.client, prID: 4, pipelines: []ado.Pipeline{{ID: 44, Name: "qa-smoke"}, {ID: 45, Name: "release"}}})
	m = next.(Model)
	m, qCmd := sendKeys(m, "q")
	if isQuit(qCmd) {
		t.Fatal("typing 'q' inside pipelinePicker must filter, not quit")
	}
	m, _ = sendKeys(m, "enter")
	if view := ansi.Strip(m.modal.view(m.width)); !strings.Contains(view, "Run qa-smoke on feat/4?") {
		t.Fatalf("expected trigger confirm:\n%s", view)
	}
	m, cmd = sendKeys(m, "y")
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(queued) != 2 || queued[1] != "44:refs/heads/feat/4" {
		t.Fatalf("queued = %v, want 44:refs/heads/feat/4", queued)
	}
}

func TestAdaptiveCIPollingLifecycle(t *testing.T) {
	prs := []ado.PullRequest{
		makeDashPR(1, "manual queued", false),
		makeDashPR(2, "active run", false),
	}
	m := dashCIModel(t, 140, 20, prs, map[int]buildResult{})

	// 1. Unstarted manual queued policy (BuildID == 0) must NOT trigger 10s polling.
	manualPolicy := ado.Policy{EvaluationID: "e-man", Status: "queued"}
	manualPolicy.Configuration.IsEnabled = true
	manualPolicy.Configuration.IsBlocking = true
	manualPolicy.Configuration.Type.ID = ado.PolicyTypeBuild
	sumManual := ado.SummarizeCI([]ado.Policy{manualPolicy}, nil, nil, nil)
	next, cmd := m.Update(buildMsg{from: m.client, id: 1, result: buildResult{summary: sumManual}})
	m = next.(Model)
	if m.ciPolling || cmd != nil {
		t.Fatalf("manual queued policy must not start polling (ciPolling=%v, cmd=%v)", m.ciPolling, cmd != nil)
	}

	// 2. Active run starts polling once.
	activeRun := fakeRun(701, 10, "ci", "inProgress", "")
	sumActive := ado.SummarizeCI(nil, []ado.Run{activeRun}, nil, nil)
	next, cmd = m.Update(buildMsg{from: m.client, id: 2, result: buildResult{summary: sumActive}})
	m = next.(Model)
	if !m.ciPolling || cmd == nil {
		t.Fatalf("active run should start polling (ciPolling=%v, cmd=%v)", m.ciPolling, cmd != nil)
	}
	gen := m.ciPollGen

	// 3. Stale generation tick is ignored.
	_, staleCmd := m.Update(ciPollTickMsg{from: m.client, gen: gen - 1})
	if staleCmd != nil {
		t.Fatal("stale ciPollTickMsg generation should be dropped")
	}

	// 4. Current generation tick returns commands; after run finishes, next tick stops polling.
	targets := m.ciPollTargets(time.Now())
	if len(targets) != 1 || targets[0].ID != 2 {
		t.Fatalf("expected only PR 2 in poll targets, got %+v", targets)
	}
	doneRun := fakeRun(701, 10, "ci", "completed", "succeeded")
	m.builds[2] = buildResult{summary: ado.SummarizeCI(nil, []ado.Run{doneRun}, nil, nil)}
	next, stopCmd := m.Update(ciPollTickMsg{from: m.client, gen: gen})
	m = next.(Model)
	if m.ciPolling || stopCmd != nil {
		t.Fatalf("polling should stop when no PR has active runs (ciPolling=%v)", m.ciPolling)
	}

	// 5. ciActionMsg adds PR to watchCI so polling runs even before buildMsg has an active run.
	next, watchCmd := m.Update(ciActionMsg{from: m.client, prID: 1, kind: ciActQueue, name: "qa", run: fakeRun(800, 9, "qa", "notStarted", "")})
	m = next.(Model)
	if !m.ciPolling || watchCmd == nil {
		t.Fatal("ciActionMsg should watch PR and start polling")
	}
	if _, watched := m.watchCI[1]; !watched {
		t.Fatal("expected PR 1 in watchCI")
	}
}

func TestDashboardPaletteCIGroupAndLocalExecution(t *testing.T) {
	prs := []ado.PullRequest{makeDashPR(7, "feat: ci palette", false)}
	r := fakeRun(901, 30, "api-ci", "inProgress", "")
	m := dashCIModel(t, 140, 20, prs, map[int]buildResult{
		7: {summary: ado.CISummary{
			State:   ado.BuildRunning,
			Running: 1,
			Checks: []ado.CICheck{{
				Kind:         ado.CICheckPRRun,
				Name:         "api-ci",
				State:        ado.BuildRunning,
				StatusText:   "in progress",
				Run:          &r,
				DefinitionID: 30,
			}},
		}},
	})
	m = onPR(t, m)
	m, _ = sendKeys(m, ":")
	titles := groupTitles(openedPalette(t, m))
	hasCI := false
	for _, title := range titles {
		if title == "CI/CD" {
			hasCI = true
		}
	}
	if !hasCI {
		t.Fatalf("expected CI/CD group in dashboard palette when checks exist: %v", titles)
	}

	// Running "Pipeline runs" from dashboard palette opens m.dashRun locally (not m.detail).
	m = typeText(m, "pipeline runs")
	m, cmd := sendKeys(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.detail != nil || m.dashRun == nil || m.dashRun.run.run.ID != 901 {
		t.Fatalf("dashboard palette 'Pipeline runs' should open dashRun locally, detail=%v dashRun=%+v", m.detail, m.dashRun)
	}
}
