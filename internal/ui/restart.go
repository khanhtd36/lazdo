package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Restarting into an installed update: lazdo quits and main starts the new
// copy with the page and pull request it showed, as "page=0,pr=15088,tab=1".

type (
	// restartMsg is Yes to restarting into the installed update.
	restartMsg struct{}
	// restoredPRMsg brings the pull request a restarted lazdo reopens.
	restoredPRMsg struct {
		id  int
		pr  ado.PullRequest
		err error
	}
)

// RestartState is what to hand the restarted lazdo once the user asked to
// restart into an installed update; ok is false on a plain quit.
func (m Model) RestartState() (state string, ok bool) {
	return m.restartState, m.restartState != ""
}

func (m Model) restoreState() string {
	parts := []string{"page=" + strconv.Itoa(int(m.page))}
	if d := m.detail; d != nil && !d.standalone {
		parts = append(parts, "pr="+strconv.Itoa(d.pr.ID), "tab="+strconv.Itoa(int(d.tab)))
	}
	return strings.Join(parts, ",")
}

// Restore puts a restarted lazdo back on the page and pull request the
// last one showed.
func (m Model) Restore(state string) Model {
	for _, kv := range strings.Split(state, ",") {
		k, v, _ := strings.Cut(kv, "=")
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			continue
		}
		switch k {
		case "page":
			if page(n) < pageCount {
				m.page = page(n)
			}
		case "pr":
			m.restorePR = n
		case "tab":
			if detailTab(n) < tabCount {
				m.restoreTab = detailTab(n)
			}
		}
	}
	return m
}

func (m Model) reopenPR() tea.Cmd {
	if m.restorePR == 0 {
		return nil
	}
	client, id := m.client, m.restorePR
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pr, err := client.PullRequestByID(ctx, id)
		return restoredPRMsg{id: id, pr: pr, err: err}
	}
}

func (m Model) onRestoredPR(msg restoredPRMsg) (tea.Model, tea.Cmd) {
	m.restorePR = 0
	if msg.err != nil {
		m.status = fmt.Sprintf("couldn't reopen !%d: %v", msg.id, msg.err)
		return m, nil
	}
	next, cmd := m.openDetail(&msg.pr)
	m = next.(Model)
	m.detail.tab = m.restoreTab
	return m, cmd
}

// restartPrompt asks before restarting into the installed update; a draft
// in an open editor would be lost, so it says so.
func (m Model) restartPrompt() modal {
	prompt := fmt.Sprintf("v%s is installed. Restart lazdo now?", m.installed)
	if m.detail != nil {
		switch m.detail.modal.(type) {
		case *editorModal, *prEditor, *completeDialog:
			prompt += "\n" + styleYellow.Render("What you are typing will be lost.")
		}
	}
	return newConfirm(prompt, func() tea.Msg { return restartMsg{} }).focusYes()
}

func (m Model) onRestart() (tea.Model, tea.Cmd) {
	m.restartState = m.restoreState()
	return m, tea.Quit
}
