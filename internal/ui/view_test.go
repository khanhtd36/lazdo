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
		if compact && (!strings.Contains(view, "TDK") || !strings.Contains(view, "[+8p]")) {
			t.Errorf("width %d: want initials TDK and [+8p]:\n%s", width, view)
		}
		// The title keeps a third of the width, and the ID column lines up.
		idCols := map[int]bool{}
		for _, line := range strings.Split(view, "\n") {
			if i := strings.Index(line, "!1"); i >= 0 {
				idCols[ansi.StringWidth(line[:i])] = true
				if !strings.Contains(line, "fix(mdbi): upgrade") && !strings.Contains(line, "feat(das): add --ver") {
					t.Errorf("width %d: title squeezed: %q", width, line)
				}
			}
		}
		if len(idCols) != 1 {
			t.Errorf("width %d: ID column not aligned: %v\n%s", width, idCols, view)
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
