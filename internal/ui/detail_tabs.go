package ui

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// listLine is one row of the Files tree, Commits or Conflicts tab; url is
// what enter opens in the browser, change and commit what it opens in lazdo.
type listLine struct {
	text   string
	url    string
	change *ado.Change
	commit string
	path   string // a conflict's file
}

// lineItems turns rows into list items; rows with nothing to open (folders,
// push groups, notes) become headers the cursor skips.
func lineItems(lines []listLine) []pickItem {
	items := make([]pickItem, 0, len(lines))
	for _, l := range lines {
		it := pickItem{value: l, render: func(int) string { return l.text }}
		switch {
		case l.change != nil:
			it.search = l.change.Item.Path
		case l.commit != "" || l.url != "":
			it.search = ansi.Strip(l.text)
		default:
			it.header = true
		}
		items = append(items, it)
	}
	return items
}

// rebuildLists refreshes the Commits and Conflicts lists from the data.
func (d *detailModel) rebuildLists() {
	if d.data == nil {
		return
	}
	d.lists[tabCommits].setItems(lineItems(d.commitLines()))
	d.lists[tabConflicts].setItems(lineItems(d.conflictLines()))
}

func (d *detailModel) listKey(msg tea.KeyMsg) tea.Cmd {
	l := &d.lists[d.tab]
	handled, activate := l.key(msg, d.bodyHeight())
	it, ok := l.selected()
	line, _ := it.value.(listLine)
	switch {
	case activate && ok && line.commit != "":
		return d.openCommitDiff(line.commit)
	case activate && ok && line.url != "":
		return openURL(line.url, "opened in browser")
	case handled:
		return nil
	}
	if msg.String() == "y" && ok {
		if line.commit != "" {
			return d.copyCommit(line.commit)
		}
		return copyMenu("conflict", copyItem{"Path", line.path})
	}
	return nil
}

func (d *detailModel) copyCommit(id string) tea.Cmd {
	msg := ""
	for _, c := range d.data.Commits {
		if c.ID == id {
			msg = firstLine(c.Comment)
		}
	}
	return copyMenu("commit "+shortSHA(id),
		copyItem{"Commit ID", id},
		copyItem{"Short ID", shortSHA(id)},
		copyItem{"Message", msg},
		copyItem{"Web URL", d.commitURL(id)},
	)
}

func (d *detailModel) renderList() string {
	h := d.bodyHeight()
	lines := d.lists[d.tab].view(max(1, d.width-5), max(1, h-2))
	return strings.Join(framePane(lines, d.width-1, h, true), "\n")
}

// --- Files ---

type fileNode struct {
	name     string
	children map[string]*fileNode
	change   *ado.Change
}

func (d *detailModel) fileLines(changes []ado.Change) []listLine {
	root := &fileNode{children: map[string]*fileNode{}}
	for i := range changes {
		ch := &changes[i]
		node := root
		parts := strings.Split(strings.Trim(ch.Item.Path, "/"), "/")
		for _, p := range parts {
			next, ok := node.children[p]
			if !ok {
				next = &fileNode{name: p, children: map[string]*fileNode{}}
				node.children[p] = next
			}
			node = next
		}
		node.change = ch
	}
	lines := []listLine{{text: styleSection.Render(d.pr.Repository.Name) + styleDim.Render(fmt.Sprintf("  %d %s",
		len(changes), plural(len(changes), "file", "files")))}}
	d.appendTree(&lines, root, 0)
	return lines
}

func (d *detailModel) appendTree(lines *[]listLine, node *fileNode, depth int) {
	names := make([]string, 0, len(node.children))
	for n := range node.children {
		names = append(names, n)
	}
	// Folders first, then files, each alphabetically, like the web tree.
	sort.Slice(names, func(i, j int) bool {
		a, b := node.children[names[i]], node.children[names[j]]
		if (a.change == nil) != (b.change == nil) {
			return a.change == nil
		}
		return names[i] < names[j]
	})
	indent := strings.Repeat("  ", depth)
	for _, n := range names {
		child := node.children[n]
		if child.change != nil {
			*lines = append(*lines, listLine{
				text:   indent + changeGlyph(child.change.ChangeType) + " " + child.name,
				url:    d.fileURL(child.change.Item.Path),
				change: child.change,
			})
			continue
		}
		// Collapse single-folder chains: "Common/CSupperDB".
		label := child.name
		for len(child.children) == 1 {
			var only *fileNode
			for _, c := range child.children {
				only = c
			}
			if only.change != nil {
				break
			}
			label += "/" + only.name
			child = only
		}
		*lines = append(*lines, listLine{text: indent + styleDim.Render("▾ ") + styleHeader.Render(label)})
		d.appendTree(lines, child, depth+1)
	}
}

func changeGlyph(changeType string) string {
	switch {
	case strings.Contains(changeType, "add"):
		return styleGreen.Render("A")
	case strings.Contains(changeType, "delete"):
		return styleRed.Render("D")
	case strings.Contains(changeType, "rename"):
		return styleCyan.Render("R")
	}
	return styleYellow.Render("M")
}

func (d *detailModel) fileURL(path string) string {
	if d.standalone {
		return d.repo.WebURL + "?path=" + url.QueryEscape(path) + "&version=GC" + d.files.cmp.commit
	}
	return d.pr.WebURL(d.client.Org) + "?_a=files&path=" + url.QueryEscape(path)
}

// --- Commits ---

// commitLines groups commits by the push that brought them, newest first.
// Push threads list their new commits; whatever no push claims came with the
// pull request when it was created.
func (d *detailModel) commitLines() []listLine {
	byID := map[string]ado.Commit{}
	for _, c := range d.data.Commits {
		byID[c.ID] = c
	}
	type group struct {
		title string
		ids   []string
	}
	var pushes []*ado.Thread
	for i := range d.data.Threads {
		if t := &d.data.Threads[i]; t.Kind() == "RefUpdate" {
			pushes = append(pushes, t)
		}
	}
	sort.Slice(pushes, func(i, j int) bool { return pushes[i].PublishedDate.Before(pushes[j].PublishedDate) })

	claimed := map[string]bool{}
	groups := make([]group, 0, len(pushes)+1)
	for i, t := range pushes {
		ids := strings.Split(t.Prop("CodeReviewRefNewCommits"), ";")
		for _, id := range ids {
			claimed[id] = true
		}
		who := t.PropIdentity("CodeReviewRefUpdatedByIdentity").DisplayName
		groups = append(groups, group{
			title: fmt.Sprintf("Push %d · %s · %s", i+2, who, relTime(time.Since(t.PublishedDate), t.PublishedDate)),
			ids:   ids,
		})
	}
	var initial []string
	for _, c := range d.data.Commits {
		if !claimed[c.ID] {
			initial = append(initial, c.ID)
		}
	}
	groups = append([]group{{
		title: fmt.Sprintf("Push 1 · %s · %s", d.pr.CreatedBy.DisplayName, relTime(time.Since(d.pr.CreationDate), d.pr.CreationDate)),
		ids:   initial,
	}}, groups...)

	// ● marks the commits Files shows when it is narrowed to a run.
	shown := map[string]bool{}
	if lo, hi, ok := d.commitRun(); ok {
		for _, c := range d.data.Commits[lo : hi+1] {
			shown[c.ID] = true
		}
	}
	var lines []listLine
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		lines = append(lines, listLine{text: styleHeader.Render(g.title)})
		for _, id := range g.ids {
			c, ok := byID[id]
			if !ok {
				continue
			}
			mark := "  "
			if shown[id] {
				mark = styleCyan.Render("●") + " "
			}
			lines = append(lines, listLine{
				text: mark + styleDim.Render(shortSHA(id)) + " " + firstLine(c.Comment) +
					styleDim.Render("  "+c.Author.Name+" · "+relTime(time.Since(c.Author.Date), c.Author.Date)),
				url:    d.commitURL(id),
				commit: id,
			})
		}
	}
	return lines
}

func (d *detailModel) commitURL(id string) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s/commit/%s",
		url.PathEscape(d.client.Org), url.PathEscape(d.pr.Repository.Project.Name), url.PathEscape(d.pr.Repository.Name), id)
}

// --- Conflicts ---

func (d *detailModel) conflictLines() []listLine {
	if d.data.MergeStatus != "conflicts" {
		return []listLine{{text: styleGreen.Render("✓") + " No merge conflicts"}}
	}
	lines := []listLine{{text: styleRed.Render(fmt.Sprintf("%d conflicting %s", len(d.data.Conflicts), plural(len(d.data.Conflicts), "file", "files"))) +
		styleDim.Render(" · resolve locally (merge "+d.pr.TargetBranch()+" into "+d.pr.SourceBranch()+") or in the browser")}}
	for _, c := range d.data.Conflicts {
		lines = append(lines, listLine{text: c.Path + styleDim.Render("  "+c.Type), url: d.pr.WebURL(d.client.Org) + "?_a=conflicts", path: c.Path})
	}
	return lines
}
