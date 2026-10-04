package ui

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// listLine is one row of the Files tree, Commits or Conflicts tab; url is
// what enter opens in the browser, change and commit what it opens in lazdo.
type listLine struct {
	text   string
	url    string
	change *ado.Change
	commit string
}

func (d *detailModel) listLines() []listLine {
	if d.data == nil {
		return nil
	}
	switch d.tab {
	case tabFiles:
		return d.treeLines()
	case tabCommits:
		return d.commitLines()
	case tabConflicts:
		return d.conflictLines()
	case tabOverview, tabCount:
	}
	return nil
}

func (d *detailModel) listKey(msg tea.KeyMsg) tea.Cmd {
	lines := d.listLines()
	cur := &d.cursor[d.tab]
	switch msg.String() {
	case "j", "down":
		*cur++
	case "k", "up":
		*cur--
	case "g", "home":
		*cur = 0
	case "G", "end":
		*cur = len(lines) - 1
	case "enter":
		if *cur < len(lines) && lines[*cur].commit != "" {
			return d.openCommitDiff(lines[*cur].commit)
		}
		if *cur < len(lines) && lines[*cur].url != "" {
			return openURL(lines[*cur].url, "opened in browser")
		}
	}
	*cur = max(0, min(*cur, len(lines)-1))
	return nil
}

func (d *detailModel) renderList() string {
	lines := d.listLines()
	if len(lines) == 0 {
		return styleDim.Render("  nothing here")
	}
	h := d.bodyHeight()
	cur := d.cursor[d.tab]
	offset := max(0, cur-h+1)
	var b strings.Builder
	for i := offset; i < min(len(lines), offset+h); i++ {
		prefix := "  "
		if i == cur {
			prefix = styleSelected.Render("▌ ")
		}
		b.WriteString(truncate(prefix+lines[i].text, d.width-1) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
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

	var lines []listLine
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		lines = append(lines, listLine{text: styleHeader.Render(g.title)})
		for _, id := range g.ids {
			c, ok := byID[id]
			if !ok {
				continue
			}
			lines = append(lines, listLine{
				text: "  " + styleDim.Render(shortSHA(id)) + " " + firstLine(c.Comment) +
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
		lines = append(lines, listLine{text: c.Path + styleDim.Render("  "+c.Type), url: d.pr.WebURL(d.client.Org) + "?_a=conflicts"})
	}
	return lines
}
