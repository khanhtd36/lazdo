package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The Files tab can show a run of the pull request's commits instead of a
// comparison of updates. Only an unbroken run has one diff (oldest's parent
// to the newest), so picking commits with gaps between them shows the gaps
// too. Every commit picked is the same as All changes.

type parentsMsg struct {
	counts map[string]int
	err    error
}

// commitIndex is id's position in the commit list, newest first.
func (d *detailModel) commitIndex(id string) int {
	for i, c := range d.data.Commits {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// commitRun is the run Files shows, as indexes into the commit list: lo is
// the newest commit, hi the oldest. ok is false for comparisons of updates.
func (d *detailModel) commitRun() (lo, hi int, ok bool) {
	cmp := d.files.cmp
	if d.data == nil || cmp.commit == "" || cmp.fromCommit != "" {
		return 0, 0, false
	}
	lo = d.commitIndex(cmp.commit)
	hi = lo
	if cmp.oldest != "" {
		hi = d.commitIndex(cmp.oldest)
	}
	return lo, hi, lo >= 0 && hi >= lo
}

// showCommits narrows Files to the commits lo..hi (newest..oldest); all of
// them is All changes.
func (d *detailModel) showCommits(lo, hi int) tea.Cmd {
	cs := d.data.Commits
	switch {
	case lo == 0 && hi == len(cs)-1:
		d.files.cmp = d.allChanges()
	case lo == hi:
		d.files.cmp = comparison{label: "Commit " + shortSHA(cs[lo].ID) + " · " + firstLine(cs[lo].Comment), commit: cs[lo].ID}
	default:
		d.files.cmp = comparison{
			label:  fmt.Sprintf("Commits %s..%s · %d of %d", shortSHA(cs[hi].ID), shortSHA(cs[lo].ID), hi-lo+1, len(cs)),
			commit: cs[lo].ID,
			oldest: cs[hi].ID,
		}
	}
	d.resetTree()
	d.rebuildLists() // the Commits tab marks what Files shows
	return tea.Batch(d.ensureFiles(), d.ensureParents())
}

// runMerge reports whether the shown run holds a merge commit, whose diff
// brings in changes of the branch it merged.
func (d *detailModel) runMerge() bool {
	lo, hi, ok := d.commitRun()
	if !ok {
		return false
	}
	for _, c := range d.data.Commits[lo : hi+1] {
		if d.parents[c.ID] > 1 {
			return true
		}
	}
	return false
}

// ensureParents fetches the parent counts of commits not seen yet.
func (d *detailModel) ensureParents() tea.Cmd {
	if d.data == nil || d.parentsLoading || d.standalone {
		return nil
	}
	var ids []string
	for _, c := range d.data.Commits {
		if _, ok := d.parents[c.ID]; !ok {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	d.parentsLoading = true
	client, pr := d.client, d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		counts, err := client.ParentCounts(ctx, pr, ids)
		return parentsMsg{counts: counts, err: err}
	}
}

// --- Commit picker ---

// commitPicker is the Compare menu's "Commits…" checklist. The marked
// commits and everything between them form the run.
type commitPicker struct {
	d      *detailModel
	marked map[string]bool
	cursor int
	top    int
}

const pickerRows = 15

func (d *detailModel) newCommitPicker() *commitPicker {
	p := &commitPicker{d: d, marked: map[string]bool{}}
	if lo, hi, ok := d.commitRun(); ok {
		p.marked[d.data.Commits[lo].ID] = true
		p.marked[d.data.Commits[hi].ID] = true
		p.cursor = lo
	}
	return p
}

// run is the span of the marked commits; ok is false with none marked.
func (p *commitPicker) run() (lo, hi int, ok bool) {
	lo, hi = -1, -1
	for i, c := range p.d.data.Commits {
		if p.marked[c.ID] {
			if lo < 0 {
				lo = i
			}
			hi = i
		}
	}
	return lo, hi, lo >= 0
}

func (p *commitPicker) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	cs := p.d.data.Commits
	last := len(cs) - 1
	switch msg.String() {
	case "esc":
		return nil, nil
	case "j", "down":
		p.cursor = min(p.cursor+1, last)
	case "k", "up":
		p.cursor = max(p.cursor-1, 0)
	case "ctrl+d", "pgdown":
		p.cursor = min(p.cursor+pickerRows/2, last)
	case "ctrl+u", "pgup":
		p.cursor = max(p.cursor-pickerRows/2, 0)
	case "g", "home":
		p.cursor = 0
	case "G", "end":
		p.cursor = last
	case " ", "space":
		id := cs[p.cursor].ID
		p.marked[id] = !p.marked[id]
	case "a":
		if _, _, some := p.run(); some {
			p.marked = map[string]bool{}
		} else {
			for _, c := range cs {
				p.marked[c.ID] = true
			}
		}
	case "enter":
		lo, hi, ok := p.run()
		if !ok {
			return p, statusCmd("mark commits with space, or a for all")
		}
		return nil, p.d.showCommits(lo, hi)
	}
	return p, nil
}

func (p *commitPicker) view(width int) string {
	cs := p.d.data.Commits
	w := max(30, min(width-8, 100))
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+pickerRows {
		p.top = p.cursor - pickerRows + 1
	}
	lo, hi, some := p.run()
	lines := []string{
		styleSection.Render("Commits") + styleDim.Render("  marked commits and all between them"),
		"",
	}
	for i := p.top; i < min(len(cs), p.top+pickerRows); i++ {
		c := cs[i]
		prefix := "  "
		if i == p.cursor {
			prefix = styleSelected.Render("› ")
		}
		box, note := "[ ]", ""
		switch {
		case p.marked[c.ID]:
			box = styleGreen.Render("[x]")
		case some && i > lo && i < hi:
			box, note = styleGreen.Render("[~]"), styleDim.Render(" (included)")
		}
		if p.d.parents[c.ID] > 1 {
			note += styleYellow.Render(" merge")
		}
		text := shortSHA(c.ID) + " " + firstLine(c.Comment)
		if p.d.parents[c.ID] > 1 {
			text = styleDim.Render(text)
		}
		lines = append(lines, prefix+box+" "+truncate(text, w-ansi.StringWidth(note)-6)+note)
	}
	if len(cs) > pickerRows {
		lines = append(lines, styleDim.Render(fmt.Sprintf("  %d–%d of %d", p.top+1, min(len(cs), p.top+pickerRows), len(cs))))
	}
	status := "none marked"
	if some {
		status = fmt.Sprintf("%d of %d commits", hi-lo+1, len(cs))
		if hi-lo+1 == len(cs) {
			status = "all commits: All changes"
		}
	}
	lines = append(lines, "", styleDim.Render(status+" · space mark · a all/none · enter show · esc cancel"))
	return styleModal.Render(strings.Join(lines, "\n"))
}
