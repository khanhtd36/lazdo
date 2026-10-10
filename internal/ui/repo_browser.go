package ui

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

type browserPane int

const (
	paneBranches browserPane = iota
	paneFiles
	paneContent
)

const (
	browserBranchesWidth = 28
	browserTreeWidth     = 36
	browserFoldWidth     = 140 // below this the branch column folds away
)

// fileContent is one file at one branch, ready to show.
type fileContent struct {
	err      error
	binary   bool
	tooLarge bool
	raw      []string
	hl       *hlText
	markdown bool
}

// repoBrowser shows a repo: branches, a lazily loaded file tree, and the
// selected file's content.
type repoBrowser struct {
	client  *ado.Client
	project ado.ProjectInfo
	repo    ado.Repo
	branch  string

	branches       pickList
	branchesLoaded bool

	folders      map[string][]ado.RepoItem // branch|folder → children; nil while loading
	expanded     map[string]bool           // open folders, kept across branches
	index        map[string][]ado.RepoItem // branch → every item, for go-to-file
	indexLoading map[string]bool
	tree         pickList
	jumpTo       string // file to select once a go-to-file jump's folders load

	file     string
	contents map[string]*fileContent // branch|path; nil while loading
	rendered struct {
		key  string
		rows []string
		src  []int // source line of each row; -1 for rendered markdown
		part []int // the row's place among its source line's rows
		nw   int   // line number width
		cw   int   // source text width
	}
	top         int
	cur         int // cursor row in the content pane
	anchor      int // first row of a V selection, -1 when none
	dragFrom    int // row a mouse drag started on
	markdownRaw bool
	md          *markdownCache

	search    string
	searching bool
	matches   []int // rows that contain the search
	match     int

	pane         browserPane
	hideBranches bool
	err          error

	tab  repoTab
	hist history
	prs  []ado.PullRequest // the project's active PRs, to warn before deleting a branch
}

type (
	folderMsg struct {
		repoID, branch, folder string
		items                  []ado.RepoItem
		err                    error
	}
	indexMsg struct {
		repoID, branch string
		items          []ado.RepoItem
		err            error
	}
	contentMsg struct {
		repoID, branch, path string
		content              *fileContent
	}
)

func newRepoBrowser(client *ado.Client, p ado.ProjectInfo, r ado.Repo) *repoBrowser {
	return &repoBrowser{
		client: client, project: p, repo: r, branch: r.DefaultBranchName(),
		folders: map[string][]ado.RepoItem{}, expanded: map[string]bool{},
		index: map[string][]ado.RepoItem{}, indexLoading: map[string]bool{},
		contents: map[string]*fileContent{}, md: newMarkdownCache(),
		pane: paneFiles, anchor: -1,
		hist: history{tagInfo: map[string]*ado.TagInfo{}},
	}
}

func (b *repoBrowser) init() tea.Cmd {
	if b.branch == "" {
		return nil // empty repo
	}
	return tea.Batch(b.loadBranches(), b.loadFolder("/"))
}

func folderKey(branch, folder string) string { return branch + "|" + folder }

func (b *repoBrowser) loadBranches() tea.Cmd {
	client, r := b.client, b.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		bs, err := client.Branches(ctx, r)
		return branchesMsg{repoID: r.ID, branches: bs, err: err}
	}
}

func (b *repoBrowser) loadFolder(folder string) tea.Cmd {
	key := folderKey(b.branch, folder)
	if _, ok := b.folders[key]; ok {
		return nil
	}
	b.folders[key] = nil
	client, r, branch := b.client, b.repo, b.branch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		items, err := client.Items(ctx, r, branch, folder, false)
		return folderMsg{repoID: r.ID, branch: branch, folder: folder, items: items, err: err}
	}
}

// loadIndex fetches every path once per branch so / can find any file.
func (b *repoBrowser) loadIndex() tea.Cmd {
	if b.index[b.branch] != nil || b.indexLoading[b.branch] {
		return nil
	}
	b.indexLoading[b.branch] = true
	client, r, branch := b.client, b.repo, b.branch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		items, err := client.Items(ctx, r, branch, "/", true)
		return indexMsg{repoID: r.ID, branch: branch, items: items, err: err}
	}
}

func (b *repoBrowser) loadContent(p string) tea.Cmd {
	key := folderKey(b.branch, p)
	if _, ok := b.contents[key]; ok {
		return nil
	}
	b.contents[key] = nil
	client, r, branch := b.client, b.repo, b.branch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return contentMsg{repoID: r.ID, branch: branch, path: p, content: loadFileContent(ctx, client, r, branch, p)}
	}
}

func loadFileContent(ctx context.Context, client *ado.Client, r ado.Repo, branch, p string) *fileContent {
	data, err := client.FileContent(ctx, r, branch, p)
	fc := &fileContent{err: err}
	switch {
	case err != nil:
		return fc
	case ado.IsBinary(data):
		fc.binary = true
		return fc
	}
	fc.raw = splitLines(string(data))
	if len(fc.raw) > maxDiffLines {
		fc.tooLarge = true
		return fc
	}
	fc.markdown = strings.EqualFold(path.Ext(p), ".md")
	fc.hl = highlight(p, string(data))
	return fc
}

func (b *repoBrowser) update(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case commitsMsg, releaseMsg, tagsMsg, tagInfoMsg, refsChangedMsg:
		return b.historyUpdate(msg)
	}
	switch msg := msg.(type) {
	case branchesMsg:
		if msg.repoID == b.repo.ID {
			b.branchesLoaded, b.err = true, msg.err
			b.branches.setItems(b.branchItems(msg.branches))
		}
	case folderMsg:
		if msg.repoID != b.repo.ID {
			return nil
		}
		if msg.err != nil {
			b.err = msg.err
			delete(b.folders, folderKey(msg.branch, msg.folder))
			return nil
		}
		b.folders[folderKey(msg.branch, msg.folder)] = msg.items
		b.rebuildTree()
		if b.jumpTo != "" && b.selectPath(b.jumpTo) {
			b.jumpTo = "" // the jumped-to file is visible now
		}
	case indexMsg:
		if msg.repoID == b.repo.ID {
			b.indexLoading[msg.branch] = false
			if msg.err == nil {
				b.index[msg.branch] = msg.items
				b.rebuildTree()
			}
		}
	case contentMsg:
		if msg.repoID == b.repo.ID {
			b.contents[folderKey(msg.branch, msg.path)] = msg.content
			b.rendered.key = ""
		}
	}
	return nil
}

func (b *repoBrowser) branchItems(bs []ado.Branch) []pickItem {
	sort.SliceStable(bs, func(i, j int) bool {
		if bs[i].IsBaseVersion != bs[j].IsBaseVersion {
			return bs[i].IsBaseVersion
		}
		return bs[i].Commit.Author.Date.After(bs[j].Commit.Author.Date)
	})
	items := make([]pickItem, 0, len(bs))
	for _, br := range bs {
		items = append(items, pickItem{
			search: br.Name,
			value:  br,
			render: func(width int) string { return b.branchCell(br, width) },
		})
	}
	return items
}

func (b *repoBrowser) branchCell(br ado.Branch, width int) string {
	name := br.Name
	if name == b.branch {
		name = styleSelected.Render("● ") + styleTitle.Render(name)
	}
	ab := ""
	if !br.IsBaseVersion {
		ab = styleGreen.Render(fmt.Sprintf("↑%d", br.AheadCount)) + styleRed.Render(fmt.Sprintf("↓%d", br.BehindCount))
	}
	return truncate(joinCols(name, ab), width)
}

// --- Tree ---

// rebuildTree lists the open folders depth-first, plus every known file as
// a search-only row so / can jump to files in folders never opened.
func (b *repoBrowser) rebuildTree() {
	var items []pickItem
	var walk func(folder string, depth int)
	walk = func(folder string, depth int) {
		children, loaded := b.folders[folderKey(b.branch, folder)]
		if !loaded {
			return
		}
		if children == nil {
			items = append(items, pickItem{render: func(int) string {
				return strings.Repeat("  ", depth) + styleDim.Render("loading…")
			}, header: true})
			return
		}
		sorted := append([]ado.RepoItem(nil), children...)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].IsFolder != sorted[j].IsFolder {
				return sorted[i].IsFolder
			}
			return strings.ToLower(sorted[i].Path) < strings.ToLower(sorted[j].Path)
		})
		for _, it := range sorted {
			items = append(items, pickItem{value: it, render: func(width int) string { return b.treeRow(it, depth, width) }})
			if it.IsFolder && b.expanded[it.Path] {
				walk(it.Path, depth+1)
			}
		}
	}
	walk("/", 0)

	for _, it := range b.searchable() {
		items = append(items, pickItem{
			searchOnly: true,
			search:     strings.TrimPrefix(it.Path, "/"),
			value:      it,
			render: func(width int) string {
				dir, name := path.Split(it.Path)
				return truncate(styleDim.Render(strings.TrimPrefix(dir, "/"))+name, width)
			},
		})
	}
	b.tree.setItems(items)
}

// searchable is every file / can find: the full index once loaded, else
// the files in the folders opened so far.
func (b *repoBrowser) searchable() []ado.RepoItem {
	var out []ado.RepoItem
	if all := b.index[b.branch]; all != nil {
		for _, it := range all {
			if !it.IsFolder {
				out = append(out, it)
			}
		}
		return out
	}
	prefix := b.branch + "|"
	for key, children := range b.folders {
		if strings.HasPrefix(key, prefix) {
			for _, it := range children {
				if !it.IsFolder {
					out = append(out, it)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (b *repoBrowser) treeRow(it ado.RepoItem, depth int, width int) string {
	name := path.Base(it.Path)
	indent := strings.Repeat("  ", depth)
	if it.IsFolder {
		arrow := "▸ "
		if b.expanded[it.Path] {
			arrow = "▾ "
		}
		return truncate(indent+styleDim.Render(arrow)+styleHeader.Render(name), width)
	}
	if it.Path == b.file {
		name = styleSelected.Render(name)
	}
	return truncate(indent+"  "+name, width)
}

func (b *repoBrowser) selectedItem() (ado.RepoItem, bool) {
	it, ok := b.tree.selected()
	if !ok {
		return ado.RepoItem{}, false
	}
	item, isItem := it.value.(ado.RepoItem)
	return item, isItem
}

// openItem expands or collapses a folder, or shows a file.
func (b *repoBrowser) openItem(it ado.RepoItem, fromSearch bool) tea.Cmd {
	if it.IsFolder {
		b.expanded[it.Path] = !b.expanded[it.Path]
		cmd := b.loadFolder(it.Path)
		b.rebuildTree()
		return cmd
	}
	b.file, b.top, b.matches, b.search = it.Path, 0, nil, ""
	b.cur, b.anchor = 0, -1
	b.rendered.key = ""
	b.pane = paneContent
	cmds := []tea.Cmd{b.loadContent(it.Path)}
	if fromSearch {
		// Open the file's folders so the tree shows where it lives.
		b.tree.filter, b.tree.typing = "", false
		for dir := path.Dir(it.Path); dir != "/" && dir != "."; dir = path.Dir(dir) {
			b.expanded[dir] = true
			cmds = append(cmds, b.loadFolder(dir))
		}
	}
	b.rebuildTree()
	if fromSearch && !b.selectPath(it.Path) {
		b.jumpTo = it.Path // select it once its folders have loaded
	}
	return tea.Batch(cmds...)
}

// selectPath puts the tree cursor on a path if it is visible.
func (b *repoBrowser) selectPath(p string) bool {
	for i, idx := range b.tree.visible() {
		if it, ok := b.tree.items[idx].value.(ado.RepoItem); ok && it.Path == p {
			b.tree.cursor = i
			return true
		}
	}
	return false
}

// switchBranch shows the same folders and file on another branch.
func (b *repoBrowser) switchBranch(name string) tea.Cmd {
	if name == b.branch {
		return nil
	}
	b.branch, b.top, b.rendered.key = name, 0, ""
	b.cur, b.anchor = 0, -1
	cmds := []tea.Cmd{b.loadFolder("/")}
	for dir, open := range b.expanded {
		if open {
			cmds = append(cmds, b.loadFolder(dir))
		}
	}
	if b.file != "" {
		cmds = append(cmds, b.loadContent(b.file))
	}
	b.branches.setItems(b.branches.items) // re-render the current-branch marker
	b.rebuildTree()
	return tea.Batch(cmds...)
}

// --- Keys ---

func (b *repoBrowser) typing() bool {
	return b.branches.typing || b.tree.typing || b.searching || b.hist.commitList.typing || b.hist.tagList.typing
}

func (b *repoBrowser) foldBranches(width int) bool {
	return b.hideBranches || width < browserFoldWidth
}

// key handles the browser's keys; handled false lets the project act (esc).
func (b *repoBrowser) key(msg tea.KeyMsg, width, height int) (bool, tea.Cmd) {
	if b.searching {
		b.searchKey(msg, width, height)
		return true, nil
	}
	// A typed filter takes every key: letters are text, not commands.
	switch {
	case b.pane == paneBranches && b.branches.typing:
		return b.branchesKey(msg, height)
	case b.tab == repoTabFiles && b.pane == paneFiles && b.tree.typing:
		return b.treeKey(msg, width, height)
	case b.hist.commitList.typing || b.hist.tagList.typing:
		return b.historyKey(msg, width, height)
	}
	k := msg.String()
	switch k {
	case "1", "2", "3":
		b.tab = repoTab(k[0] - '1')
		if b.pane == paneContent || b.tab == repoTabTags {
			b.pane = paneFiles
		}
		return true, b.ensureHistory()
	}
	if b.tab != repoTabFiles {
		return b.historyKey(msg, width, height)
	}
	switch k {
	case "z":
		b.hideBranches = !b.hideBranches
		if b.hideBranches && b.pane == paneBranches {
			b.pane = paneFiles
		}
		return true, nil
	case "tab", "shift+tab":
		step := browserPane(1)
		if k == "shift+tab" {
			step = 2
		}
		b.pane = (b.pane + step) % 3
		if b.pane == paneBranches && b.foldBranches(width) {
			b.pane = (b.pane + step) % 3
		}
		return true, nil
	case "r":
		return true, b.refresh()
	case "o":
		return true, b.linkKey(k)
	case "y":
		if b.pane == paneContent {
			return b.contentKey(msg, width, height) // copies a selection first
		}
		return true, b.copySelection()
	case "c":
		br := b.branch
		if it, ok := b.branches.selected(); ok && b.pane == paneBranches {
			br = it.value.(ado.Branch).Name
		}
		return true, requestCheckout(b.client.Org, b.project.Name, b.repo.Name, br)
	}
	switch b.pane {
	case paneBranches:
		return b.branchesKey(msg, height)
	case paneFiles:
		return b.treeKey(msg, width, height)
	case paneContent:
		return b.contentKey(msg, width, height)
	}
	return false, nil
}

func (b *repoBrowser) refresh() tea.Cmd {
	delete(b.folders, folderKey(b.branch, "/"))
	if b.file != "" {
		delete(b.contents, folderKey(b.branch, b.file))
		b.rendered.key = ""
	}
	return tea.Batch(b.loadBranches(), b.loadFolder("/"), b.loadContent(b.file))
}

func (b *repoBrowser) linkKey(string) tea.Cmd {
	u := b.repo.WebURL + "?version=GB" + b.branch
	if b.file != "" && b.pane != paneBranches {
		u = b.client.FileURL(b.repo, b.branch, b.file)
	}
	return openURL(u, "opened in browser")
}

// copySelection opens the Copy menu for the branch or file in focus.
func (b *repoBrowser) copySelection() tea.Cmd {
	if b.pane == paneBranches {
		if it, ok := b.branches.selected(); ok {
			name := it.value.(ado.Branch).Name
			return copyMenu("branch "+name,
				copyItem{"Name", name},
				copyItem{"Web URL", b.client.BranchURL(b.repo, name)})
		}
		return nil
	}
	p := b.file
	if it, ok := b.selectedItem(); ok && b.pane == paneFiles {
		p = it.Path
	}
	if p == "" {
		return copyRepo(b.repo)
	}
	return copyMenu("file",
		copyItem{"Path", p},
		copyItem{"Web URL", b.client.FileURL(b.repo, b.branch, p)},
		copyItem{"File name", pathBase(p)})
}

func (b *repoBrowser) branchesKey(msg tea.KeyMsg, height int) (bool, tea.Cmd) {
	handled, activate := b.branches.key(msg, height)
	it, ok := b.branches.selected()
	if activate && ok {
		b.pane = paneFiles
		return true, b.switchBranch(it.value.(ado.Branch).Name)
	}
	if handled {
		return true, nil
	}
	switch msg.String() {
	case "l", "right", "esc":
		b.pane = paneFiles // esc from a side pane returns to the tree
		return true, nil
	case "d":
		return true, b.deleteBranchKey()
	}
	return false, nil
}

func (b *repoBrowser) treeKey(msg tea.KeyMsg, width, height int) (bool, tea.Cmd) {
	wasFiltering := b.tree.filter != ""
	if msg.String() == "/" && !b.tree.typing {
		b.tree.typing = true
		return true, b.loadIndex()
	}
	handled, activate := b.tree.key(msg, height)
	it, ok := b.selectedItem()
	if activate && ok {
		return true, b.openItem(it, wasFiltering)
	}
	if handled {
		return true, nil
	}
	switch msg.String() {
	case "l", "right":
		if ok && it.IsFolder && !b.expanded[it.Path] {
			return true, b.openItem(it, false)
		}
		if ok && !it.IsFolder {
			return true, b.openItem(it, false)
		}
		return true, nil
	case "h", "left":
		if ok && it.IsFolder && b.expanded[it.Path] {
			return true, b.openItem(it, false) // collapse
		}
		if !b.foldBranches(width) {
			b.pane = paneBranches
		}
		return true, nil
	}
	return false, nil
}

func (b *repoBrowser) contentKey(msg tea.KeyMsg, width, height int) (bool, tea.Cmd) {
	rows := b.contentRows(b.contentWidth(width))
	page := max(1, height/2)
	switch msg.String() {
	case "j", "down":
		b.cur++
	case "k", "up":
		b.cur--
	case "ctrl+d", "pgdown":
		b.cur += page
	case "ctrl+u", "pgup":
		b.cur -= page
	case "g", "home":
		b.cur = 0
	case "G", "end":
		b.cur = len(rows) - 1
	case "V":
		if b.anchor >= 0 {
			b.anchor = -1
		} else {
			b.anchor = b.cur
		}
		return true, nil
	case "y":
		if text := b.selectedText(width); text != "" {
			n := strings.Count(text, "\n") + 1
			b.anchor = -1
			return true, copyText(text, fmt.Sprintf("copied %d %s", n, plural(n, "line", "lines")))
		}
		return true, b.copySelection()
	case "/":
		b.searching, b.search = true, ""
		return true, nil
	case "n", "N", "p":
		b.jumpMatch(msg.String() == "n")
		return true, nil
	case "M":
		b.markdownRaw = !b.markdownRaw
		b.rendered.key, b.top, b.cur, b.anchor = "", 0, 0, -1
		return true, nil
	case "h", "left":
		b.pane = paneFiles
		return true, nil
	case "esc":
		switch {
		case b.anchor >= 0:
			b.anchor = -1
		case b.search != "":
			b.search, b.matches = "", nil
		default:
			b.pane = paneFiles // esc from a side pane returns to the tree
		}
		return true, nil
	default:
		return false, nil
	}
	b.cur = max(0, min(b.cur, len(rows)-1))
	b.followCursor(height)
	return true, nil
}

// followCursor scrolls the content so the cursor row stays visible (the
// pane shows height-1 rows below its heading).
func (b *repoBrowser) followCursor(height int) {
	visible := max(1, height-1)
	if b.cur < b.top {
		b.top = b.cur
	}
	if b.cur >= b.top+visible {
		b.top = b.cur - visible + 1
	}
}

func (b *repoBrowser) searchKey(msg tea.KeyMsg, width, height int) {
	switch msg.String() {
	case "esc":
		b.searching, b.search, b.matches = false, "", nil
	case "enter":
		b.searching = false
	case "backspace":
		if r := []rune(b.search); len(r) > 0 {
			b.search = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			s := string(msg.Runes)
			if s == "" {
				s = " "
			}
			b.search += s
		}
	}
	b.contentRows(b.contentWidth(width))
	b.findMatches()
	if len(b.matches) > 0 && b.searching {
		b.match = 0
		b.cur = b.matches[0]
		b.top = max(0, b.cur-height/3)
	}
}

func (b *repoBrowser) findMatches() {
	b.matches = nil
	if b.search == "" {
		return
	}
	q := strings.ToLower(b.search)
	for i := range b.rendered.rows {
		if strings.Contains(strings.ToLower(b.rowText(i)), q) {
			b.matches = append(b.matches, i)
		}
	}
}

func (b *repoBrowser) jumpMatch(forward bool) {
	if len(b.matches) == 0 {
		return
	}
	if forward {
		b.match = (b.match + 1) % len(b.matches)
	} else {
		b.match = (b.match + len(b.matches) - 1) % len(b.matches)
	}
	b.cur = b.matches[b.match]
	b.top = max(0, b.cur-5)
}
