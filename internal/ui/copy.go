package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/actions"
	"github.com/khanhtd36/lazdo/internal/ado"
)

// copyItem is one thing the Copy menu (y) can put on the clipboard.
type copyItem struct {
	label string
	value string
}

// copyMenuMsg asks the root to open the Copy menu for a selection; the
// first item is the most common choice, so y enter copies it.
type copyMenuMsg struct {
	title string
	items []copyItem
}

func copyMenu(title string, items ...copyItem) tea.Cmd {
	var kept []copyItem
	for _, it := range items {
		if it.value != "" {
			kept = append(kept, it)
		}
	}
	return func() tea.Msg { return copyMenuMsg{title: title, items: kept} }
}

// copyText puts text on the clipboard straight away (a selection is already
// a precise choice, so it skips the menu).
func copyText(text, done string) tea.Cmd {
	return func() tea.Msg { return resultMsg(actions.CopyToClipboard(text), done) }
}

func copyPR(org string, pr ado.PullRequest) tea.Cmd {
	return copyMenu(fmt.Sprintf("!%d", pr.ID),
		copyItem{"Web URL", pr.WebURL(org)},
		copyItem{"Source branch", pr.SourceBranch()},
		copyItem{"ID", fmt.Sprintf("!%d", pr.ID)},
		copyItem{"Title", pr.Title},
	)
}

func copyRepo(r ado.Repo) tea.Cmd {
	return copyMenu("repo "+r.Name,
		copyItem{"Clone URL (https)", r.RemoteURL},
		copyItem{"Clone URL (ssh)", r.SSHURL},
		copyItem{"Web URL", r.WebURL},
		copyItem{"Name", r.Name},
	)
}

func copyProject(client *ado.Client, p ado.ProjectInfo) tea.Cmd {
	return copyMenu("project "+p.Name,
		copyItem{"Web URL", client.ProjectURL(p)},
		copyItem{"Name", p.Name},
	)
}

func copyRun(client *ado.Client, projectName string, r ado.Run) tea.Cmd {
	return copyMenu("run "+r.BuildNumber,
		copyItem{"Web URL", client.RunURL(projectName, r.ID)},
		copyItem{"Run number", r.BuildNumber},
		copyItem{"Branch", r.Branch()},
	)
}

func newCopyMenu(msg copyMenuMsg) modal {
	items := make([]menuItem, 0, len(msg.items))
	for _, it := range msg.items {
		items = append(items, menuItem{
			label: it.label + styleDim.Render("  "+truncate(it.value, 60)),
			run: func() (modal, tea.Cmd) {
				return nil, func() tea.Msg { return resultMsg(actions.CopyToClipboard(it.value), "copied "+it.value) }
			},
		})
	}
	return &menuModal{title: "Copy " + msg.title, items: items}
}
