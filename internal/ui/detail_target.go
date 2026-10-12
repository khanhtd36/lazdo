package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type (
	// targetBranchesMsg brings the branches a pull request can merge into.
	targetBranchesMsg struct {
		prID          int
		names         []string
		defaultBranch string
		err           error
	}
	// targetChangedMsg reports a change of target branch.
	targetChangedMsg struct {
		prID   int
		branch string
		err    error
	}
)

// targetPicker is "Change target branch…": the repo's branches but the
// source, filtered as you type, the current target selected.
type targetPicker struct {
	d       *detailModel
	list    pickList
	loading bool
}

func (d *detailModel) openTargetPicker() (modal, tea.Cmd) {
	p := &targetPicker{d: d, loading: true}
	p.list.typing = true
	client, pr := d.client, d.pr
	return p, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		names, def, err := client.TargetBranches(ctx, pr)
		return targetBranchesMsg{prID: pr.ID, names: names, defaultBranch: def, err: err}
	}
}

// onBranches fills the list: the default branch first, then A–Z.
func (p *targetPicker) onBranches(msg targetBranchesMsg) tea.Cmd {
	p.loading = false
	if msg.err != nil {
		p.d.modal = nil
		return statusCmd("error: list branches: " + msg.err.Error())
	}
	current, source := p.d.pr.TargetBranch(), p.d.pr.SourceBranch()
	names := make([]string, 0, len(msg.names))
	for _, n := range msg.names {
		if n != source {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == msg.defaultBranch) != (names[j] == msg.defaultBranch) {
			return names[i] == msg.defaultBranch
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	items := make([]pickItem, 0, len(names))
	for i, n := range names {
		note := ""
		switch n {
		case current:
			note = "  (current)"
			p.list.cursor = i
		case msg.defaultBranch:
			note = "  (default)"
		}
		items = append(items, pickItem{search: n, value: n, render: func(int) string {
			return styleAccent.Render(n) + styleDim.Render(note)
		}})
	}
	p.list.items = items
	return nil
}

func (p *targetPicker) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	if p.loading {
		if msg.String() == "esc" {
			return nil, nil
		}
		return p, nil
	}
	handled, activate := p.list.key(msg, pickerRows)
	switch {
	case activate:
		return p.pick()
	case !handled && msg.String() == "esc":
		return nil, nil
	}
	return p, nil
}

// pick asks before changing the target: with auto-complete set, the pull
// request would merge into the new branch on its own.
func (p *targetPicker) pick() (modal, tea.Cmd) {
	it, ok := p.list.selected()
	if !ok {
		return p, nil
	}
	branch := it.value.(string)
	d := p.d
	if branch == d.pr.TargetBranch() {
		return p, statusCmd(branch + " is already the target")
	}
	prompt := fmt.Sprintf("Change the target of !%d from %s to %s?", d.pr.ID,
		styleAccent.Render(d.pr.TargetBranch()), styleAccent.Render(branch))
	if a := d.data.AutoCompleteSetBy; a != nil && a.ID != "" {
		prompt += "\n" + styleYellow.Render("Auto-complete is set: it will merge into "+branch+".")
	}
	c := newConfirm(prompt, d.changeTarget(branch))
	c.back = p
	return c, nil
}

func (d *detailModel) changeTarget(branch string) tea.Cmd {
	client, pr := d.client, d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return targetChangedMsg{prID: pr.ID, branch: branch, err: client.SetTargetBranch(ctx, pr, branch)}
	}
}

// onTargetChanged reloads everything the target decides: files, commits,
// conflicts, merge status and checks.
func (d *detailModel) onTargetChanged(msg targetChangedMsg) tea.Cmd {
	if msg.err != nil {
		d.status = "error: change target branch: " + msg.err.Error()
		return nil
	}
	d.status = "target changed to " + msg.branch
	d.files = newFilesView()
	d.rowCaches = map[string]*diffRows{}
	return d.reload()
}

func (p *targetPicker) view(width int) string {
	w := max(30, min(width-8, 80))
	lines := []string{styleSection.Render("Change target branch") + styleDim.Render("  into which branch should !"+fmt.Sprint(p.d.pr.ID)+" merge?"), ""}
	if p.loading {
		lines = append(lines, styleDim.Render("loading branches…"))
	} else {
		rows := min(pickerRows, max(1, len(p.list.visible())))
		if _, ok := p.list.filterLine(); ok {
			rows++
		}
		lines = append(lines, p.list.view(w, rows)...)
	}
	lines = append(lines, "", styleDim.Render("type to filter · enter pick · esc close"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

func (p *targetPicker) clickRow(row int) (modal, tea.Cmd) {
	if !p.loading && p.list.click(row-2) { // below the title and a blank line
		return p.pick()
	}
	return p, nil
}
