package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/khanhtd36/lazdo/internal/ado"
	"github.com/khanhtd36/lazdo/internal/update"
)

func TestViewFitsWidth(t *testing.T) {
	pr := ado.PullRequest{
		ID:            15074,
		Title:         strings.Repeat("very long title ", 20),
		IsDraft:       true,
		CreatedBy:     ado.Identity{DisplayName: "Nam X. Duong"},
		CreationDate:  time.Now().Add(-3 * time.Hour),
		TargetRefName: "refs/heads/stable/8.0",
		Repository:    ado.Repository{Name: "MITS11"},
		Reviewers: []ado.Reviewer{
			{Identity: ado.Identity{ID: "me", DisplayName: "Truong Duy Khanh"}, Vote: ado.VoteWaitingForAuthor, IsRequired: true},
			{Identity: ado.Identity{DisplayName: "[MITS11]\\Reviewers"}, Vote: ado.VoteApproved},
		},
	}
	m := New(ado.NewClient("arbinSW"), time.Minute)
	m.me = ado.Identity{ID: "me"}
	m.loading = false
	m.sections = ado.Classify("me", []ado.PullRequest{pr}, nil)
	m.stats = map[int]ado.Stats{pr.ID: {Comments: 2, ActiveComments: 2, Visited: true, NewPushes: 8, NewComments: 1}}
	m.builds[pr.ID] = buildResult{state: ado.BuildFailed}

	for _, width := range []int{80, 120, 200} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		view := next.View()
		// Narrow terminals clip the right-hand columns first.
		if width >= 120 && !strings.Contains(view, "!15074") {
			t.Fatalf("width %d: PR row missing:\n%s", width, view)
		}
		for line := range strings.SplitSeq(view, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line is %d wide: %q", width, w, ansi.Strip(line))
			}
		}
	}
}

func TestDashboardCompactLayout(t *testing.T) {
	me := ado.Identity{ID: "me"}
	mk := func(id int, title, author string, draft bool) ado.PullRequest {
		return ado.PullRequest{
			ID: id, Title: title, IsDraft: draft, CreatedBy: ado.Identity{DisplayName: author},
			Repository: ado.Repository{Name: "MITS11"}, TargetRefName: "refs/heads/develop",
			Reviewers: []ado.Reviewer{{Identity: me, IsRequired: true}},
		}
	}
	prs := []ado.PullRequest{
		mk(15069, "fix(mdbi): upgrade master, info and data databases", "Truong Duy Khanh", false),
		mk(14736, "feat(das): add --verify-iv, the firmware wire contract", "Nam X. Duong", true),
	}
	for _, width := range []int{100, 130, 200} {
		m := New(ado.NewClient("org"), time.Minute)
		m.me, m.loading = me, false
		m.sections = ado.Classify("me", prs, nil)
		m.stats = map[int]ado.Stats{14736: {Visited: true, NewPushes: 8}}
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		view := ansi.Strip(next.View())

		compact := width < compactWidth
		if compact != strings.Contains(view, "[d]") || compact == strings.Contains(view, "[draft]") {
			t.Errorf("width %d: compact=%v but badges are wrong:\n%s", width, compact, view)
		}
		if strings.Contains(view, "[r]") || strings.Contains(view, "[required]") {
			t.Errorf("width %d: no required badge:\n%s", width, view)
		}
		if compact && !strings.Contains(view, "[+8p]") {
			t.Errorf("width %d: want [+8p]:\n%s", width, view)
		}
	}
}

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"Truong Duy Khanh":    "TK",
		"Nam X. Duong":        "ND",
		"[MITS11]\\Reviewers": "MR",
		"nhan":                "NH",
		"":                    "?",
	} {
		if got := initials(in); got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDashboardBoldsNewActivity(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	m := New(ado.NewClient("org"), time.Minute)
	m.width, m.height = 200, 20
	pr := func(id int, title string) *ado.PullRequest {
		return &ado.PullRequest{ID: id, Title: title, IsDraft: id == 5, Repository: ado.Repository{Name: "r"}}
	}
	m.stats = map[int]ado.Stats{
		1: {Visited: true, NewComments: 1},
		2: {Visited: true, NewPushes: 2},
		3: {Visited: true, NewVotes: 1},
		4: {Visited: false},
		5: {Visited: false}, // a draft
	}
	bold := lipgloss.NewStyle().Bold(true)
	for id, want := range map[int]bool{1: true, 2: true, 3: false, 4: true, 5: false} {
		title := fmt.Sprintf("title %d", id)
		got := strings.Contains(m.renderPR(pr(id, title)), bold.Render(title))
		if got != want {
			t.Errorf("PR %d: bold title = %v, want %v", id, got, want)
		}
	}
}

func TestSinceLastVisitMarkers(t *testing.T) {
	news := ado.Stats{Visited: true, NewPushes: 3, NewComments: 1, NewVotes: 2}
	cases := []struct {
		symbols, compact bool
		stats            ado.Stats
		want             string
	}{
		{true, false, news, "↑3 “1 ★2"},
		{true, true, news, "↑3 “1 ★2"}, // symbols are already short
		{false, false, news, "[+3 pushes] [+1 comment] [+2 votes]"},
		{false, true, news, "[+3p] [+1c] [+2v]"},
		{true, false, ado.Stats{}, "●"},
		{false, true, ado.Stats{}, "[new]"},
	}
	defer UseSymbols(false)
	for _, c := range cases {
		UseSymbols(c.symbols)
		if got := ansi.Strip(strings.Join(sinceLastVisit(c.stats, c.compact), " ")); got != c.want {
			t.Errorf("symbols=%v compact=%v: got %q, want %q", c.symbols, c.compact, got, c.want)
		}
	}
	UseSymbols(true)
	if got := ansi.Strip(draftBadge(false)); got != "◌" {
		t.Errorf("draft symbol: %q", got)
	}
}

func TestDashboardGraysDraftTitles(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	m := New(ado.NewClient("org"), time.Minute)
	m.width, m.height = 200, 20
	m.stats = map[int]ado.Stats{1: {Visited: true}, 2: {Visited: true, NewPushes: 1}}
	gray := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	draft := &ado.PullRequest{ID: 1, Title: "wip thing", IsDraft: true, Repository: ado.Repository{Name: "r"}}
	if !strings.Contains(m.renderPR(draft), gray.Render("wip thing")) {
		t.Error("a draft's title should be gray")
	}
	news := &ado.PullRequest{ID: 2, Title: "wip pushed", IsDraft: true, Repository: ado.Repository{Name: "r"}}
	if !strings.Contains(m.renderPR(news), gray.Bold(true).Render("wip pushed")) {
		t.Error("a draft with new pushes should be gray and bold")
	}
}

func TestDashboardNarrowKeepsRepoOverComments(t *testing.T) {
	m := New(ado.NewClient("org"), time.Minute)
	m.width = 100
	l := m.rowLayout()
	if !l.shown[columnRepo] || !l.shown[columnID] || !l.shown[columnBuild] {
		t.Fatalf("at width 100 the repo → target branch column should stay: %+v", l.shown)
	}
	m.width = 90 // room for the repo column or the comments, not both
	if l := m.rowLayout(); !l.shown[columnRepo] || l.shown[columnComments] {
		t.Fatalf("when only one fits, the repo column wins over comments: %+v", l.shown)
	}
	m.width = 80 // too narrow for the repo column at all: comments may show
	if l := m.rowLayout(); l.shown[columnRepo] {
		t.Fatalf("the repo column can't fit at width 80: %+v", l.shown)
	}
	m.width = 220
	for c := range columnCount {
		if !m.rowLayout().shown[c] {
			t.Fatalf("a wide screen shows every column, missing %d", c)
		}
	}
}

func TestUpdateNoticeAndDialog(t *testing.T) {
	SetVersion("0.2.13")
	defer SetVersion("dev")
	m := New(ado.NewClient("org"), time.Minute)
	m.width, m.height, m.loading = 160, 20, false
	next, _ := m.Update(updatesMsg{releases: []update.Release{
		{Tag: "v0.2.14", Notes: "## Changelog\n* 10529aa fix(checks): list a policy set on both branch and repo once\n"},
	}})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.titleLine()), "update v0.2.14 (U)") {
		t.Fatalf("the title should offer the update: %q", ansi.Strip(m.titleLine()))
	}
	next, _ = m.Update(keyMsg("U"))
	m = next.(Model)
	u, ok := m.modal.(*updateModal)
	if !ok {
		t.Fatalf("U should open the update dialog, got %T", m.modal)
	}
	view := ansi.Strip(u.view(160))
	if !strings.Contains(view, "v0.2.13 → v0.2.14") || !strings.Contains(view, "list a policy set on both branch and repo once") {
		t.Fatalf("the dialog should show what changes:\n%s", view)
	}
	next, _ = m.Update(keyMsg("n"))
	if next.(Model).modal != nil {
		t.Fatal("n should close the dialog without updating")
	}
}

func TestQIsTypedInThePREditor(t *testing.T) {
	m := New(ado.NewClient("org"), time.Minute)
	m.detail = fakeDetail(t, 120)
	m.detail.modal = m.detail.newPREditor()
	if !m.inTextInput() {
		t.Fatal("q must type into the title, not quit lazdo")
	}
}
