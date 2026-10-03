package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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
