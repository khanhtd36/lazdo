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
	for _, want := range []string{"● pr failed", "○ pr queued", "× err", "● run", "○ queue", "! warn", "✓ pass", "✓ 2/2", "× 1 err"} {
		if !strings.Contains(view, want) {
			t.Errorf("wide dashboard should contain %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"Active", "Draft", "ACT", "DRF"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("wide dashboard should not contain removed status column %q:\n%s", unwanted, view)
		}
	}

	narrow := dashCIModel(t, 95, 20, prs[:3], builds)
	narrowView := ansi.Strip(narrow.View())
	for _, want := range []string{"● pr failed", "○ pr queued"} {
		if !strings.Contains(narrowView, want) {
			t.Errorf("narrow dashboard should contain %q:\n%s", want, narrowView)
		}
	}
	for _, unwanted := range []string{"Active", "Draft", "ACT", "DRF", "× err", "● run", "○ queue"} {
		if strings.Contains(narrowView, unwanted) {
			t.Errorf("narrow dashboard should omit %q:\n%s", unwanted, narrowView)
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

func TestDetailAllTabsCIKeysAndStandaloneGuard(t *testing.T) {
	savedList := listPipelines
	defer func() { listPipelines = savedList }()
	listPipelines = func(_ context.Context, _ *ado.Client, _ string) ([]ado.Pipeline, error) {
		return []ado.Pipeline{{ID: 1, Name: "ci"}}, nil
	}

	rRun := fakeRun(501, 11, "api-ci", "inProgress", "")
	rFail := fakeRun(502, 12, "lint", "completed", "failed")

	for _, tab := range []detailTab{tabFiles, tabCommits, tabConflicts} {
		d := fakeFiles(t, 160)
		d.data.PRRuns = []ado.Run{rRun}
		d.data.BranchRuns = []ado.Run{rFail}
		d.tab = tab
		if tab == tabFiles {
			d.files.pane = paneDiff
		}

		// b loads runs and opens menu from any tab (not blocked by tab != tabOverview).
		press(d, "b")
		if d.status != "loading runs…" {
			t.Fatalf("tab %v: b should start loading runs, status=%q", tab, d.status)
		}
		d.update(prRunsMsg{prID: d.pr.ID, runs: d.data.PRRuns, branchRuns: d.data.BranchRuns})
		if _, ok := d.modal.(*menuModal); !ok {
			t.Fatalf("tab %v: b should open runs menu, got %T", tab, d.modal)
		}
		press(d, "esc")

		// Q opens Re-run / Re-queue menu from any tab.
		press(d, "Q")
		if _, ok := d.modal.(*menuModal); !ok {
			t.Fatalf("tab %v: Q should open rerun menu, got %T", tab, d.modal)
		}
		press(d, "esc")

		// X opens Cancel confirm from any tab.
		press(d, "X")
		if cf, ok := d.modal.(*confirmModal); !ok || !strings.Contains(cf.prompt, "Cancel api-ci") {
			t.Fatalf("tab %v: X should open cancel confirm, got %T", tab, d.modal)
		}
		press(d, "esc")

		// t opens pipelinePicker from any tab.
		press(d, "t")
		if _, ok := d.modal.(*pipelinePicker); !ok {
			t.Fatalf("tab %v: t should open pipelinePicker, got %T", tab, d.modal)
		}
		press(d, "esc")
	}

	// Typing in a tab filter must NOT trigger b/Q/X/t.
	dFilter := fakeDetail(t, 160)
	dFilter.tab = tabCommits
	dFilter.data.PRRuns = []ado.Run{rRun}
	press(dFilter, "/", "b", "Q", "X", "t")
	if dFilter.modal != nil || dFilter.lists[tabCommits].filter != "bQXt" {
		t.Fatalf("typing in Commits filter triggered modal=%T filter=%q", dFilter.modal, dFilter.lists[tabCommits].filter)
	}

	// Standalone repo diff ignores b/Q/X/t.
	repo := ado.Repo{ID: "r1", Name: "repo", Project: ado.Project{ID: "p1", Name: "Proj"}}
	standalone, _ := newRepoDiff(ado.NewClient("org"), repo, "commit", "", "abc", 160, 30)
	press(standalone, "b", "Q", "X", "t")
	if standalone.modal != nil || standalone.run != nil || standalone.status != "" {
		t.Fatalf("standalone diff should ignore CI keys: modal=%v run=%v status=%q", standalone.modal, standalone.run, standalone.status)
	}
}

func TestDetailOverviewSidebarCICDAndClick(t *testing.T) {
	// 1. Hidden when PR has no CI/CD checks.
	dEmpty := fakeDetail(t, 160)
	emptyPlain := ansi.Strip(dEmpty.view())
	if strings.Contains(emptyPlain, "CI/CD") {
		t.Fatalf("Overview sidebar should hide CI/CD box when there are no checks:\n%s", emptyPlain)
	}
	for _, want := range []string{"Reviewers", "Tags", "Work items"} {
		if !strings.Contains(emptyPlain, want) {
			t.Fatalf("Overview sidebar missing %q when CI/CD is hidden:\n%s", want, emptyPlain)
		}
	}

	setupDetail := func(width int) *detailModel {
		d := fakeDetail(t, width)
		d.data.Policies = []ado.Policy{
			buildPolicy(t, "e-web", "approved", "web", 10, 101, true),
		}
		d.data.PRRuns = []ado.Run{
			fakeRun(101, 10, "web", "completed", "succeeded"), // covered by policy
			fakeRun(601, 20, "pr-lint", "completed", "failed"),
		}
		d.data.BranchRuns = []ado.Run{
			fakeRun(602, 30, "branch-deploy", "inProgress", ""),
		}
		d.data.Statuses = []ado.Status{
			{ID: 1, State: "succeeded", Description: "github-ci", TargetURL: "https://example.com/ci/1"},
		}
		d.rebuildOverview()
		return d
	}

	findSideRow := func(view string, overviewW, totalW int, text string) int {
		t.Helper()
		for y, ln := range strings.Split(view, "\n") {
			if strings.Contains(ansi.Strip(ansi.Cut(ln, overviewW, totalW)), text) {
				return y
			}
		}
		t.Fatalf("%q not found in right sidebar (x >= %d):\n%s", text, overviewW, ansi.Strip(view))
		return -1
	}

	// 2. Wide side-by-side widths (120, 180): Reviewers -> CI/CD (4) -> Tags -> Work items,
	// no duplicate "Pipeline runs" in the left Overview checks, and every line fits width.
	for _, width := range []int{120, 180} {
		d := setupDetail(width)
		view := d.view()
		plain := ansi.Strip(view)
		for _, want := range []string{"CI/CD (4)", "web", "pr-lint", "branch-deploy", "github-ci", "PR trigger", "source branch"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("width %d: Overview sidebar missing %q:\n%s", width, want, plain)
			}
		}
		if strings.Contains(plain, "Pipeline runs") {
			t.Fatalf("width %d: left Overview should not duplicate Pipeline runs section:\n%s", width, plain)
		}
		revY := findSideRow(view, d.overviewWidth(), width, "Reviewers")
		ciY := findSideRow(view, d.overviewWidth(), width, "CI/CD (4)")
		tagY := findSideRow(view, d.overviewWidth(), width, "Tags")
		wiY := findSideRow(view, d.overviewWidth(), width, "Work items")
		if !(revY < ciY && ciY < tagY && tagY < wiY) {
			t.Fatalf("width %d: wrong sidebar order: Reviewers=%d CI/CD=%d Tags=%d WorkItems=%d", width, revY, ciY, tagY, wiY)
		}
		for _, line := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line exceeds width (%d): %q", width, w, ansi.Strip(line))
			}
		}
	}

	// 3. Mouse interactions on wide right sidebar.
	d := setupDetail(160)
	view := d.view()
	sideX := d.overviewWidth() + 3

	// Clicking line 1 and line 2 of pr-lint opens run 601.
	prLintY := findSideRow(view, d.overviewWidth(), 160, "pr-lint")
	for offset, label := range []string{"line 1", "line 2"} {
		d.run, d.modal, d.status = nil, nil, ""
		d.onMouse(click(sideX, prLintY+offset))
		if d.run == nil || d.run.run.ID != 601 {
			t.Fatalf("clicking %s of sidebar pr-lint should open run 601, got %+v", label, d.run)
		}
	}
	d.run, d.modal, d.status = nil, nil, ""

	// Clicking CI/CD (4) header triggers loadRuns (b).
	ciHeaderY := findSideRow(view, d.overviewWidth(), 160, "CI/CD (4)")
	d.onMouse(click(sideX, ciHeaderY))
	if d.status != "loading runs…" {
		t.Fatalf("clicking CI/CD header should trigger loadRuns, status=%q", d.status)
	}
	d.run, d.modal, d.status = nil, nil, ""

	// Clicking Reviewers still opens reviewersModal.
	revY := findSideRow(view, d.overviewWidth(), 160, "Reviewers")
	d.onMouse(click(sideX, revY+1))
	if _, ok := d.modal.(*reviewersModal); !ok {
		t.Fatalf("clicking Reviewers pane should open reviewersModal, got %T", d.modal)
	}
	d.modal, d.status = nil, ""

	// Clicking external check returns openURL cmd instead of "hasn't run yet".
	ghY := findSideRow(view, d.overviewWidth(), 160, "github-ci")
	extCmd := d.onMouse(click(sideX, ghY))
	if d.modal != nil || extCmd == nil || strings.Contains(d.status, "hasn't run") {
		t.Fatalf("clicking external CI check with URL should return openURL cmd (cmd=%v modal=%T status=%q)", extCmd != nil, d.modal, d.status)
	}

	// 4. Narrow stacked mode (width 80): renders CI/CD (4) and d.runLines click opens run 601.
	dNarrow := setupDetail(80)
	narrowView := dNarrow.view()
	narrowPlain := ansi.Strip(narrowView)
	for _, want := range []string{"CI/CD (4)", "pr-lint", "branch-deploy", "github-ci"} {
		if !strings.Contains(narrowPlain, want) {
			t.Fatalf("narrow mode missing %q:\n%s", want, narrowPlain)
		}
	}
	for _, line := range strings.Split(narrowView, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("narrow mode line exceeds width 80 (%d): %q", w, ansi.Strip(line))
		}
	}
	lineIdx := -1
	for l, c := range dNarrow.runLines {
		if c.Name == "pr-lint" {
			lineIdx = l
			break
		}
	}
	if lineIdx < 0 {
		t.Fatal("pr-lint missing from narrow dNarrow.runLines")
	}
	dNarrow.onMouse(click(5, dNarrow.bodyTop()+1+lineIdx))
	if dNarrow.run == nil || dNarrow.run.run.ID != 601 {
		t.Fatalf("clicking narrow CI/CD sidebar line should open run 601, got %+v", dNarrow.run)
	}
}

func projectPRsModel(t *testing.T, width, height int, prs []ado.PullRequest, builds map[int]buildResult) Model {
	t.Helper()
	m := dashCIModel(t, width, height, nil, nil)
	proj := ado.ProjectInfo{ID: "p1", Name: "Demo"}
	pm := newProject(m.client, proj, prs, []ado.Repo{{ID: "r1", Name: "app", Project: ado.Project{ID: "p1", Name: "Demo"}}})
	pm.width, pm.height = width, height
	pm.tab = projTabPRs
	pm.buildsLoaded = true
	if builds != nil {
		pm.builds = builds
	}
	pm.lists[projTabPRs].setItems(pm.prItems())
	m.project = pm
	return m
}

func runProjectBatch(m Model, cmd tea.Cmd, steps int) Model {
	queue := []tea.Cmd{cmd}
	for step := 0; step < steps && len(queue) > 0; step++ {
		var nextQueue []tea.Cmd
		for _, c := range queue {
			if c == nil {
				continue
			}
			msg := c()
			if msg == nil {
				continue
			}
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					if sub == nil {
						continue
					}
					subMsg := sub()
					if subMsg == nil {
						continue
					}
					next, nextCmd := m.Update(subMsg)
					m = next.(Model)
					if nextCmd != nil {
						nextQueue = append(nextQueue, nextCmd)
					}
				}
				continue
			}
			next, nextCmd := m.Update(msg)
			m = next.(Model)
			if nextCmd != nil {
				nextQueue = append(nextQueue, nextCmd)
			}
		}
		queue = nextQueue
	}
	return m
}

func TestProjectPRsTabLazyLoadsAndRendersStatusCIColumnsAndBar(t *testing.T) {
	prs := []ado.PullRequest{
		makeDashPR(11, "feat: add api", false),
		makeDashPR(12, "wip: draft pr", true),
	}
	r11 := fakeRun(701, 10, "api-build", "inProgress", "")
	r12 := fakeRun(702, 20, "e2e-check", "completed", "failed")

	// Switching onto projTabPRs lazily triggers CI loading.
	m := projectPRsModel(t, 150, 20, prs, nil)
	m.project.tab = projTabRepos
	m.project.buildsLoaded = false
	m, cmd := sendKeys(m, "2")
	if m.project.tab != projTabPRs || !m.project.buildsLoaded || cmd == nil {
		t.Fatalf("switching to tab 2 should mark buildsLoaded and trigger fetches (tab=%v loaded=%v cmd=%v)",
			m.project.tab, m.project.buildsLoaded, cmd != nil)
	}

	// Populate builds via projectBuildMsg and verify rendering at widths 80, 120, 160.
	builds := map[int]buildResult{
		11: {summary: ado.CISummary{State: ado.BuildRunning, Running: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPRRun, Name: "api-build", State: ado.BuildRunning, StatusText: "in progress", Run: &r11, DefinitionID: 10},
		}}},
		12: {summary: ado.CISummary{State: ado.BuildFailed, Failed: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPRRun, Name: "e2e-check", State: ado.BuildFailed, StatusText: "failed", Run: &r12, DefinitionID: 20},
		}}},
	}
	for _, w := range []int{80, 120, 160} {
		mw := projectPRsModel(t, w, 20, prs, builds)
		view := mw.View()
		lines := strings.Split(view, "\n")
		if len(lines) != 20 {
			t.Errorf("width %d: view height = %d, want 20", w, len(lines))
		}
		for _, line := range lines {
			if lw := ansi.StringWidth(line); lw > w {
				t.Errorf("width %d: line exceeds width (%d): %q", w, lw, ansi.Strip(line))
			}
		}
		plain := ansi.Strip(view)
		if !strings.Contains(plain, "CI !11: ● api-build") {
			t.Errorf("width %d: missing bottom CI summary bar:\n%s", w, plain)
		}
		if !strings.Contains(plain, "● feat: add api") || !strings.Contains(plain, "○ wip: draft pr") {
			t.Errorf("width %d: expected leading ● / ○ PR status icons:\n%s", w, plain)
		}
		for _, old := range []string{"Active", "Draft", "ACT", "DRF"} {
			if strings.Contains(plain, old) {
				t.Errorf("width %d: unexpected status column text %q:\n%s", w, old, plain)
			}
		}
		if w >= narrowCIWidth {
			if !strings.Contains(plain, "● run") || !strings.Contains(plain, "× err") {
				t.Errorf("width %d: expected CI cells:\n%s", w, plain)
			}
		}
	}

	// Clicking bottom CI summary bar opens b.
	mClick := projectPRsModel(t, 150, 20, prs, builds)
	_, y := findRow(t, mClick.View(), "CI !11:")
	next, clickCmd := mClick.Update(click(5, y))
	mClick = next.(Model)
	if clickCmd != nil {
		next, _ = mClick.Update(clickCmd())
		mClick = next.(Model)
	}
	if mClick.dashRun == nil || mClick.dashRun.run.run.ID != 701 {
		t.Fatalf("clicking bottom CI bar on Project -> PRs should open run 701, got %+v", mClick.dashRun)
	}
}

func TestProjectPRsTabCommandsFilterPollingAndPalette(t *testing.T) {
	savedCancel, savedQueue, savedList := cancelRun, queueRun, listPipelines
	defer func() { cancelRun, queueRun, listPipelines = savedCancel, savedQueue, savedList }()

	var cancelled []int
	var queued []string
	cancelRun = func(_ context.Context, _ *ado.Client, _ string, runID int) error {
		cancelled = append(cancelled, runID)
		return nil
	}
	queueRun = func(_ context.Context, _ *ado.Client, _ string, defID int, branch string) (ado.Run, error) {
		queued = append(queued, fmt.Sprintf("%d:%s", defID, branch))
		return fakeRun(950, defID, "deploy", "notStarted", ""), nil
	}
	listPipelines = func(_ context.Context, _ *ado.Client, _ string) ([]ado.Pipeline, error) {
		return []ado.Pipeline{{ID: 88, Name: "deploy"}}, nil
	}

	r1 := fakeRun(801, 10, "api-ci", "inProgress", "")
	r2 := fakeRun(802, 20, "web-lint", "completed", "failed")
	prs := []ado.PullRequest{
		makeDashPR(1, "alpha", false),
		makeDashPR(2, "beta", true),
	}
	builds := map[int]buildResult{
		1: {summary: ado.CISummary{State: ado.BuildRunning, Running: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPRRun, Name: "api-ci", State: ado.BuildRunning, StatusText: "in progress", Run: &r1, DefinitionID: 10},
		}}},
		2: {summary: ado.CISummary{State: ado.BuildFailed, Failed: 1, Checks: []ado.CICheck{
			{Kind: ado.CICheckPRRun, Name: "web-lint", State: ado.BuildFailed, StatusText: "failed", Run: &r2, DefinitionID: 20},
		}}},
	}

	m := projectPRsModel(t, 150, 20, prs, builds)

	// 1. b opens dashRun over m.project with project breadcrumb, esc returns directly to m.project.
	m, cmd := sendKeys(m, "b")
	m = runProjectBatch(m, cmd, 1)
	if m.dashRun == nil || m.dashRun.run.run.ID != 801 || m.project == nil {
		t.Fatalf("b on Project -> PRs should open dashRun over project: dashRun=%+v project=%v", m.dashRun, m.project)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Demo › Pull requests") {
		t.Fatalf("dashRun breadcrumb should show Demo › Pull requests:\n%s", ansi.Strip(m.View()))
	}
	m, _ = sendKeys(m, "esc")
	if m.dashRun != nil || m.project == nil || m.project.tab != projTabPRs {
		t.Fatalf("esc should return to Project -> PRs tab, dashRun=%v project=%v", m.dashRun, m.project)
	}

	// 2. X cancels active run over m.project and updates project.watchCI.
	m, cmd = sendKeys(m, "X")
	m = runProjectBatch(m, cmd, 1)
	if cf, ok := m.modal.(*confirmModal); !ok || !strings.Contains(cf.prompt, "Cancel api-ci") {
		t.Fatalf("expected cancel confirm over project, got %T", m.modal)
	}
	m, cmd = sendKeys(m, "y")
	m = runProjectBatch(m, cmd, 1)
	if len(cancelled) != 1 || cancelled[0] != 801 {
		t.Fatalf("cancelled = %v, want [801]", cancelled)
	}
	if _, watched := m.project.watchCI[1]; !watched {
		t.Fatal("ciActionMsg should record PR 1 in m.project.watchCI")
	}

	// 3. t opens pipelinePicker, confirms, and queues on source branch.
	m, cmd = sendKeys(m, "t")
	m = runProjectBatch(m, cmd, 2)
	if _, ok := m.modal.(*pipelinePicker); !ok {
		t.Fatalf("t on Project -> PRs should open pipelinePicker, got %T", m.modal)
	}
	m, cmd = sendKeys(m, "enter")
	m, cmd = sendKeys(m, "y")
	m = runProjectBatch(m, cmd, 1)
	if strings.Join(queued, ",") != "88:refs/heads/feat/1" {
		t.Fatalf("queued = %v, want [88:refs/heads/feat/1]", queued)
	}

	// 4. Q on failed non-policy PR 2 opens Retry/Run-new choice menu.
	m, cmd = sendKeys(m, "j", "Q")
	m = runProjectBatch(m, cmd, 1)
	if m.modal == nil || !strings.Contains(ansi.Strip(m.modal.view(m.width)), "Retry failed jobs") {
		t.Fatalf("expected Retry/Run-new menu, got %T", m.modal)
	}
	m, _ = sendKeys(m, "esc")

	// 5. / filter on Project -> PRs matches draft, err, and pipeline name.
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"draft", 2},
		{"err", 2},
		{"web-lint", 2},
		{"api-ci", 1},
	} {
		m.project.lists[projTabPRs].filter = tc.query
		vis := m.project.lists[projTabPRs].visible()
		if len(vis) != 1 || m.project.lists[projTabPRs].items[vis[0]].value.(ado.PullRequest).ID != tc.want {
			t.Errorf("Project -> PRs filter %q matched %d items, want PR %d", tc.query, len(vis), tc.want)
		}
	}
	m.project.lists[projTabPRs].filter = ""

	// 6. Adaptive polling on projectModel: starts for active run, ignores manual queued, stops when done or switched away.
	mp := projectPRsModel(t, 150, 20, prs, map[int]buildResult{})
	manualPol := ado.Policy{EvaluationID: "e-man", Status: "queued"}
	manualPol.Configuration.IsEnabled = true
	manualPol.Configuration.Type.ID = ado.PolicyTypeBuild
	sumManual := ado.SummarizeCI([]ado.Policy{manualPol}, nil, nil, nil)
	next, pollCmd := mp.Update(projectBuildMsg{from: mp.client, projectID: "p1", prID: 2, result: buildResult{summary: sumManual}})
	mp = next.(Model)
	if mp.project.ciPolling || pollCmd != nil {
		t.Fatal("manual queued policy must not start polling on projectModel")
	}
	next, pollCmd = mp.Update(projectBuildMsg{from: mp.client, projectID: "p1", prID: 1, result: builds[1]})
	mp = next.(Model)
	if !mp.project.ciPolling || pollCmd == nil {
		t.Fatal("active run must start polling on projectModel")
	}
	gen := mp.project.ciPollGen
	// Stale generation dropped.
	next, stale := mp.Update(projectCIPollTickMsg{from: mp.client, projectID: "p1", gen: gen - 1})
	if stale != nil {
		t.Fatal("stale projectCIPollTickMsg should be ignored")
	}
	// Switching off projTabPRs stops polling on tick.
	mp.project.tab = projTabRepos
	next, offTab := mp.Update(projectCIPollTickMsg{from: mp.client, projectID: "p1", gen: gen})
	mp = next.(Model)
	if mp.project.ciPolling || offTab != nil {
		t.Fatal("polling should stop when user leaves projTabPRs")
	}

	// 7. Palette : on Project -> PRs shows CI/CD group and executes b locally.
	mp.project.tab = projTabPRs
	mp, _ = sendKeys(mp, ":")
	titles := groupTitles(openedPalette(t, mp))
	hasCI := false
	for _, title := range titles {
		if title == "CI/CD" {
			hasCI = true
		}
	}
	if !hasCI {
		t.Fatalf("Project -> PRs palette should include CI/CD group, got %v", titles)
	}
	mp = typeText(mp, "pipeline runs")
	mp, cmd = sendKeys(mp, "enter")
	mp = runProjectBatch(mp, cmd, 2)
	if mp.detail != nil || mp.dashRun == nil || mp.dashRun.run.run.ID != 801 {
		t.Fatalf("Project -> PRs palette 'Pipeline runs' should open dashRun locally: detail=%v dashRun=%+v", mp.detail, mp.dashRun)
	}
}
