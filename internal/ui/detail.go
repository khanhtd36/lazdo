package ui

import (
	"context"
	"fmt"
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
	client  *ado.Client
	me      ado.Identity
	pr      ado.PullRequest
	repoKey string

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

	// Commits, Conflicts
	cursor [tabCount]int

	modal modal
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

func newDetail(client *ado.Client, me ado.Identity, pr ado.PullRequest, repoKey string, width, height int) *detailModel {
	d := &detailModel{
		client:    client,
		me:        me,
		pr:        pr,
		repoKey:   repoKey,
		loading:   true,
		md:        newMarkdownCache(),
		files:     newFilesView(),
		rowCaches: map[string]*diffRows{},
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
func (d *detailModel) bodyHeight() int { return max(1, d.height-6) }

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
		if d.tab == tabFiles {
			return d.ensureFiles()
		}
	case filesLoadedMsg:
		return d.onFilesLoaded(msg)
	case fileDiffMsg:
		d.files.diffs[msg.key] = msg.diff
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
		if e, ok := d.modal.(*editorModal); ok {
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

func (d *detailModel) onKey(msg tea.KeyMsg) tea.Cmd {
	d.status = ""
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
		return openURL(d.pr.WebURL(d.client.Org), fmt.Sprintf("opened !%d", d.pr.ID))
	case "y":
		u := d.pr.WebURL(d.client.Org)
		return func() tea.Msg { return resultMsg(actions.CopyToClipboard(u), "copied "+u) }
	case "c":
		return checkoutCmd(d.client.Org, d.repoKey, d.pr)
	case "v":
		if d.data != nil {
			d.modal = d.voteMenu()
		}
	case "m":
		if d.data != nil {
			d.modal = d.completeMenu()
		}
	default:
		switch d.tab {
		case tabOverview:
			return d.overviewKey(msg)
		case tabFiles:
			return d.filesKey(msg)
		case tabCommits, tabConflicts:
			return d.listKey(msg)
		case tabCount:
		}
	}
	return nil
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
	if d.modal != nil || msg.String() != "esc" {
		return false
	}
	if f := &d.files; d.tab == tabFiles {
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
		}
	}
	if d.inThread {
		d.inThread = false
		d.rebuildOverview()
		return false
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

func checkoutCmd(org, repoKey string, pr ado.PullRequest) tea.Cmd {
	prKey := actions.RepoKey(org, pr.Repository.Project.Name, pr.Repository.Name)
	if repoKey != prKey {
		return func() tea.Msg {
			return statusMsg("checkout: run lazdo inside a clone of " + pr.Repository.Name)
		}
	}
	branch := pr.SourceBranch()
	return func() tea.Msg { return resultMsg(actions.Checkout(branch), "switched to "+branch) }
}
