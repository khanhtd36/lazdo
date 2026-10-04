package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

type repoTab int

const (
	repoTabFiles repoTab = iota
	repoTabCommits
	repoTabTags
	repoTabCount
)

func (t repoTab) title() string {
	switch t {
	case repoTabFiles:
		return "Files"
	case repoTabCommits:
		return "Commits"
	case repoTabTags:
		return "Tags"
	default:
		return "?"
	}
}

const commitPage = 100

// history is the Commits and Tags state of a repo browser.
type history struct {
	commitList     pickList
	commits        []ado.RepoCommit
	commitsFor     string // branch the list holds; "" until loaded
	commitsMore    bool
	commitsLoading bool
	release        *release // set while the list shows a tag's release changes

	tags        []ado.Tag // sorted newest version first
	tagsLoading bool
	tagsLoaded  bool
	tagList     pickList
	tagInfo     map[string]*ado.TagInfo // by name; nil while loading
	commitTags  map[string][]string     // commit ID → tag names
}

// release is a tag's release changes: the commits since the previous tag.
type release struct {
	tag, prev ado.Tag
	hasPrev   bool
}

type (
	commitsMsg struct {
		repoID, branch string
		skip           int
		commits        []ado.RepoCommit
		err            error
	}
	releaseMsg struct {
		repoID, tag string
		commits     []ado.RepoCommit
		err         error
	}
	tagsMsg struct {
		repoID string
		tags   []ado.Tag
		err    error
	}
	tagInfoMsg struct {
		repoID, name string
		info         ado.TagInfo
		err          error
	}
	// refsChangedMsg reports a tag or branch created or deleted.
	refsChangedMsg struct {
		repoID, text string
		err          error
	}
	// openRepoDiffMsg asks the root to open a commit or tag-range diff.
	openRepoDiffMsg struct {
		repo                ado.Repo
		label, from, commit string
	}
	// showModalMsg asks the root to show a dialog over the screen.
	showModalMsg struct{ modal modal }
)

func showModal(m modal) tea.Cmd { return func() tea.Msg { return showModalMsg{modal: m} } }

// --- Loading ---

func (b *repoBrowser) loadCommits(skip int) tea.Cmd {
	if b.hist.commitsLoading {
		return nil
	}
	b.hist.commitsLoading = true
	client, r, branch := b.client, b.repo, b.branch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cs, err := client.Commits(ctx, r, branch, skip, commitPage)
		return commitsMsg{repoID: r.ID, branch: branch, skip: skip, commits: cs, err: err}
	}
}

func (b *repoBrowser) loadTags() tea.Cmd {
	h := &b.hist
	if h.tagsLoading || h.tagsLoaded {
		return nil
	}
	h.tagsLoading = true
	client, r := b.client, b.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		tags, err := client.Tags(ctx, r)
		return tagsMsg{repoID: r.ID, tags: tags, err: err}
	}
}

// loadTagInfo fetches the selected annotated tag's message, once.
func (b *repoBrowser) loadTagInfo() tea.Cmd {
	t, ok := b.selectedTag()
	if !ok || !t.Annotated() {
		return nil
	}
	if _, seen := b.hist.tagInfo[t.Name]; seen {
		return nil
	}
	b.hist.tagInfo[t.Name] = nil
	client, r := b.client, b.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := client.TagInfo(ctx, r, t)
		return tagInfoMsg{repoID: r.ID, name: t.Name, info: info, err: err}
	}
}

func (b *repoBrowser) openRelease(t ado.Tag) tea.Cmd {
	rel := &release{tag: t}
	rel.prev, rel.hasPrev = b.previousTag(t)
	h := &b.hist
	h.release, h.commitList = rel, pickList{}
	b.tab, b.pane = repoTabCommits, paneFiles
	if !rel.hasPrev {
		return statusCmd("no earlier version tag; showing nothing to compare")
	}
	client, r := b.client, b.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cs, err := client.CommitsBetween(ctx, r, rel.prev.Name, t.Name)
		return releaseMsg{repoID: r.ID, tag: t.Name, commits: cs, err: err}
	}
}

// historyUpdate handles the Commits and Tags messages.
func (b *repoBrowser) historyUpdate(msg tea.Msg) tea.Cmd {
	h := &b.hist
	switch msg := msg.(type) {
	case commitsMsg:
		if msg.repoID != b.repo.ID || msg.branch != b.branch {
			return nil
		}
		h.commitsLoading = false
		if msg.err != nil {
			b.err = msg.err
			return nil
		}
		if msg.skip == 0 {
			h.commits = nil
		}
		h.commits = append(h.commits, msg.commits...)
		h.commitsFor, h.commitsMore = msg.branch, len(msg.commits) == commitPage
		if h.release == nil {
			cur := h.commitList.cursor
			h.commitList.setItems(b.commitItems(h.commits))
			h.commitList.cursor = cur
		}
	case releaseMsg:
		if msg.repoID == b.repo.ID && h.release != nil && h.release.tag.Name == msg.tag {
			b.err = msg.err
			h.commitList.setItems(b.commitItems(msg.commits))
		}
	case tagsMsg:
		if msg.repoID != b.repo.ID {
			return nil
		}
		h.tagsLoading = false
		if msg.err != nil {
			b.err = msg.err
			return nil
		}
		h.tagsLoaded = true
		h.tags = sortTags(msg.tags)
		h.commitTags = map[string][]string{}
		for _, t := range h.tags {
			h.commitTags[t.CommitID] = append(h.commitTags[t.CommitID], t.Name)
		}
		h.tagList.setItems(b.tagItems())
		if h.release == nil && h.commits != nil {
			cur := h.commitList.cursor
			h.commitList.setItems(b.commitItems(h.commits)) // now with tag badges
			h.commitList.cursor = cur
		}
		return b.loadTagInfo()
	case tagInfoMsg:
		if msg.repoID == b.repo.ID {
			info := msg.info
			if msg.err != nil {
				info = ado.TagInfo{Message: "(no message: " + msg.err.Error() + ")"}
			}
			h.tagInfo[msg.name] = &info
		}
	case refsChangedMsg:
		if msg.repoID != b.repo.ID {
			return nil
		}
		if msg.err != nil {
			return statusCmd("error: " + msg.err.Error())
		}
		h.tagsLoaded, h.tagInfo = false, map[string]*ado.TagInfo{}
		return tea.Batch(statusCmd(msg.text), b.loadTags(), b.loadBranches())
	}
	return nil
}

// ensureHistory loads what the current tab shows the first time it's seen.
func (b *repoBrowser) ensureHistory() tea.Cmd {
	switch b.tab {
	case repoTabCommits:
		cmds := []tea.Cmd{b.loadTags()} // for the tag badges
		if b.hist.commitsFor != b.branch && b.hist.release == nil {
			b.hist.commitList = pickList{}
			cmds = append(cmds, b.loadCommits(0))
		}
		return tea.Batch(cmds...)
	case repoTabTags:
		return b.loadTags()
	case repoTabFiles, repoTabCount:
	}
	return nil
}

// --- Tags by version ---

// version parses "v1.2.3-pre+build" into numbers and a pre-release flag;
// ok is false for names that aren't versions.
func version(name string) (nums []int, pre bool, ok bool) {
	s := strings.TrimPrefix(strings.TrimPrefix(name, "v"), "V")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	core := s
	if i := strings.IndexByte(s, '-'); i >= 0 {
		core, pre = s[:i], true
	}
	for _, p := range strings.Split(core, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false, false
		}
		nums = append(nums, n)
	}
	return nums, pre, len(nums) > 0
}

// tagNewer orders tags newest version first; a release beats its own
// pre-releases; names that aren't versions go last, A-Z.
func tagNewer(a, b string) bool {
	va, preA, okA := version(a)
	vb, preB, okB := version(b)
	switch {
	case okA != okB:
		return okA
	case !okA:
		return strings.ToLower(a) < strings.ToLower(b)
	}
	for i := range max(len(va), len(vb)) {
		x, y := 0, 0
		if i < len(va) {
			x = va[i]
		}
		if i < len(vb) {
			y = vb[i]
		}
		if x != y {
			return x > y
		}
	}
	if preA != preB {
		return !preA
	}
	return a > b // same core, both pre-releases: later build names sort later
}

func sortTags(tags []ado.Tag) []ado.Tag {
	sort.SliceStable(tags, func(i, j int) bool { return tagNewer(tags[i].Name, tags[j].Name) })
	return tags
}

// previousTag is the next older version tag after t.
func (b *repoBrowser) previousTag(t ado.Tag) (ado.Tag, bool) {
	for i, x := range b.hist.tags {
		if x.Name != t.Name {
			continue
		}
		for _, y := range b.hist.tags[i+1:] {
			if _, _, ok := version(y.Name); ok {
				return y, true
			}
		}
	}
	return ado.Tag{}, false
}

// --- Rows ---

func (b *repoBrowser) commitItems(cs []ado.RepoCommit) []pickItem {
	items := make([]pickItem, 0, len(cs))
	for _, c := range cs {
		items = append(items, pickItem{
			search: c.ID[:min(8, len(c.ID))] + " " + firstLine(c.Comment) + " " + c.Author.Name,
			value:  c,
			render: func(width int) string { return b.commitRow(c, width) },
		})
	}
	return items
}

func (b *repoBrowser) commitRow(c ado.RepoCommit, width int) string {
	tags := ""
	for _, name := range b.hist.commitTags[c.ID] {
		tags += styleCyan.Render("["+name+"]") + " "
	}
	counts := ""
	if n := c.ChangeCounts; n != nil {
		counts = styleGreen.Render(fmt.Sprintf("+%d", n["Add"])) + styleYellow.Render(fmt.Sprintf(" ~%d", n["Edit"])) + styleRed.Render(fmt.Sprintf(" -%d", n["Delete"]))
	}
	right := joinCols(styleDim.Render(fit(nameInitials(c.Author.Name), 4)),
		styleDim.Render(fit(relTime(time.Since(c.Author.Date), c.Author.Date), 9)), counts)
	left := styleDim.Render(shortSHA(c.ID)) + " " + tags + firstLine(c.Comment)
	return fitStyled(left, max(20, width-ansi.StringWidth(right)-2)) + "  " + right
}

func (b *repoBrowser) tagItems() []pickItem {
	items := make([]pickItem, 0, len(b.hist.tags))
	for _, t := range b.hist.tags {
		items = append(items, pickItem{
			search: t.Name,
			value:  t,
			render: func(width int) string {
				kind := styleDim.Render("lightweight")
				if t.Annotated() {
					kind = styleDim.Render("annotated")
				}
				return truncate(joinCols(styleTitle.Render(fit(t.Name, 40)), styleDim.Render(shortSHA(t.CommitID)), kind), width)
			},
		})
	}
	return items
}

func (b *repoBrowser) selectedTag() (ado.Tag, bool) {
	it, ok := b.hist.tagList.selected()
	t, isTag := it.value.(ado.Tag)
	return t, ok && isTag
}

func (b *repoBrowser) selectedCommit() (ado.RepoCommit, bool) {
	it, ok := b.hist.commitList.selected()
	c, isCommit := it.value.(ado.RepoCommit)
	return c, ok && isCommit
}
