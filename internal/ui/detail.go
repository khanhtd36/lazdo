package ui

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/actions"
	"github.com/khanhtd36/lazdo/internal/ado"
)

type detailTab int

const (
	tabOverview detailTab = iota
	tabFiles
	tabCommits
	tabConflicts
	tabCount
)

func (t detailTab) title() string {
	switch t {
	case tabOverview:
		return "Overview"
	case tabFiles:
		return "Files"
	case tabCommits:
		return "Commits"
	case tabConflicts:
		return "Conflicts"
	default:
		return "?"
	}
}

// detailModel is the full-screen view of one pull request.
type detailModel struct {
	client *ado.Client
	me     ado.Identity
	pr     ado.PullRequest

	data      *ado.PRDetail
	err       error
	loading   bool
	prevVisit time.Time // Me's visit before this one; zero if none
	status    string

	tab           detailTab
	width, height int

	// Overview
	vp         viewport.Model
	filter     activityFilter
	threadSel  int  // index into the filtered activity entries
	inThread   bool // stepped into the selected thread
	commentSel int  // index into the selected thread's live comments
	entryLines []int
	md         *markdownCache

	// Files
	files     filesView
	rowCaches map[string]*diffRows
	// parents counts each commit's parents, to spot merges; fetched when
	// commits are picked.
	parents        map[string]int
	parentsLoading bool

	// Commits, Conflicts
	lists [tabCount]pickList
	find  textFind // Overview's / find

	modal modal

	// standalone is a repo diff (a commit or a tag range) shown with the
	// Files view alone: no pull request header, tabs, votes or comments.
	standalone bool
	repo       ado.Repo
}

// newRepoDiff opens the Files view on a commit (from empty: against its
// parent) or on the range from..commit.
func newRepoDiff(client *ado.Client, r ado.Repo, label, from, commit string, width, height int) (*detailModel, tea.Cmd) {
	pr := ado.PullRequest{Title: label, Repository: ado.Repository{ID: r.ID, Name: r.Name, Project: r.Project}}
	d := newDetail(client, ado.Identity{}, pr, width, height)
	d.standalone, d.repo, d.loading = true, r, false
	d.data = &ado.PRDetail{PullRequest: pr}
	d.tab = tabFiles
	d.files.cmp = comparison{label: label, commit: commit, fromCommit: from}
	return d, d.ensureFiles()
}

type (
	detailLoadedMsg struct {
		d   *ado.PRDetail
		err error
	}
	visitMsg struct {
		prev time.Time
		err  error
	}
	// actionDoneMsg reports a write; leave closes the detail view.
	actionDoneMsg struct {
		text  string
		err   error
		leave bool
	}
)

func newDetail(client *ado.Client, me ado.Identity, pr ado.PullRequest, width, height int) *detailModel {
	d := &detailModel{
		client:    client,
		me:        me,
		pr:        pr,
		loading:   true,
		md:        newMarkdownCache(),
		files:     newFilesView(),
		rowCaches: map[string]*diffRows{},
		parents:   map[string]int{},
	}
	d.resize(width, height)
	return d
}

func (d *detailModel) init() tea.Cmd {
	client, pr := d.client, d.pr
	visit := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		prev, err := client.RecordVisit(ctx, pr)
		return visitMsg{prev: prev, err: err}
	}
	return tea.Batch(d.load(), visit)
}

func (d *detailModel) load() tea.Cmd {
	client, pr := d.client, d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		data, err := client.Detail(ctx, pr)
		return detailLoadedMsg{d: data, err: err}
	}
}

// reload refreshes the data unless the user is in the middle of something.
func (d *detailModel) reload() tea.Cmd {
	if d.loading || d.modal != nil {
		return nil
	}
	d.loading = true
	return d.load()
}

// bodyHeight is what's left below the header (title, subtitle, tabs, rule)
// and above the footer (status, help).
func (d *detailModel) bodyHeight() int { return max(1, d.height-5) }

// bodyTop is the first screen row of the body, right below the title,
// subtitle and tabs: the block tabs need no rule under them.
func (d *detailModel) bodyTop() int { return 3 }

func (d *detailModel) resize(width, height int) {
	d.width, d.height = width, height
	// One column short of the screen: the viewport pads lines to its width
	// and a full-width line would wrap.
	d.vp.Width, d.vp.Height = width-1, d.bodyHeight()
	d.rebuildOverview()
}

func (d *detailModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case detailLoadedMsg:
		d.loading = false
		d.err = msg.err
		if msg.d != nil {
			d.data = msg.d
			d.pr = msg.d.PullRequest
		}
		d.rebuildOverview()
		d.rebuildLists()
		if d.tab == tabFiles {
			return d.ensureFiles()
		}
	case filesLoadedMsg:
		return d.onFilesLoaded(msg)
	case parentsMsg:
		d.parentsLoading = false
		maps.Copy(d.parents, msg.counts)
		if msg.err != nil {
			d.status = "error: find merge commits: " + msg.err.Error()
		}
	case fileDiffMsg:
		d.files.diffs[msg.key] = msg.diff
		if ch := d.selectedChange(); ch != nil {
			d.useDiff(d.diffKey(ch))
		}
		d.landCursor()
		d.scrollDiffToCursor()
	case visitMsg:
		if msg.err != nil {
			d.status = "error: record visit: " + msg.err.Error()
		}
		d.prevVisit = msg.prev
		d.rebuildOverview()
	case actionDoneMsg:
		if msg.err != nil {
			d.status = "error: " + msg.err.Error()
			return nil
		}
		d.status = msg.text
		d.files.loadedKey = "" // refetch threads so new line comments show
		return tea.Batch(d.reload(), d.ensureFiles())
	case editorDoneMsg:
		if e, ok := d.modal.(editorDoner); ok {
			return e.editorDone(msg)
		}
	case tea.KeyMsg:
		if d.modal != nil {
			var cmd tea.Cmd
			d.modal, cmd = d.modal.update(msg)
			return cmd
		}
		return d.onKey(msg)
	}
	return nil
}

// typing reports whether a / filter or find box in the current tab is
// taking keys as text.
func (d *detailModel) typing() bool {
	switch d.tab {
	case tabOverview:
		return d.find.typing
	case tabFiles:
		return d.files.tree.typing
	case tabCommits, tabConflicts:
		return d.lists[d.tab].typing
	case tabCount:
	}
	return false
}

func (d *detailModel) tabKey(msg tea.KeyMsg) tea.Cmd {
	switch d.tab {
	case tabOverview:
		return d.overviewKey(msg)
	case tabFiles:
		return d.filesKey(msg)
	case tabCommits, tabConflicts:
		return d.listKey(msg)
	case tabCount:
	}
	return nil
}

func (d *detailModel) onKey(msg tea.KeyMsg) tea.Cmd {
	d.status = ""
	if d.typing() {
		return d.tabKey(msg)
	}
	if d.standalone {
		return d.standaloneKey(msg)
	}
	switch msg.String() {
	case "1", "2", "3", "4":
		d.tab = detailTab(msg.String()[0] - '1')
		return d.onTabChange()
	case "]":
		d.tab = (d.tab + 1) % tabCount
		return d.onTabChange()
	case "[":
		d.tab = (d.tab + tabCount - 1) % tabCount
		return d.onTabChange()
	case "r":
		return d.reload()
	case "o":
		if ch := d.selectedChange(); d.tab == tabFiles && ch != nil {
			return openURL(d.fileURL(ch.Item.Path), "opened "+ch.Item.Path)
		}
		if d.tab == tabOverview {
			return d.openMenu()
		}
		return openURL(d.pr.WebURL(d.client.Org), fmt.Sprintf("opened !%d", d.pr.ID))
	case "c":
		return prCheckout(d.client.Org, d.pr)
	case "v":
		if d.data != nil {
			d.modal = d.voteMenu()
		}
	case "m":
		if d.data != nil {
			d.modal = d.completeMenu()
		}
	case "E":
		if d.data != nil {
			d.modal = d.newPREditor()
		}
	default:
		return d.tabKey(msg)
	}
	return nil
}

// standaloneKey handles a repo diff: only the Files keys plus browser and
// refresh; there is no pull request to vote on, complete or check out.
func (d *detailModel) standaloneKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "o":
		if ch := d.selectedChange(); ch != nil {
			return openURL(d.fileURL(ch.Item.Path), "opened "+ch.Item.Path)
		}
		return openURL(d.repo.WebURL+"/commit/"+d.files.cmp.commit, "opened in browser")
	case "r":
		d.files.loadedKey = ""
		return d.ensureFiles()
	case "u", "a", "v", "m", "c":
		return nil
	}
	return d.filesKey(msg)
}

func (d *detailModel) onTabChange() tea.Cmd {
	if d.tab == tabFiles {
		return d.ensureFiles()
	}
	return nil
}

// closeRequested reports whether esc should leave the detail view; before
// that, esc backs out of whatever is open inside it.
func (d *detailModel) closeRequested(msg tea.KeyMsg) bool {
	if d.modal != nil || msg.String() != "esc" || d.typing() {
		return false // a typed filter takes esc itself
	}
	switch d.tab {
	case tabFiles:
		f := &d.files
		switch {
		case f.inThread:
			f.inThread = false
			return false
		case f.anchor >= 0:
			f.anchor = -1
			return false
		case f.pane == paneDiff && !f.hideTree:
			f.pane = paneTree
			return false
		case f.tree.filter != "":
			f.tree.filter = ""
			f.tree.clamp(1)
			return false
		}
	case tabOverview:
		switch {
		case d.find.active():
			d.find = textFind{}
			return false
		case d.inThread:
			d.inThread = false
			d.rebuildOverview()
			return false
		}
	case tabCommits, tabConflicts:
		if l := &d.lists[d.tab]; l.filter != "" {
			l.filter = ""
			l.clamp(1)
			return false
		}
	case tabCount:
	}
	return true
}

// act runs a write and reports it as an actionDoneMsg.
func (d *detailModel) act(done string, leave bool, f func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return actionDoneMsg{text: done, err: f(ctx), leave: leave}
	}
}

func openURL(u, done string) tea.Cmd {
	return func() tea.Msg { return resultMsg(actions.OpenBrowser(u), done) }
}

// prCheckout opens the checkout dialog for a pull request's source branch.
func prCheckout(org string, pr ado.PullRequest) tea.Cmd {
	return requestCheckout(org, pr.Repository.Project.Name, pr.Repository.Name, pr.SourceBranch())
}
