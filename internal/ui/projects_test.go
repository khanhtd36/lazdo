package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func fakeProjectsPage() projectsPage {
	mits := ado.ProjectInfo{ID: "p1", Name: "MITS11", Description: "DAS, WebConsole"}
	dw := ado.ProjectInfo{ID: "p2", Name: "DataWatcher_Git"}
	old := ado.ProjectInfo{ID: "p3", Name: "Archive"}
	p := projectsPage{}
	p.onLoaded(projectsLoadedMsg{
		projects: []ado.ProjectInfo{old, dw, mits},
		recent:   []string{"p1"},
		repos: []ado.Repo{
			{ID: "r1", Name: "MITS11", Project: ado.Project{ID: "p1", Name: "MITS11"}, DefaultBranch: "refs/heads/develop"},
			{ID: "r2", Name: "dev-tools", Project: ado.Project{ID: "p1", Name: "MITS11"}, DefaultBranch: "refs/heads/develop"},
		},
		prs: []ado.PullRequest{
			{ID: 1, Repository: ado.Repository{Project: ado.Project{ID: "p2"}}},
			{ID: 2, Repository: ado.Repository{Project: ado.Project{ID: "p1"}}},
		},
	})
	return p
}

func listText(l *pickList) string {
	return ansi.Strip(strings.Join(l.view(120, 20), "\n"))
}

func TestProjectsPageGroups(t *testing.T) {
	p := fakeProjectsPage()
	text := listText(&p.list)
	recent, active, all := strings.Index(text, "Recent (1)"), strings.Index(text, "Active (1)"), strings.Index(text, "All projects (3)")
	if recent < 0 || active < recent || all < active {
		t.Fatalf("groups out of order:\n%s", text)
	}
	if strings.Contains(text, "dev-tools") {
		t.Fatal("repos should only show while searching")
	}
	if it, _ := p.list.selected(); it.value.(ado.ProjectInfo).Name != "MITS11" {
		t.Fatal("cursor should start on the first project, not a header")
	}
}

func TestProjectsSearchFindsRepoAndOpensBranches(t *testing.T) {
	p := fakeProjectsPage()
	for _, k := range []string{"/", "d", "e", "v", "-", "t"} {
		p.list.key(keyMsg(k), 20)
	}
	it, ok := p.list.selected()
	if r, isRepo := it.value.(ado.Repo); !ok || !isRepo || r.Name != "dev-tools" {
		t.Fatalf("search should select the dev-tools repo, got %+v", it.value)
	}
	open, cmd := p.openSelected(ado.NewClient("org"))
	if open == nil || cmd == nil || open.level != levelRepo || open.repo.Name != "dev-tools" {
		t.Fatalf("opening a found repo should land on its browser: %+v", open)
	}
}

func TestProjectMainRepoFirst(t *testing.T) {
	p := fakeProjectsPage()
	m := p.newProject(ado.NewClient("org"), ado.ProjectInfo{ID: "p1", Name: "MITS11"})
	if m.repos[0].Name != "MITS11" || m.repos[1].Name != "dev-tools" {
		t.Fatalf("main repo first, then A-Z: %s, %s", m.repos[0].Name, m.repos[1].Name)
	}
}

func TestRankFuzzyMatchesPRNumbersLiterally(t *testing.T) {
	texts := []string{
		"fix(export): keep large exports from stalling Nhan Nguyen !15018 fix/QA-11278-export",
		"feat(download): stream exported large test data Nhan Nguyen !15050 feat/download-15",
		"chore: bump 1 5 0 5 0 deps Khanh Truong !14001 chore/deps",
	}
	for q, want := range map[string][]int{"15050": {1}, "!15050": {1}, "!150": {0, 1}, "1501": {0}} {
		var got []int
		for _, m := range rankFuzzy(q, texts) {
			got = append(got, m.Index)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q matched %v, want %v", q, got, want)
		}
	}
}

func TestPickListSkipsHeadersAndClicks(t *testing.T) {
	var l pickList
	row := func(s string) pickItem {
		return pickItem{search: s, value: s, render: func(int) string { return s }}
	}
	head := pickItem{header: true, render: func(int) string { return "H" }}
	l.setItems([]pickItem{head, row("a"), head, row("b")})
	l.move(1)
	if it, _ := l.selected(); it.value != "b" {
		t.Fatalf("j from a should skip the header to b, got %v", it.value)
	}
	if l.click(1) || l.click(1) != true {
		t.Fatal("first click selects a, second click activates it")
	}
}

func TestPagesSwitchAndProjectEsc(t *testing.T) {
	m := New(ado.NewClient("org"), time.Minute)
	m.width, m.height, m.loading = 120, 20, false
	next, cmd := m.Update(keyMsg("2"))
	if next.(Model).page != pageProjects || cmd == nil {
		t.Fatal("2 should switch to Projects and start loading")
	}
	m = next.(Model)
	m.projects = fakeProjectsPage()
	next, _ = m.Update(keyMsg("enter"))
	m = next.(Model)
	if m.project == nil || m.project.project.Name != "MITS11" {
		t.Fatal("enter should open the selected project")
	}
	// / then 1 types into the filter instead of switching pages.
	next, _ = m.Update(keyMsg("/"))
	next, _ = next.Update(keyMsg("1"))
	if p := next.(Model).project; p == nil || p.tab != projTabRepos || p.lists[projTabRepos].filter != "1" {
		t.Fatal("1 while filtering should be typed, not switch pages")
	}
	next, _ = next.Update(keyMsg("esc")) // clears the filter
	next, _ = next.Update(keyMsg("esc")) // closes the project
	if next.(Model).project != nil {
		t.Fatal("esc at the top of a project should go back to the Projects page")
	}
}

func TestProjectTabKeysLeavePipelineRuns(t *testing.T) {
	p := fakeProjectsPage()
	client := ado.NewClient("org")
	for _, level := range []projectLevel{levelRuns, levelRun} {
		m := p.newProject(client, ado.ProjectInfo{ID: "p1", Name: "MITS11"})
		m.width, m.height = 120, 20
		m.tab, m.level = projTabPipelines, level
		if level == levelRun {
			m.run = newRunView(client, m.project, ado.Run{ID: 1})
		}
		m.key(keyMsg("1"))
		if m.level != levelTabs || m.tab != projTabRepos || m.run != nil {
			t.Fatalf("1 at level %d should switch to Repos at the tabs level, got level %d tab %d", level, m.level, m.tab)
		}
	}
}

func TestRunTreeFlattensPhases(t *testing.T) {
	v := newRunView(ado.NewClient("org"), ado.ProjectInfo{}, ado.Run{ID: 1, Status: "completed"})
	v.update(runLoadedMsg{runID: 1, run: ado.Run{ID: 1, Status: "completed"}, records: []ado.TimelineRecord{
		{ID: "s", Type: "Stage", Name: "__default"},
		{ID: "ph", ParentID: "s", Type: "Phase", Name: "Build"},
		{ID: "j", ParentID: "ph", Type: "Job", Name: "Build linux", Order: 1},
		{ID: "t1", ParentID: "j", Type: "Task", Name: "Checkout", Order: 1, Result: "succeeded", Log: &struct {
			ID int `json:"id"`
		}{ID: 5}},
		{ID: "t2", ParentID: "j", Type: "Task", Name: "Compile", Order: 2, Result: "failed", Log: &struct {
			ID int `json:"id"`
		}{ID: 6}},
	}})
	text := listText(&v.tree)
	if strings.Contains(text, "__default") || strings.Contains(text, "Build\n") {
		t.Fatalf("stage __default and phases should be flattened:\n%s", text)
	}
	if v.logID != 6 {
		t.Fatalf("the failed step's log should open first, got log %d", v.logID)
	}
}

func TestRunTreeJumpsBetweenJobs(t *testing.T) {
	v := newRunView(ado.NewClient("org"), ado.ProjectInfo{}, ado.Run{ID: 1, Status: "completed"})
	v.update(runLoadedMsg{runID: 1, run: ado.Run{ID: 1, Status: "completed"}, records: []ado.TimelineRecord{
		{ID: "j1", Type: "Job", Name: "Setup", Order: 1},
		{ID: "a", ParentID: "j1", Type: "Task", Name: "Init", Order: 1},
		{ID: "b", ParentID: "j1", Type: "Task", Name: "Checkout", Order: 2},
		{ID: "j2", Type: "Job", Name: "Build WebConsole", Order: 2},
		{ID: "c", ParentID: "j2", Type: "Task", Name: "Install", Order: 1},
		{ID: "j3", Type: "Job", Name: "Build DAS", Order: 3},
	}})
	name := func() string {
		it, _ := v.tree.selected()
		return it.value.(ado.TimelineRecord).Name
	}
	v.tree.cursor = 0
	for _, want := range []string{"Build WebConsole", "Build DAS"} {
		v.tree.key(keyMsg("J"), 20)
		if name() != want {
			t.Fatalf("J should jump to the next job %q, got %q", want, name())
		}
	}
	v.tree.move(-1) // Install, inside Build WebConsole
	for _, want := range []string{"Build WebConsole", "Setup"} {
		v.tree.key(keyMsg("K"), 20)
		if name() != want {
			t.Fatalf("K should go to %q, got %q", want, name())
		}
	}
}

func TestPolicySentences(t *testing.T) {
	cfg := func(raw string) ado.PolicyConfig {
		var p ado.PolicyConfig
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		`{"isEnabled":true,"isBlocking":true,"type":{"id":"fa4e907d-c16b-4a4c-9dfa-4906e5d171dd"},"settings":{"minimumApproverCount":2,"resetOnSourcePush":true}}`: "✓ At least 2 reviewers must approve, reset on new pushes · blocking",
		`{"isEnabled":true,"isBlocking":true,"type":{"id":"0609b952-1397-4640-95ec-e00a01b2c241"},"settings":{"buildDefinitionId":7,"validDuration":720}}`:         "✓ Build BE validation on PR must pass, expires after 12h · blocking",
		`{"isEnabled":false,"isBlocking":false,"type":{"id":"fd2167ab-b0be-447a-8ec8-39368250530e"},"settings":{"requiredReviewerIds":["ABC"]}}`:                   "○ Required reviewers: Hung Nguyen · optional · off",
		`{"isEnabled":true,"isBlocking":true,"type":{"id":"fa4e907d-c16b-4a4c-9dfa-4916e5d171ab"},"settings":{"allowSquash":true,"allowRebase":true}}`:             "✓ Merge types: rebase, squash · blocking",
	}
	names, pipelines := map[string]string{"abc": "Hung Nguyen"}, map[int]string{7: "Build BE validation on PR"}
	for raw, want := range cases {
		if got := ansi.Strip(policySentence(cfg(raw), names, pipelines)); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	}
	scoped := cfg(`{"settings":{"scope":[{"refName":"refs/heads/stable","matchKind":"Prefix","repositoryId":"r1"}]}}`)
	if b, r := scoped.Scope(); b != "stable/*" || r != "r1" {
		t.Errorf("prefix scope: %q %q", b, r)
	}
}

func TestGroupSIDMatchesPermissions(t *testing.T) {
	// A graph group descriptor is "vssgp." and the group's SID in base64.
	g := ado.Group{Descriptor: "vssgp.Uy0xLTktMTU1MTM3NDI0NS0xMjA0NDAwOTY5"}
	if g.SID() != "S-1-9-1551374245-1204400969" {
		t.Fatalf("SID %q", g.SID())
	}
}

// fakeSettings is a Settings tab loaded with one repo, one pipeline and one
// minimum-reviewers policy on develop.
func fakeSettings(t *testing.T) *settingsView {
	t.Helper()
	s := newSettingsView(ado.NewClient("org"), ado.ProjectInfo{ID: "p1", Name: "MITS11"}, nil)
	var minRev ado.PolicyConfig
	raw := `{"id":170,"isEnabled":true,"isBlocking":true,"type":{"id":"fa4e907d-c16b-4a4c-9dfa-4906e5d171dd","displayName":"Minimum number of reviewers"},
		"settings":{"minimumApproverCount":1,"blockLastPusherVote":true,"scope":[{"refName":"refs/heads/develop","matchKind":"Exact","repositoryId":null}]}}`
	if err := json.Unmarshal([]byte(raw), &minRev); err != nil {
		t.Fatal(err)
	}
	s.update(settingsLoadedMsg{projectID: "p1", data: &ado.ProjectSettings{
		Repos:        []ado.Repo{{ID: "r1", Name: "MITS11"}},
		AllPipelines: []ado.Pipeline{{ID: 7, Name: "Build BE validation on PR"}},
		Policies:     []ado.PolicyConfig{minRev},
		Names:        map[string]string{},
		Errs:         map[string]error{},
	}})
	return s
}

func TestPolicyFormEditsAndKeepsOtherSettings(t *testing.T) {
	s := fakeSettings(t)
	p := s.data.Policies[0]
	f := s.policyForm(&p, p.Type.ID)
	f.get("count").input.SetValue("2")
	f.get("branch").input.SetValue("stable/*")
	f.get("repo").choice = 1 // MITS11
	if ch := f.changes(); len(ch) != 3 {
		t.Fatalf("three changes expected, got %q", ch)
	}
	got, reviewers, refused := s.policyFromForm(f, p)
	if refused != "" || reviewers != "" {
		t.Fatalf("refused %q", refused)
	}
	if got.Settings["minimumApproverCount"] != 2 || got.Settings["blockLastPusherVote"] != true {
		t.Fatalf("count should change and settings the form doesn't show stay: %v", got.Settings)
	}
	scope := got.Settings["scope"].([]any)[0].(map[string]any)
	if scope["refName"] != "refs/heads/stable" || scope["matchKind"] != "Prefix" || scope["repositoryId"] != "r1" {
		t.Fatalf("scope: %v", scope)
	}
	if p.Settings["minimumApproverCount"] != float64(1) {
		t.Fatal("the loaded policy must not change until saved")
	}
}

func TestPolicyFormRefusesBadInput(t *testing.T) {
	s := fakeSettings(t)
	build := s.policyForm(nil, ado.PolicyTypeBuild)
	build.get("branch").input.SetValue("develop")
	if _, _, why := s.policyFromForm(build, ado.PolicyConfig{Type: struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	}{ID: ado.PolicyTypeBuild}}); why != "pick a pipeline" {
		t.Fatalf("a build policy without a pipeline should be refused, got %q", why)
	}
	merge := s.policyForm(nil, policyTypeMergeStrategy)
	merge.get("branch").input.SetValue("develop")
	for _, k := range []string{"allowNoFastForward", "allowSquash", "allowRebase", "allowRebaseMerge"} {
		merge.get(k).on = false
	}
	p := ado.PolicyConfig{}
	p.Type.ID = policyTypeMergeStrategy
	if _, _, why := s.policyFromForm(merge, p); why == "" {
		t.Fatal("no merge type at all should be refused")
	}
}

func TestVariableFormRules(t *testing.T) {
	s := fakeSettings(t)
	g := ado.VariableGroup{ID: 3, Name: "ArbinCloud", Variables: []ado.Variable{{Name: "GITHUB_ACTOR", Value: "x"}, {Name: "GITHUB_TOKEN", Secret: true}}}
	s.data.VarGroups = []ado.VariableGroup{g}
	submit := func(f *formModal) string {
		_, why := f.save(f)
		return why
	}
	add := s.variableForm(g, nil)
	add.get("name").input.SetValue("github_actor")
	if why := submit(add); !strings.Contains(why, "already has") {
		t.Fatalf("a taken name should be refused, got %q", why)
	}
	add.get("name").input.SetValue("NEW_SECRET")
	add.get("secret").on = true
	if why := submit(add); why != "a new secret needs a value" {
		t.Fatalf("got %q", why)
	}
	keep := s.variableForm(g, &g.Variables[1])
	if keep.get("value").input.Value() != "" {
		t.Fatal("a secret's value must never be filled in")
	}
	keep.get("name").input.SetValue("GITHUB_PAT") // rename, keep the stored secret
	if why := submit(keep); why != "" {
		t.Fatalf("renaming a secret without retyping it should be allowed, got %q", why)
	}
}

func TestDeleteRepoNeedsItsName(t *testing.T) {
	c := newNameConfirm("Delete?", "MITS11", func() tea.Msg { return nil })
	for _, r := range "MITS1" {
		c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m, _ := c.update(tea.KeyMsg{Type: tea.KeyEnter}); m == nil {
		t.Fatal("a wrong name must not confirm")
	}
	c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if m, cmd := c.update(tea.KeyMsg{Type: tea.KeyEnter}); m != nil || cmd == nil {
		t.Fatal("the exact name should confirm")
	}
}
