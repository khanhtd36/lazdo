package ui

import (
	"fmt"
	"path"
	"strings"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// The Files tree's folders are rows: enter opens or closes one, l opens,
// h closes or goes up to the parent folder, like the repo browser's.

// selectedFolder is the folder under the tree's cursor, if a folder is.
func (d *detailModel) selectedFolder() (string, bool) {
	it, ok := d.files.tree.selected()
	if !ok {
		return "", false
	}
	line, _ := it.value.(listLine)
	return line.folder, line.folder != ""
}

// setFolder opens or closes a folder; the cursor stays on its row.
func (d *detailModel) setFolder(folder string, closed bool) {
	if closed {
		d.files.closed[folder] = true
	} else {
		delete(d.files.closed, folder)
	}
	d.rebuildTree()
}

// rebuildTree redraws the tree for opened or closed folders, the cursor
// kept on the same row.
func (d *detailModel) rebuildTree() {
	d.files.tree.refreshItems(lineItems(d.fileLines(d.files.changes)), func(was, is any) bool {
		a, _ := was.(listLine)
		b, _ := is.(listLine)
		if a.folder != "" || b.folder != "" {
			return a.folder == b.folder
		}
		return a.change != nil && b.change != nil && a.change.Item.Path == b.change.Item.Path
	})
}

// openFoldersOf opens the folders a file is in, so the tree shows it.
func (d *detailModel) openFoldersOf(p string) {
	opened := false
	for dir := path.Dir(p); dir != "/" && dir != "."; dir = path.Dir(dir) {
		if d.files.closed[dir] {
			delete(d.files.closed, dir)
			opened = true
		}
	}
	if opened {
		d.rebuildTree()
	}
}

// selectedPath is the path of the tree's selected file or folder.
func (d *detailModel) selectedPath() string {
	if folder, ok := d.selectedFolder(); ok {
		return folder
	}
	if ch := d.selectedChange(); ch != nil {
		return ch.Item.Path
	}
	return ""
}

// toParent moves the tree's cursor to the folder holding the selected row.
func (d *detailModel) toParent() bool {
	p, t := d.selectedPath(), &d.files.tree
	if p == "" {
		return false
	}
	vis := t.visible()
	for i := t.cursor - 1; i >= 0; i-- {
		if line, _ := t.items[vis[i]].value.(listLine); line.folder != "" && strings.HasPrefix(p, line.folder+"/") {
			t.cursor = i
			return true
		}
	}
	return false
}

// firstFile moves the tree's cursor to the first file it shows.
func (d *detailModel) firstFile() {
	t := &d.files.tree
	for i, idx := range t.visible() {
		if line, _ := t.items[idx].value.(listLine); line.change != nil {
			t.cursor = i
			return
		}
	}
}

// selectFile moves the tree's cursor to a file's row.
func (d *detailModel) selectFile(p string) {
	t := &d.files.tree
	for i, idx := range t.visible() {
		if line, _ := t.items[idx].value.(listLine); line.change != nil && line.change.Item.Path == p {
			t.cursor = i
			return
		}
	}
}

// folderKey opens and closes folders: enter switches, l opens, h closes
// or goes up a level. ok is false for a key it leaves to the tree.
func (d *detailModel) folderKey(key string) (ok bool) {
	folder, onFolder := d.selectedFolder()
	closed := d.files.closed[folder]
	switch key {
	case "enter":
		if onFolder {
			d.setFolder(folder, !closed)
			return true
		}
	case "l", "right":
		if onFolder {
			if closed {
				d.setFolder(folder, false)
			}
			return true
		}
	case "h", "left":
		if onFolder && !closed {
			d.setFolder(folder, true)
			return true
		}
		d.toParent()
		return true
	}
	return false
}

// folderSummary is the diff pane for a folder: the changed files in it,
// with their comment threads.
func (d *detailModel) folderSummary(folder string, h, width int) []string {
	var files []*ado.Change
	for i := range d.files.changes {
		if ch := &d.files.changes[i]; strings.HasPrefix(ch.Item.Path, folder+"/") {
			files = append(files, ch)
		}
	}
	out := []string{"  " + styleHeader.Render(folder) + styleDim.Render(fmt.Sprintf("  %d changed %s", len(files), plural(len(files), "file", "files"))), ""}
	for _, ch := range files {
		if len(out) >= h {
			break
		}
		threads := 0
		for _, t := range d.files.threads {
			if t.IsHuman() && t.ThreadContext != nil && t.ThreadContext.FilePath == ch.Item.Path {
				threads++
			}
		}
		line := "  " + changeGlyph(ch.ChangeType) + " " + strings.TrimPrefix(ch.Item.Path, folder+"/")
		if threads > 0 {
			line += styleDim.Render(fmt.Sprintf("  %d %s", threads, plural(threads, "comment", "comments")))
		}
		out = append(out, truncate(line, width))
	}
	return out
}
