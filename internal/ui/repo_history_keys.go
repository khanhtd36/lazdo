package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// historyKey handles the Commits and Tags tabs; handled false lets the
// project act (esc back to the repos).
func (b *repoBrowser) historyKey(msg tea.KeyMsg, width, height int) (bool, tea.Cmd) {
	if b.tab == repoTabTags {
		return b.tagsKey(msg, height)
	}
	k := msg.String()
	switch k {
	case "z":
		b.hideBranches = !b.hideBranches
		b.pane = paneFiles
		return true, nil
	case "tab", "shift+tab":
		if b.pane == paneBranches || b.foldBranches(width) {
			b.pane = paneFiles
		} else {
			b.pane = paneBranches
		}
		return true, nil
	}
	if b.pane == paneBranches {
		return b.historyBranchesKey(msg, height)
	}
	return b.commitsKey(msg, height)
}

func (b *repoBrowser) historyBranchesKey(msg tea.KeyMsg, height int) (bool, tea.Cmd) {
	switch msg.String() {
	case "y":
		return true, b.copySelection()
	case "c":
		if it, ok := b.branches.selected(); ok {
			return true, requestCheckout(b.client.Org, b.project.Name, b.repo.Name, it.value.(ado.Branch).Name)
		}
		return true, nil
	}
	handled, cmd := b.branchesKey(msg, height)
	if b.branch != b.hist.commitsFor && b.hist.release == nil {
		return true, tea.Batch(cmd, b.ensureHistory()) // branch switched: its commits
	}
	return handled, cmd
}

func (b *repoBrowser) commitsKey(msg tea.KeyMsg, height int) (bool, tea.Cmd) {
	h := &b.hist
	handled, activate := h.commitList.key(msg, height)
	c, ok := b.selectedCommit()
	if activate && ok {
		return true, b.openCommitDiff(c)
	}
	if handled {
		return true, b.loadMoreCommits()
	}
	switch msg.String() {
	case "esc":
		if h.release != nil {
			h.release = nil
			h.commitsFor = "" // reload the branch history
			b.tab = repoTabTags
			return true, nil
		}
		return false, nil
	case "r":
		h.commitsFor = ""
		return true, b.ensureHistory()
	case "y":
		if ok {
			return true, copyMenu("commit "+shortSHA(c.ID),
				copyItem{"Web URL", b.client.CommitURL(b.repo, c.ID)},
				copyItem{"Commit ID", c.ID},
				copyItem{"Short ID", shortSHA(c.ID)},
				copyItem{"Message", firstLine(c.Comment)})
		}
	case "o":
		if ok {
			return true, openURL(b.client.CommitURL(b.repo, c.ID), "opened "+shortSHA(c.ID))
		}
	case "c":
		if ok {
			return true, requestDetached(b.client.Org, b.project.Name, b.repo.Name, b.branch, c.ID, "commit "+shortSHA(c.ID))
		}
	case "T":
		if ok {
			return true, showModal(b.newTagDialog(c))
		}
	}
	return true, nil
}

// loadMoreCommits fetches the next page as the cursor nears the end.
func (b *repoBrowser) loadMoreCommits() tea.Cmd {
	h := &b.hist
	if h.release != nil || !h.commitsMore || h.commitsLoading {
		return nil
	}
	if h.commitList.cursor < len(h.commitList.visible())-10 && h.commitList.filter == "" {
		return nil
	}
	return b.loadCommits(len(h.commits))
}

func (b *repoBrowser) openCommitDiff(c ado.RepoCommit) tea.Cmd {
	r := b.repo
	label := shortSHA(c.ID) + " · " + firstLine(c.Comment)
	return func() tea.Msg { return openRepoDiffMsg{repo: r, label: label, commit: c.ID} }
}

func (b *repoBrowser) tagsKey(msg tea.KeyMsg, height int) (bool, tea.Cmd) {
	handled, activate := b.hist.tagList.key(msg, height)
	t, ok := b.selectedTag()
	if activate && ok {
		return true, b.openRelease(t)
	}
	if handled {
		return true, b.loadTagInfo()
	}
	switch msg.String() {
	case "esc":
		return false, nil
	case "r":
		b.hist.tagsLoaded, b.hist.tagInfo = false, map[string]*ado.TagInfo{}
		return true, b.loadTags()
	case "D":
		if !ok {
			return true, nil
		}
		prev, hasPrev := b.previousTag(t)
		if !hasPrev {
			return true, statusCmd("no earlier version tag to compare with")
		}
		r, label := b.repo, prev.Name+" → "+t.Name
		return true, func() tea.Msg {
			return openRepoDiffMsg{repo: r, label: label, from: prev.CommitID, commit: t.CommitID}
		}
	case "y":
		if ok {
			return true, copyText(t.Name, "copied "+t.Name) // the tag itself, no menu
		}
	case "o":
		if ok {
			return true, openURL(b.client.TagURL(b.repo, t.Name), "opened "+t.Name)
		}
	case "c":
		if ok {
			return true, requestDetached(b.client.Org, b.project.Name, b.repo.Name, "", t.Name, "tag "+t.Name)
		}
	case "d":
		if ok {
			client, r := b.client, b.repo
			return true, showModal(newConfirm("Delete tag "+t.Name+"?", refsCmd(r, "deleted tag "+t.Name, func(ctx context.Context) error {
				return client.DeleteTag(ctx, r, t)
			})))
		}
	}
	return true, nil
}

// deleteBranchKey asks to delete the branch under the cursor; the default
// branch is refused, and a PR using it is named in the question.
func (b *repoBrowser) deleteBranchKey() tea.Cmd {
	it, ok := b.branches.selected()
	if !ok {
		return nil
	}
	br := it.value.(ado.Branch)
	if br.IsBaseVersion || br.Name == b.repo.DefaultBranchName() {
		return statusCmd("refusing to delete the default branch " + br.Name)
	}
	prompt := "Delete branch " + br.Name + "?"
	for _, pr := range b.prs {
		if pr.Repository.ID == b.repo.ID && pr.SourceBranch() == br.Name {
			prompt = fmt.Sprintf("!%d uses this branch; deleting it breaks the PR.\n%s", pr.ID, prompt)
		}
	}
	client, r := b.client, b.repo
	return showModal(newConfirm(prompt, refsCmd(r, "deleted branch "+br.Name, func(ctx context.Context) error {
		return client.DeleteBranch(ctx, r, br.Name, br.Commit.ID)
	})))
}

// refsCmd runs a ref write and reports it as a refsChangedMsg.
func refsCmd(r ado.Repo, done string, f func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return refsChangedMsg{repoID: r.ID, text: done, err: f(ctx)}
	}
}

// --- Tag dialog ---

// tagDialog creates a tag on a commit: annotated with a message, or
// lightweight when the message is left empty.
type tagDialog struct {
	b       *repoBrowser
	commit  ado.RepoCommit
	name    textField
	message textField
	onName  bool
}

func (b *repoBrowser) newTagDialog(c ado.RepoCommit) *tagDialog {
	return &tagDialog{b: b, commit: c, name: newTextField(40), message: newTextField(60), onName: true}
}

func (t *tagDialog) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "tab", "shift+tab":
		t.onName = !t.onName
		return t, nil
	case "enter", "ctrl+s":
		name := strings.TrimSpace(t.name.value)
		if name == "" {
			return t, statusCmd("a tag needs a name")
		}
		b, c, message := t.b, t.commit, strings.TrimSpace(t.message.value)
		kind := "lightweight"
		if message != "" {
			kind = "annotated"
		}
		return nil, refsCmd(b.repo, fmt.Sprintf("created %s tag %s on %s", kind, name, shortSHA(c.ID)), func(ctx context.Context) error {
			return b.client.CreateTag(ctx, b.repo, name, c.ID, message)
		})
	}
	if t.onName {
		t.name.edit(msg)
	} else {
		t.message.edit(msg)
	}
	return t, nil
}

func (t *tagDialog) view(int) string {
	field := func(label string, f textField, focused bool) string {
		mark := "  "
		if focused {
			mark = styleSelected.Render("› ")
		}
		return mark + styleDim.Render(label) + "\n  " + f.view(focused)
	}
	return styleModal.Render(strings.Join([]string{
		styleSection.Render("Tag "+shortSHA(t.commit.ID)) + styleDim.Render("  "+firstLine(t.commit.Comment)),
		"",
		field("Name", t.name, t.onName),
		field("Message (empty for a lightweight tag)", t.message, !t.onName),
		"",
		styleDim.Render("enter create · tab next field · esc cancel"),
	}, "\n"))
}

// textField is a minimal one-line input.
type textField struct {
	value string
	width int
}

func newTextField(width int) textField { return textField{width: width} }

func (f *textField) edit(msg tea.KeyMsg) {
	switch {
	case msg.String() == "backspace":
		if r := []rune(f.value); len(r) > 0 {
			f.value = string(r[:len(r)-1])
		}
	case msg.Type == tea.KeyRunes:
		f.value += string(msg.Runes)
	case msg.Type == tea.KeySpace:
		f.value += " "
	}
}

func (f textField) view(focused bool) string {
	cursor := ""
	if focused {
		cursor = "▏"
	}
	return fit(f.value+cursor, f.width)
}
