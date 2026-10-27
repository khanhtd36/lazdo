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

	// A pipeline run of the pull request, shown in place of the tabs.
	run       *runView
	runName   string      // its pipeline or build policy
	runPolicy *ado.Policy // the build policy behind it; nil for another pipeline
	runStart  string      // its state when first loaded, to spot a change
	wait      *runWait    // a re-queue waiting for its new run
	// buildLines maps Overview lines to the build policy checks on them.
	buildLines map[int]ado.Policy
	// pending is a command picked from outside the pull request, run once
	// it has loaded.
	pending string
	// people and teams are who @ in a comment can mention.
	people *peopleCache
	teams  []ado.Team

	// standalone is a repo diff (a commit or a tag range) shown with the
	// Files view alone: no pull request header, tabs, votes or comments.
	standalone bool
	repo       ado.Repo

	// backTab is the tab a commit's diff was opened from; esc on the Files
	// tab returns there before it leaves the pull request.
	backTab *detailTab
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
		prID int
		d    *ado.PRDetail
		err  error
	}
	visitMsg struct {
		prID int
		prev time.Time
		err  error
	}
	// actionDoneMsg reports a write; leave closes the detail view.
	actionDoneMsg struct {
		prID  int
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
		return visitMsg{prID: pr.ID, prev: prev, err: err}
	}
	return tea.Batch(d.load(), visit)
}

func (d *detailModel) load() tea.Cmd {
	client, pr := d.client, d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		data, err := client.Detail(ctx, pr)
		return detailLoadedMsg{prID: pr.ID, d: data, err: err}
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
	// Inside the Overview pane's frame.
	d.vp.Width, d.vp.Height = d.overviewWidth()-2, paneInnerHeight(d.bodyHeight())
	d.rebuildOverview()
}

func (d *detailModel) update(msg tea.Msg) tea.Cmd {
	if p, ok := msg.(forPR); ok && p.pullRequest() != d.pr.ID {
		return nil // an answer for a pull request opened before this one
	}
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
		if d.pending != "" && d.data != nil && msg.err == nil {
			return d.runPending()
		}
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
	case targetBranchesMsg:
		if p, ok := d.modal.(*targetPicker); ok && msg.prID == d.pr.ID {
			return p.onBranches(msg)
		}
	case targetChangedMsg:
		return d.onTargetChanged(msg)
	case teamsMsg:
		d.teams = msg.teams
	case mentionTickMsg:
		if e, ok := d.modal.(*editorModal); ok {
			return e.onMentionTick(msg)
		}
	case mentionFoundMsg:
		if e, ok := d.modal.(*editorModal); ok {
			e.onMentionFound(msg)
		}
	case peopleTickMsg:
		if p, ok := d.modal.(*peoplePicker); ok {
			return p.onTick(msg)
		}
	case peopleFoundMsg:
		if p, ok := d.modal.(*peoplePicker); ok {
			p.onFound(msg)
		}
	case actionDoneMsg:
		if msg.err != nil {
			d.status = "error: " + msg.err.Error()
			return nil
		}
		d.status = msg.text
		d.files.loadedKey = "" // refetch threads so new line comments show
		return tea.Batch(d.reload(), d.ensureFiles())
	case prRunsMsg:
		return d.onRuns(msg)
	case requeuedMsg:
		return d.onRequeued(msg)
	case runWaitTickMsg:
		return d.onWaitTick(msg)
	case runWaitMsg:
		return d.onWait(msg)
	case runLoadedMsg, logMsg, runTickMsg:
		return d.onRunMsg(msg)
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
	if d.run != nil {
		return d.run.tree.typing || d.run.find.typing
	}
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
	if d.run != nil {
		return d.runKey(msg)
	}
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
		if p := d.selectedPath(); d.tab == tabFiles && p != "" {
			return openURL(d.fileURL(p), "opened "+p)
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
	d.backTab = nil
	if d.tab == tabFiles {
		return d.ensureFiles()
	}
	return nil
}

// closeRequested reports whether esc should leave the detail view; before
// that, esc backs out of whatever is open inside it.
func (d *detailModel) closeRequested(msg tea.KeyMsg) bool {
	if d.modal != nil || msg.String() != "esc" || d.typing() || d.run != nil {
		return false // a typed filter takes esc itself; a run goes back to the Overview
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
		case d.backTab != nil:
			d.tab, d.backTab = *d.backTab, nil
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
// act runs a write on the pull request as it is now, when the key is
// pressed: a reload landing before the request goes out can't change it.
func (d *detailModel) act(done string, leave bool, f func(ctx context.Context, pr ado.PullRequest) error) tea.Cmd {
	pr := d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return actionDoneMsg{prID: pr.ID, text: done, err: f(ctx, pr), leave: leave}
	}
}

func openURL(u, done string) tea.Cmd {
	return func() tea.Msg { return resultMsg(actions.OpenBrowser(u), done) }
}

// prCheckout opens the checkout dialog for a pull request's source branch.
func prCheckout(org string, pr ado.PullRequest) tea.Cmd {
	return requestCheckout(org, pr.Repository.Project.Name, pr.Repository.Name, pr.SourceBranch())
}

// forPR is a message answering a request about one pull request: the
// detail view drops it unless that is the pull request it shows, so a slow
// answer about one opened before can't replace what this one shows, or
// what its next vote or merge acts on.
type forPR interface{ pullRequest() int }

func (m detailLoadedMsg) pullRequest() int   { return m.prID }
func (m visitMsg) pullRequest() int          { return m.prID }
func (m actionDoneMsg) pullRequest() int     { return m.prID }
func (m targetBranchesMsg) pullRequest() int { return m.prID }
func (m targetChangedMsg) pullRequest() int  { return m.prID }
func (m parentsMsg) pullRequest() int        { return m.prID }
func (m filesLoadedMsg) pullRequest() int    { return m.prID }
func (m fileDiffMsg) pullRequest() int       { return m.prID }
