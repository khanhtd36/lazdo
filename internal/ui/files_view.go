package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

const (
	maxDiffLines    = 10000
	sideBySideWidth = 160
)

// comparison is what the Files tab diffs: push base against push target
// (base 0 is the merge base, i.e. all changes), or one commit against its
// parent.
type comparison struct {
	label  string
	target int
	base   int
	commit string
	// fromCommit, with commit, compares two commits (a tag range) instead
	// of one commit against its parent.
	fromCommit string
	// oldest, with commit, is a run of pull request commits: everything
	// from oldest's parent up to commit.
	oldest string
}

func (c comparison) key() string {
	return fmt.Sprintf("%d:%d:%s:%s:%s", c.target, c.base, c.commit, c.fromCommit, c.oldest)
}

type filePane int

const (
	paneTree filePane = iota
	paneDiff
)

type fileDiff struct {
	err              error
	binary, tooLarge bool
	leftRaw          []string
	rightRaw         []string
	leftHL, rightHL  [][]seg
	sbs, inline      []diffLine
}

type filesView struct {
	cmp       comparison
	loadedKey string // comparison the changes and threads below belong to
	loading   bool
	err       error

	baseCommit, targetCommit string
	changes                  []ado.Change
	threads                  []ado.Thread

	tree       pickList
	pane       filePane
	hideTree   bool
	modeChosen bool // the user toggled the mode; stop following the width
	sideBySide bool
	side       ado.Side

	diffs      map[string]*fileDiff // by comparison key + path; nil value = loading
	cursor     int
	top        int
	anchor     int // first line of a V range, -1 when none
	drag       int // line a mouse drag started on
	inThread   bool
	commentSel int
	// edge is the direction n (1) or N (-1) last stopped at the file's end
	// in; pressing it again moves to the next or previous file.
	edge int
	// land is where to put the cursor once the newly opened file's diff
	// loads: its first change (1) or last change (-1).
	land int
}

func newFilesView() filesView {
	return filesView{diffs: map[string]*fileDiff{}, anchor: -1}
}

type (
	filesLoadedMsg struct {
		key                      string
		baseCommit, targetCommit string
		changes                  []ado.Change
		threads                  []ado.Thread
		err                      error
	}
	fileDiffMsg struct {
		key  string
		diff *fileDiff
	}
)

func (d *detailModel) lastPush() int {
	if d.data == nil || len(d.data.Pushes) == 0 {
		return 0
	}
	return d.data.Pushes[len(d.data.Pushes)-1].ID
}

func (d *detailModel) push(id int) (ado.Push, bool) {
	for _, p := range d.data.Pushes {
		if p.ID == id {
			return p, true
		}
	}
	return ado.Push{}, false
}

func (d *detailModel) allChanges() comparison {
	return comparison{label: "All changes", target: d.lastPush()}
}

// ensureFiles loads the changed files of the current comparison if needed.
func (d *detailModel) ensureFiles() tea.Cmd {
	f := &d.files
	if d.data == nil || f.loading || (f.loadedKey == f.cmp.key() && f.cmp.target != 0) {
		return nil
	}
	if f.cmp.target == 0 && f.cmp.commit == "" {
		f.cmp = d.allChanges()
		if f.cmp.target == 0 {
			return nil
		}
	}
	f.loading = true
	client, pr, cmp := d.client, d.pr, f.cmp
	target, _ := d.push(cmp.target)
	base, hasBase := d.push(cmp.base)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		msg := filesLoadedMsg{key: cmp.key()}
		if cmp.commit != "" && cmp.fromCommit != "" {
			msg.baseCommit, msg.targetCommit = cmp.fromCommit, cmp.commit
			msg.changes, msg.err = client.RangeChanges(ctx, pr.AsRepo(), cmp.fromCommit, cmp.commit)
			return msg
		}
		if cmp.commit != "" && cmp.oldest != "" {
			msg.targetCommit = cmp.commit
			if msg.baseCommit, msg.err = client.CommitParent(ctx, pr, cmp.oldest); msg.err != nil {
				return msg
			}
			msg.changes, msg.err = client.RangeChanges(ctx, pr.AsRepo(), msg.baseCommit, cmp.commit)
			return msg
		}
		if cmp.commit != "" {
			msg.targetCommit = cmp.commit
			if msg.baseCommit, msg.err = client.CommitParent(ctx, pr, cmp.commit); msg.err != nil {
				return msg
			}
			msg.changes, msg.err = client.CommitChanges(ctx, pr, cmp.commit)
			return msg
		}
		msg.targetCommit = target.SourceRefCommit.CommitID
		msg.baseCommit = target.CommonRefCommit.CommitID
		if hasBase {
			msg.baseCommit = base.SourceRefCommit.CommitID
		}
		var wg sync.WaitGroup
		var errChanges, errThreads error
		wg.Go(func() { msg.changes, errChanges = client.IterationChanges(ctx, pr, cmp.target, cmp.base) })
		wg.Go(func() { msg.threads, errThreads = client.TrackedThreads(ctx, pr, cmp.target, cmp.base) })
		wg.Wait()
		msg.err = firstErr(errChanges, errThreads)
		return msg
	}
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (d *detailModel) onFilesLoaded(msg filesLoadedMsg) tea.Cmd {
	f := &d.files
	f.loading = false
	if msg.key != f.cmp.key() {
		return d.ensureFiles() // the comparison changed while loading
	}
	f.err = msg.err
	f.loadedKey = msg.key
	f.baseCommit, f.targetCommit = msg.baseCommit, msg.targetCommit
	f.changes, f.threads = msg.changes, msg.threads
	// Folders are headers, so the cursor lands on (and stays on) files.
	f.tree.setItems(lineItems(d.fileLines(f.changes)))
	return d.ensureDiff()
}

func (d *detailModel) selectedChange() *ado.Change {
	it, ok := d.files.tree.selected()
	if !ok {
		return nil
	}
	line, _ := it.value.(listLine)
	return line.change
}

// resetDiffCursor starts the diff of a newly selected file at the top.
func (d *detailModel) resetDiffCursor() {
	f := &d.files
	f.cursor, f.top, f.anchor, f.inThread = 0, 0, -1, false
	f.edge, f.land = 0, 0
}

// resetTree forgets the tree position for a new comparison.
func (d *detailModel) resetTree() {
	d.files.tree = pickList{}
	d.resetDiffCursor()
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}

func (d *detailModel) diffKey(ch *ado.Change) string {
	return d.files.cmp.key() + "|" + ch.Item.Path
}

func (d *detailModel) currentDiff() (*ado.Change, *fileDiff) {
	ch := d.selectedChange()
	if ch == nil {
		return nil, nil
	}
	return ch, d.files.diffs[d.diffKey(ch)]
}

// ensureDiff downloads the selected file's two versions and line alignment.
func (d *detailModel) ensureDiff() tea.Cmd {
	ch := d.selectedChange()
	if ch == nil {
		return nil
	}
	key := d.diffKey(ch)
	if _, ok := d.files.diffs[key]; ok {
		return nil
	}
	d.files.diffs[key] = nil
	client, pr, change := d.client, d.pr, *ch
	base, target := d.files.baseCommit, d.files.targetCommit
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return fileDiffMsg{key: key, diff: loadFileDiff(ctx, client, pr, change, base, target)}
	}
}

func loadFileDiff(ctx context.Context, client *ado.Client, pr ado.PullRequest, ch ado.Change, base, target string) *fileDiff {
	leftID, rightID := ch.Item.OriginalObjectID, ch.Item.ObjectID
	originalPath := ch.OriginalPath
	if originalPath == "" {
		originalPath = ch.Item.Path
	}
	switch {
	case strings.Contains(ch.ChangeType, "add"):
		leftID, originalPath = "", ""
	case strings.Contains(ch.ChangeType, "delete"):
		rightID = ""
	}
	var (
		wg                   sync.WaitGroup
		left, right          []byte
		blocks               []ado.LineBlock
		errL, errR, errBlock error
	)
	if leftID != "" {
		wg.Go(func() { left, errL = client.Blob(ctx, pr, leftID) })
	}
	if rightID != "" {
		wg.Go(func() { right, errR = client.Blob(ctx, pr, rightID) })
	}
	wg.Go(func() { blocks, errBlock = client.FileDiff(ctx, pr, base, target, ch.Item.Path, originalPath) })
	wg.Wait()
	fd := &fileDiff{err: firstErr(errL, errR, errBlock)}
	if fd.err != nil {
		return fd
	}
	if ado.IsBinary(left) || ado.IsBinary(right) {
		fd.binary = true
		return fd
	}
	fd.leftRaw, fd.rightRaw = splitLines(string(left)), splitLines(string(right))
	if len(fd.leftRaw) > maxDiffLines || len(fd.rightRaw) > maxDiffLines {
		fd.tooLarge = true
		return fd
	}
	fd.leftHL = highlight(ch.Item.Path, string(left))
	fd.rightHL = highlight(ch.Item.Path, string(right))
	fd.sbs = sideBySideLines(blocks, len(fd.leftRaw), len(fd.rightRaw))
	fd.inline = inlineLines(fd.sbs)
	return fd
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// --- Layout ---

func (d *detailModel) treeWidth() int {
	if d.files.hideTree {
		return 0
	}
	return min(40, max(20, d.width*30/100))
}

func (d *detailModel) diffWidth() int {
	w := d.width - 1
	if tw := d.treeWidth(); tw > 0 {
		w -= tw + 1
	}
	return max(20, w)
}

func (d *detailModel) isSideBySide() bool {
	if d.files.modeChosen {
		return d.files.sideBySide
	}
	return d.width >= sideBySideWidth
}

func (d *detailModel) diffLines() []diffLine {
	_, fd := d.currentDiff()
	if fd == nil {
		return nil
	}
	if d.isSideBySide() {
		return fd.sbs
	}
	return fd.inline
}

// cursorSide is the file side the cursor is on: the chosen side in
// side-by-side mode, the line's own side inline.
func (d *detailModel) cursorSide(l diffLine) ado.Side {
	if d.isSideBySide() {
		return d.files.side
	}
	if l.right == 0 {
		return ado.SideLeft
	}
	return ado.SideRight
}

func lineOn(l diffLine, s ado.Side) int {
	if s == ado.SideLeft {
		return l.left
	}
	return l.right
}

// threadsAt returns the threads whose range ends on the given line.
func (d *detailModel) threadsAt(path string, s ado.Side, line int) []*ado.Thread {
	if line == 0 {
		return nil
	}
	var out []*ado.Thread
	for i := range d.files.threads {
		t := &d.files.threads[i]
		if !t.IsHuman() || t.ThreadContext == nil || t.ThreadContext.FilePath != path {
			continue
		}
		end := t.ThreadContext.RightFileEnd
		if s == ado.SideLeft {
			end = t.ThreadContext.LeftFileEnd
		}
		if end != nil && end.Line == line {
			out = append(out, t)
		}
	}
	return out
}

// lineThreads are the threads shown under a displayed line.
func (d *detailModel) lineThreads(path string, l diffLine) []*ado.Thread {
	if d.isSideBySide() {
		return append(d.threadsAt(path, ado.SideLeft, l.left), d.threadsAt(path, ado.SideRight, l.right)...)
	}
	if l.right > 0 {
		return d.threadsAt(path, ado.SideRight, l.right)
	}
	return d.threadsAt(path, ado.SideLeft, l.left)
}

func (d *detailModel) diffThread() (*ado.Thread, bool) {
	ch, _ := d.currentDiff()
	lines := d.diffLines()
	if ch == nil || d.files.cursor >= len(lines) {
		return nil, false
	}
	ts := d.lineThreads(ch.Item.Path, lines[d.files.cursor])
	if len(ts) == 0 {
		return nil, false
	}
	return ts[0], true
}
