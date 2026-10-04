package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// TestDetailLive renders a real pull request's detail without recording a
// visit. Opt in: LAZDO_LIVE_PR=<id> go test ./internal/ui -run Live -v
func TestDetailLive(t *testing.T) {
	id, _ := strconv.Atoi(os.Getenv("LAZDO_LIVE_PR"))
	if id == 0 {
		t.Skip("set LAZDO_LIVE_PR to run")
	}
	ctx := context.Background()
	org, err := ado.DefaultOrg()
	if err != nil {
		t.Fatal(err)
	}
	c := ado.NewClient(org)
	me, err := c.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *ado.PullRequest
	for _, role := range []string{"reviewerId", "creatorId"} {
		prs, err := c.ActivePRs(ctx, role, me.ID)
		if err != nil {
			t.Fatal(err)
		}
		for i := range prs {
			if prs[i].ID == id {
				found = &prs[i]
			}
		}
	}
	if found == nil {
		t.Fatalf("PR %d not found among my active PRs", id)
	}
	width, _ := strconv.Atoi(os.Getenv("LAZDO_LIVE_WIDTH"))
	if width == 0 {
		width = 160
	}
	d := newDetail(c, me, *found, width, 50)
	data, err := c.Detail(ctx, *found)
	d.update(detailLoadedMsg{d: data, err: err})
	keys := strings.Fields(os.Getenv("LAZDO_LIVE_KEYS"))
	if len(keys) == 0 {
		keys = []string{"1", "2", "j", "l", "n", "|", "S", "|", "3", "j", "enter", "l", "|", "4", "|"}
	}
	for _, key := range keys {
		if key == "|" {
			fmt.Println(d.view())
			fmt.Println("=====")
			continue
		}
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		if key == "enter" {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		drain(d, d.update(msg))
	}
}

// drain runs commands synchronously, feeding their messages back, so the
// test sees loaded files and diffs. Visits are never recorded: the test
// never calls init.
func drain(d *detailModel, cmd tea.Cmd) {
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drain(d, c)
			}
			return
		}
		if msg == nil {
			return
		}
		cmd = d.update(msg)
	}
}
