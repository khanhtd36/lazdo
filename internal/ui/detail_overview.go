package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

const (
	sidebarWidth    = 34
	twoColumnsWidth = 120
)

var (
	styleSection  = lipgloss.NewStyle().Bold(true)
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleNew      = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14"))
)

type activityFilter int

const (
	filterEverything activityFilter = iota
	filterComments
	filterNew
	filterMine
	filterActive
	filterResolved
	filterCount
)

func (f activityFilter) title() string {
	switch f {
	case filterEverything:
		return "Show everything"
	case filterComments:
		return "All comments"
	case filterNew:
		return "What's new"
	case filterMine:
		return "My comments/replies"
	case filterActive:
		return "Active comments"
	case filterResolved:
		return "Resolved comments"
	default:
		return "?"
	}
}

// entry is one activity item: a thread, or the synthesized "created" event.
type entry struct {
	thread  *ado.Thread // nil for the created event
	date    time.Time
	isNew   bool
	isHuman bool
}

func (d *detailModel) entries(f activityFilter) []entry {
	if d.data == nil {
		return nil
	}
	var out []entry
	for i := range d.data.Threads {
		t := &d.data.Threads[i]
		e := entry{thread: t, date: t.PublishedDate, isNew: d.isNew(t), isHuman: t.IsHuman()}
		if d.matches(f, e) {
			out = append(out, e)
		}
	}
	if f == filterEverything {
		out = append(out, entry{date: d.pr.CreationDate})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].date.After(out[j].date) })
	return out
}

func (d *detailModel) matches(f activityFilter, e entry) bool {
	t := e.thread
	switch f {
	case filterEverything:
		return true
	case filterComments:
		return e.isHuman
	case filterNew:
		return e.isNew
	case filterMine:
		if !e.isHuman {
			return false
		}
		for _, c := range t.LiveComments() {
			if c.Author.ID == d.me.ID {
				return true
			}
		}
		return false
	case filterActive:
		return e.isHuman && !t.IsResolved()
	case filterResolved:
		return e.isHuman && t.IsResolved()
	case filterCount:
	}
	return false
}

// isNew follows the web UI: a thread is new when someone other than Me wrote
// or edited a comment in it after Me's previous visit.
func (d *detailModel) isNew(t *ado.Thread) bool {
	if d.prevVisit.IsZero() {
		return false
	}
	for _, c := range t.LiveComments() {
		if c.Author.ID != d.me.ID && c.LastContentDate.After(d.prevVisit) {
			return true
		}
	}
	return false
}

func (d *detailModel) selectedEntry() (entry, bool) {
	es := d.entries(d.filter)
	if d.threadSel < 0 || d.threadSel >= len(es) {
		return entry{}, false
	}
	return es[d.threadSel], true
}

func (d *detailModel) overviewKey(msg tea.KeyMsg) tea.Cmd {
	if d.find.typing {
		d.findKey(msg)
		return nil
	}
	n := len(d.entries(d.filter))
	switch msg.String() {
	case "ctrl+d", "pgdown":
		d.vp.HalfPageDown()
	case "ctrl+u", "pgup":
		d.vp.HalfPageUp()
	case "g", "home":
		d.vp.GotoTop()
	case "G", "end":
		d.vp.GotoBottom()
	case "/":
		d.find = textFind{typing: true}
	case "n", "N", "p":
		if line, ok := d.find.next(msg.String() == "n"); ok {
			d.vp.SetYOffset(max(0, line-3))
		}
	case "y":
		if e, ok := d.selectedEntry(); ok && d.inThread && e.isHuman {
			return d.copyComment(*e.thread, d.commentSel)
		}
		return copyPR(d.client.Org, d.pr, d.checkLinks()...)
	case "J":
		d.threadSel, d.inThread = max(0, min(d.threadSel+1, n-1)), false
		d.rebuildOverview()
		d.scrollToEntry()
	case "K":
		d.threadSel, d.inThread = max(d.threadSel-1, 0), false
		d.rebuildOverview()
		d.scrollToEntry()
	case "f":
		d.modal = d.filterMenu()
	case "enter":
		if e, ok := d.selectedEntry(); ok && e.isHuman {
			d.inThread, d.commentSel = true, 0
			d.rebuildOverview()
			d.scrollToEntry()
		}
	case "j", "down":
		if d.inThread {
			return d.moveComment(1)
		}
		d.vp.ScrollDown(1)
	case "k", "up":
		if d.inThread {
			return d.moveComment(-1)
		}
		d.vp.ScrollUp(1)
	case "a":
		d.modal = d.newCommentEditor()
	case "R", "s", "e", "d":
		e, ok := d.selectedEntry()
		if !ok || !e.isHuman {
			return statusCmd("select a comment thread first (J/K)")
		}
		return d.threadKey(msg.String(), *e.thread, d.inThread, d.commentSel)
	}
	return nil
}

// filterMenu picks the activity filter, starting on the current one; each
// choice shows how many entries it keeps.
func (d *detailModel) filterMenu() modal {
	items := make([]menuItem, 0, filterCount)
	for f := range filterCount {
		mark := "  "
		if f == d.filter {
			mark = "✓ "
		}
		items = append(items, menuItem{
			label: fmt.Sprintf("%s%s (%d)", mark, f.title(), len(d.entries(f))),
			run: func() (modal, tea.Cmd) {
				d.filter, d.threadSel, d.inThread = f, 0, false
				d.rebuildOverview()
				return nil, nil
			},
		})
	}
	return &menuModal{title: "Show", items: items, cursor: int(d.filter)}
}

// findKey edits the Overview's / find box and jumps to the first match.
func (d *detailModel) findKey(msg tea.KeyMsg) {
	d.find.edit(msg)
	d.find.search(d.overviewLines())
	if line, ok := d.find.current(); ok {
		d.vp.SetYOffset(max(0, line-3))
	}
}

// overviewLines is the whole rendered Overview, one string per row.
func (d *detailModel) overviewLines() []string {
	return strings.Split(d.renderOverview(), "\n")
}

func (d *detailModel) moveComment(delta int) tea.Cmd {
	if e, ok := d.selectedEntry(); ok {
		n := len(e.thread.LiveComments())
		d.commentSel = max(0, min(d.commentSel+delta, n-1))
		d.rebuildOverview()
	}
	return nil
}

// threadKey runs reply (R), status (s), edit (e) or delete (d) on a thread;
// edit and delete act on the picked comment, so they need inThread.
func (d *detailModel) threadKey(key string, t ado.Thread, inThread bool, commentSel int) tea.Cmd {
	switch key {
	case "R":
		d.modal = d.replyEditor(t)
		return nil
	case "s":
		d.modal = d.threadStatusMenu(t)
		return nil
	}
	if !inThread {
		return statusCmd("press enter to step into the thread, then pick a comment (j/k)")
	}
	live := t.LiveComments()
	cm := live[min(commentSel, len(live)-1)]
	if cm.Author.ID != d.me.ID {
		return statusCmd("you can only change your own comments")
	}
	if key == "e" {
		d.modal = d.editCommentEditor(t, cm)
		return nil
	}
	d.modal = newConfirm("Delete this comment?", d.act("comment deleted", false, func(ctx context.Context) error {
		return d.client.DeleteComment(ctx, d.pr, t, cm)
	}))
	return nil
}

func statusCmd(s string) tea.Cmd { return func() tea.Msg { return statusMsg(s) } }

func (d *detailModel) scrollToEntry() {
	if d.threadSel >= 0 && d.threadSel < len(d.entryLines) {
		d.vp.SetYOffset(max(0, d.entryLines[d.threadSel]-2))
	}
}

// rebuildOverview re-renders the overview into the viewport, keeping the
// scroll position.
func (d *detailModel) rebuildOverview() {
	if d.width == 0 {
		return
	}
	y := d.vp.YOffset
	d.vp.SetContent(d.renderOverview())
	d.vp.SetYOffset(y)
}

func (d *detailModel) renderOverview() string {
	if d.data == nil {
		if d.err != nil {
			return styleRed.Render("error: " + d.err.Error())
		}
		return styleDim.Render("loading…")
	}
	mainWidth := d.width - 2
	if d.width >= twoColumnsWidth {
		mainWidth = d.width - sidebarWidth - 3
	}
	main := make([]string, 0, 64)
	main = append(main, d.renderChecks(mainWidth)...)
	main = append(main, "", styleSection.Render("Description"))
	desc := d.data.Description
	if strings.TrimSpace(desc) == "" {
		desc = "_No description._"
	}
	main = append(main, strings.Split(d.md.render(d.resolveMentions(desc), mainWidth), "\n")...)
	activityStart := len(main)
	activity, offsets := d.renderActivity(mainWidth)
	main = append(main, activity...)

	side := d.renderSidebar(sidebarWidth)
	if d.width < twoColumnsWidth {
		all := append(side, "")
		d.entryLines = shift(offsets, len(all)+activityStart)
		return strings.Join(append(all, main...), "\n")
	}
	d.entryLines = shift(offsets, activityStart)
	rows := make([]string, max(len(main), len(side)))
	for i := range rows {
		left, right := "", ""
		if i < len(main) {
			left = main[i]
		}
		if i < len(side) {
			right = side[i]
		}
		rows[i] = fit(left, mainWidth) + "  " + truncate(right, sidebarWidth)
	}
	return strings.Join(rows, "\n")
}

func shift(xs []int, by int) []int {
	out := make([]int, len(xs))
	for i, x := range xs {
		out[i] = x + by
	}
	return out
}

// --- Checks ---

const (
	policyTypeBuild            = "0609b952-1397-4640-95ec-e00a01b2c241"
	policyTypeMinReviewers     = "fa4e907d-c16b-4a4c-9dfa-4906e5d171dd"
	policyTypeRequiredReviewer = "fd2167ab-b0be-447a-8ec8-39368250530e"
	policyTypeMergeStrategy    = "fa4e907d-c16b-4a4c-9dfa-4916e5d171ab"
	policyTypeComments         = "c6a1889d-b943-4856-b76f-9e46bb6b0df2"
	policyTypeWorkItems        = "40e92b44-2fe1-4dd6-b3d8-74a9c21d0c6e"
)

func (d *detailModel) renderChecks(width int) []string {
	var required, optional []ado.Policy
	for _, p := range d.distinctPolicies(d.data.Policies) {
		if p.Configuration.Type.ID == policyTypeMergeStrategy {
			continue // satisfied by picking a merge type at completion
		}
		if p.Configuration.IsBlocking {
			required = append(required, p)
		} else {
			optional = append(optional, p)
		}
	}
	var out []string
	failed, pending := 0, 0
	for _, p := range required {
		switch p.Status {
		case "rejected", "broken":
			failed++
		case "approved":
		default:
			pending++
		}
	}
	switch {
	case failed > 0:
		out = append(out, styleRed.Render(fmt.Sprintf("✗ %d required %s failed", failed, plural(failed, "check", "checks"))))
	case pending > 0:
		out = append(out, styleYellow.Render(fmt.Sprintf("● %d required %s pending", pending, plural(pending, "check", "checks"))))
	default:
		out = append(out, styleGreen.Render("✓ Required checks succeeded"))
	}
	for _, p := range required {
		out = append(out, "  "+truncate(d.policyLine(p), width-2))
	}
	unrequired := d.data.UnrequiredStatuses()
	if len(optional) > 0 || len(unrequired) > 0 {
		out = append(out, styleDim.Render("Optional checks"))
		for _, p := range optional {
			out = append(out, "  "+truncate(d.policyLine(p), width-2))
		}
		for _, s := range unrequired {
			out = append(out, "  "+truncate(statusIcon(s.State)+" "+statusText(s, s.Key()), width-2))
		}
	}
	out = append(out, d.mergeLine())
	if by := d.data.AutoCompleteSetBy; by != nil && by.ID != "" {
		out = append(out, styleCyan.Render("⟳ Auto-complete set by "+by.DisplayName))
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func policyIcon(status string) string {
	switch status {
	case "approved":
		return styleGreen.Render("✓")
	case "rejected", "broken":
		return styleRed.Render("✗")
	case "running":
		return styleYellow.Render("●")
	default:
		return styleDim.Render("○")
	}
}

func (d *detailModel) policyLine(p ado.Policy) string {
	return policyIcon(p.Status) + " " + d.policyText(p)
}

// policyText composes the sentence the web UI shows for a policy.
func (d *detailModel) policyText(p ado.Policy) string {
	s := p.Configuration.Settings
	ok := p.Status == "approved"
	switch p.Configuration.Type.ID {
	case policyTypeBuild:
		name, _ := s["displayName"].(string)
		if name == "" {
			name = p.BuildName
		}
		if name == "" {
			name = "Build"
		}
		return name + ": build " + buildStatusText(p.Status)
	case policyTypeMinReviewers:
		n := intSetting(s, "minimumApproverCount")
		if ok {
			return fmt.Sprintf("At least %d %s approved", n, plural(n, "reviewer", "reviewers"))
		}
		return fmt.Sprintf("At least %d %s must approve", n, plural(n, "reviewer", "reviewers"))
	case policyTypeRequiredReviewer:
		names := d.reviewerNames(s["requiredReviewerIds"])
		if ok {
			return names + " approved"
		}
		return names + " must approve"
	case policyTypeComments:
		if ok {
			return "All comments resolved"
		}
		return "All comments must be resolved"
	case policyTypeWorkItems:
		if ok {
			return "Work items linked"
		}
		return "Work items must be linked"
	case ado.PolicyTypeStatus:
		return d.statusPolicyText(p)
	}
	return p.Configuration.Type.DisplayName + ": " + p.Status
}

// statusPolicyText reads like the web's Checks panel: the external
// service's latest description, then its state.
func (d *detailModel) statusPolicyText(p ado.Policy) string {
	name, _ := p.Configuration.Settings["defaultDisplayName"].(string)
	if name == "" {
		name = p.StatusKey()
	}
	s, ok := d.data.LatestStatus(p)
	if !ok {
		return name + styleDim.Render("  external · waiting for status")
	}
	return statusText(s, name)
}

func statusText(s ado.Status, fallback string) string {
	text := cmp.Or(s.Description, fallback)
	state := s.State
	switch state {
	case "pending":
		state = "running"
	case "notApplicable":
		state = "not applicable"
	}
	return text + styleDim.Render("  external · "+state)
}

// checkLinks are the external checks' runs (a GitHub Actions run, say), for
// the Overview's copy and open menus.
func (d *detailModel) checkLinks() []copyItem {
	if d.data == nil {
		return nil
	}
	var out []copyItem
	add := func(s ado.Status) {
		if s.TargetURL != "" {
			out = append(out, copyItem{"Check: " + cmp.Or(s.Description, s.Key()), s.TargetURL})
		}
	}
	for _, p := range d.data.Policies {
		if p.Configuration.Type.ID == ado.PolicyTypeStatus {
			if s, ok := d.data.LatestStatus(p); ok {
				add(s)
			}
		}
	}
	for _, s := range d.data.UnrequiredStatuses() {
		add(s)
	}
	return out
}

// openMenu opens the pull request, or asks which when it has external
// checks to open too.
func (d *detailModel) openMenu() tea.Cmd {
	pr := openURL(d.pr.WebURL(d.client.Org), fmt.Sprintf("opened !%d", d.pr.ID))
	links := d.checkLinks()
	if len(links) == 0 {
		return pr
	}
	items := []menuItem{{label: "Pull request", run: func() (modal, tea.Cmd) { return nil, pr }}}
	for _, l := range links {
		items = append(items, menuItem{label: l.label, run: func() (modal, tea.Cmd) {
			return nil, openURL(l.value, "opened "+l.label)
		}})
	}
	d.modal = &menuModal{title: "Open", items: items}
	return nil
}

// statusIcon is policyIcon for a status posted without a policy.
func statusIcon(state string) string {
	switch state {
	case "succeeded":
		return policyIcon("approved")
	case "failed", "error":
		return policyIcon("rejected")
	case "pending":
		return policyIcon("running")
	}
	return policyIcon("")
}

func buildStatusText(status string) string {
	switch status {
	case "approved":
		return "succeeded"
	case "rejected", "broken":
		return "failed"
	case "running":
		return "in progress"
	case "queued":
		return "queued"
	}
	return status
}

func intSetting(s map[string]any, key string) int {
	f, _ := s[key].(float64)
	return int(f)
}

func (d *detailModel) reviewerNames(ids any) string {
	list, _ := ids.([]any)
	names := make([]string, 0, len(list))
	for _, id := range list {
		s, _ := id.(string)
		name := "a required reviewer"
		for _, r := range d.data.Reviewers {
			if strings.EqualFold(r.ID, s) {
				name = r.DisplayName
			}
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "Required reviewers"
	}
	return strings.Join(names, ", ")
}

func (d *detailModel) mergeLine() string {
	switch d.data.MergeStatus {
	case "succeeded":
		s := styleGreen.Render("✓") + " No merge conflicts"
		if c := d.data.LastMergeCommit; c != nil && !c.Author.Date.IsZero() {
			s += styleDim.Render(" · last checked " + relTime(time.Since(c.Author.Date), c.Author.Date))
		}
		return s
	case "conflicts":
		return styleRed.Render("✗") + fmt.Sprintf(" Merge conflicts in %d %s (see Conflicts tab)", len(d.data.Conflicts), plural(len(d.data.Conflicts), "file", "files"))
	case "queued":
		return styleYellow.Render("●") + " Checking for merge conflicts"
	case "failure", "rejectedByPolicy":
		return styleRed.Render("✗") + " Merge failed: " + d.data.MergeFailure
	}
	return styleDim.Render("○ Merge status: " + d.data.MergeStatus)
}

// --- Activity ---

func (d *detailModel) renderActivity(width int) ([]string, []int) {
	es := d.entries(d.filter)
	header := fmt.Sprintf("Activity · %s (%d)", d.filter.title(), len(es))
	if d.filter == filterEverything {
		header = fmt.Sprintf("Activity · %s (%d)", d.filter.title(), len(es)-1)
	}
	out := []string{"", styleSection.Render(header) + styleDim.Render("   f filter · J/K thread · enter open thread · n comment")}
	offsets := make([]int, len(es))
	for i, e := range es {
		offsets[i] = len(out)
		selected := i == d.threadSel
		out = append(out, d.renderEntry(e, selected, width)...)
	}
	if len(es) == 0 {
		out = append(out, styleDim.Render("  nothing here"))
	}
	return out, offsets
}

func (d *detailModel) renderEntry(e entry, selected bool, width int) []string {
	marker := "  "
	if selected {
		marker = styleSelected.Render("▌ ")
	}
	date := styleDim.Render(relTime(time.Since(e.date), e.date))
	if e.thread == nil {
		return []string{marker + d.pr.CreatedBy.DisplayName + " created the pull request  " + date}
	}
	t := e.thread
	newTag := ""
	if e.isNew {
		newTag = " " + styleNew.Render(" new ")
	}
	if !e.isHuman {
		lines := []string{marker + truncate(d.systemText(t), width-20) + "  " + date + newTag}
		if t.Kind() == "RefUpdate" {
			lines = append(lines, d.pushedCommits(t, width)...)
		}
		return lines
	}

	status := styleYellow.Render(ado.ThreadStatusTitle(t.Status))
	if t.IsResolved() {
		status = styleGreen.Render(ado.ThreadStatusTitle(t.Status))
	}
	head := marker + threadMark() + status
	if t.ThreadContext != nil && t.ThreadContext.FilePath != "" {
		head += styleDim.Render("  " + t.ThreadContext.FilePath)
	}
	lines := []string{head + newTag}
	for i, c := range t.LiveComments() {
		indent := "    "
		if c.ParentCommentID != 0 && i > 0 {
			indent = "      "
		}
		cmark := indent
		if selected && d.inThread && i == d.commentSel {
			cmark = indent[:len(indent)-2] + styleSelected.Render("› ")
		}
		lines = append(lines, cmark+styleSection.Render(c.Author.DisplayName)+"  "+
			styleDim.Render(relTime(time.Since(c.PublishedDate), c.PublishedDate)))
		body := d.md.render(d.resolveMentions(c.Content), max(20, width-len(indent)))
		for _, l := range strings.Split(body, "\n") {
			lines = append(lines, indent+l)
		}
	}
	return lines
}

func (d *detailModel) pushedCommits(t *ado.Thread, width int) []string {
	byID := map[string]ado.Commit{}
	for _, c := range d.data.Commits {
		byID[c.ID] = c
	}
	var out []string
	for _, id := range strings.Split(t.Prop("CodeReviewRefNewCommits"), ";") {
		if c, ok := byID[id]; ok {
			out = append(out, "      "+styleDim.Render(shortSHA(id))+" "+truncate(firstLine(c.Comment), width-16))
		}
	}
	return out
}

func shortSHA(id string) string { return id[:min(8, len(id))] }

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// systemText renders a system thread the way the web activity feed does.
func (d *detailModel) systemText(t *ado.Thread) string {
	who := func(key string) string {
		if id := t.PropIdentity(key); id.DisplayName != "" {
			return id.DisplayName
		}
		return "Someone"
	}
	switch t.Kind() {
	case "RefUpdate":
		n := intProp(t, "CodeReviewRefNewCommitsCount")
		if n == 0 {
			return who("CodeReviewRefUpdatedByIdentity") + " updated the source branch"
		}
		return fmt.Sprintf("%s pushed %d %s", who("CodeReviewRefUpdatedByIdentity"), n, plural(n, "commit", "commits"))
	case "VoteUpdate":
		return who("CodeReviewVotedByIdentity") + " voted " + strings.ToLower(voteText(intProp(t, "CodeReviewVoteResult")))
	case "ReviewersUpdate":
		return d.reviewersUpdateText(t, who("CodeReviewReviewersUpdatedByIdentity"))
	case "PolicyStatusUpdate":
		return d.requiredReviewersText(t)
	case "StatusUpdate":
		by := who("CodeReviewStatusUpdatedByIdentity")
		switch t.Prop("CodeReviewStatus") {
		case "Completed":
			return by + " completed the pull request"
		case "Abandoned":
			return by + " abandoned the pull request"
		case "Active":
			return by + " reactivated the pull request"
		}
	case "IsDraftUpdate":
		if strings.EqualFold(t.Prop("CodeReviewIsDraftNowSet"), "true") {
			return who("CodeReviewIsDraftUpdatedByIdentity") + " marked the pull request as draft"
		}
		return who("CodeReviewIsDraftUpdatedByIdentity") + " published the pull request"
	case "AutoCompleteUpdate":
		if strings.EqualFold(t.Prop("CodeReviewAutoCompleteNowSet"), "true") {
			return who("CodeReviewAutoCompleteUpdatedByIdentity") + " set auto-complete"
		}
		return who("CodeReviewAutoCompleteUpdatedByIdentity") + " cancelled auto-complete"
	}
	return firstLine(t.LiveComments()[0].Content)
}

func intProp(t *ado.Thread, key string) int {
	var n int
	_, _ = fmt.Sscan(t.Prop(key), &n)
	return n
}

func (d *detailModel) reviewersUpdateText(t *ado.Thread, by string) string {
	if n := intProp(t, "CodeReviewReviewersUpdatedNumAdded"); n > 0 {
		added := t.PropIdentity("CodeReviewReviewersUpdatedAddedIdentity").DisplayName
		if n > 1 {
			added += fmt.Sprintf(" and %d others", n-1)
		}
		return by + " added " + added + " as " + plural(n, "a reviewer", "reviewers")
	}
	if intProp(t, "CodeReviewReviewersUpdatedNumChanged") > 0 {
		changed := t.PropIdentity("CodeReviewReviewersUpdatedChangedIdentity").DisplayName
		if strings.EqualFold(t.Prop("CodeReviewReviewersUpdatedChangedToRequired"), "true") {
			return by + " made " + changed + " a required reviewer"
		}
		return by + " made " + changed + " an optional reviewer"
	}
	if n := intProp(t, "CodeReviewReviewersUpdatedNumRemoved"); n > 0 {
		return fmt.Sprintf("%s removed %d %s", by, n, plural(n, "reviewer", "reviewers"))
	}
	return firstLine(t.LiveComments()[0].Content)
}

func (d *detailModel) requiredReviewersText(t *ado.Thread) string {
	var keys []string
	_ = json.Unmarshal([]byte(t.Prop("CodeReviewRequiredReviewerExampleReviewerIdentities")), &keys)
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		names = append(names, t.Identities[k].DisplayName)
	}
	if len(names) == 0 {
		return firstLine(t.LiveComments()[0].Content)
	}
	kind := "an optional reviewer"
	if strings.EqualFold(t.Prop("CodeReviewRequiredReviewerIsRequired"), "true") {
		kind = "a required reviewer"
	}
	verb := "was added as " + kind
	if len(names) > 1 {
		verb = "were added as " + strings.TrimPrefix(strings.TrimPrefix(kind, "a "), "an ") + "s"
	}
	text := strings.Join(names, " and ") + " " + verb
	if p := t.Prop("CodeReviewRequiredReviewerExamplePathThatTriggered"); p != "" {
		text += " for " + path.Base(p)
		if n := intProp(t, "CodeReviewRequiredReviewerNumFilesThatTriggered"); n > 1 {
			text += fmt.Sprintf(" and %d other files", n-1)
		}
	}
	return text
}

var mentionPattern = regexp.MustCompile(`@<([0-9A-Fa-f-]{36})>`)

// resolveMentions turns Azure DevOps "@<identity-id>" mentions into
// "@Display Name" for anyone on the pull request.
func (d *detailModel) resolveMentions(s string) string {
	if !strings.Contains(s, "@<") {
		return s
	}
	names := map[string]string{strings.ToLower(d.pr.CreatedBy.ID): d.pr.CreatedBy.DisplayName}
	for _, r := range d.data.Reviewers {
		names[strings.ToLower(r.ID)] = r.DisplayName
	}
	for _, t := range d.data.Threads {
		for _, c := range t.Comments {
			names[strings.ToLower(c.Author.ID)] = c.Author.DisplayName
		}
	}
	return mentionPattern.ReplaceAllStringFunc(s, func(m string) string {
		id := strings.ToLower(mentionPattern.FindStringSubmatch(m)[1])
		if name, ok := names[id]; ok {
			return "**@" + name + "**"
		}
		return "@someone"
	})
}

func voteText(v int) string {
	switch {
	case v >= ado.VoteApproved:
		return "Approved"
	case v >= ado.VoteApprovedWithSuggests:
		return "Approved with suggestions"
	case v <= ado.VoteRejected:
		return "Rejected"
	case v <= ado.VoteWaitingForAuthor:
		return "Waiting for author"
	}
	return "No review yet"
}

// --- Sidebar ---

func (d *detailModel) renderSidebar(width int) []string {
	var required, optional []ado.Reviewer
	for _, r := range d.data.Reviewers {
		if r.IsRequired {
			required = append(required, r)
		} else {
			optional = append(optional, r)
		}
	}
	out := []string{styleSection.Render("Reviewers")}
	for _, group := range []struct {
		title string
		rs    []ado.Reviewer
	}{{"Required", required}, {"Optional", optional}} {
		if len(group.rs) == 0 {
			continue
		}
		out = append(out, styleDim.Render(group.title))
		for _, r := range group.rs {
			out = append(out, "  "+voteGlyph(r.Vote)+" "+truncate(r.DisplayName, width-4))
			out = append(out, "    "+styleDim.Render(voteText(r.Vote)))
		}
	}
	out = append(out, "", styleSection.Render("Tags"))
	tags := 0
	for _, l := range d.data.Labels {
		if l.Active {
			out = append(out, "  "+truncate(l.Name, width-2))
			tags++
		}
	}
	if tags == 0 {
		out = append(out, styleDim.Render("  No tags"))
	}
	out = append(out, "", styleSection.Render("Work items"))
	for _, w := range d.data.WorkItems {
		out = append(out, "  "+truncate(fmt.Sprintf("#%d %s", w.ID, w.Fields.Title), width-2))
		out = append(out, "    "+styleDim.Render(w.Fields.Type+" · "+w.Fields.State))
	}
	if len(d.data.WorkItems) == 0 {
		out = append(out, styleDim.Render("  No work items"))
	}
	return out
}

func voteGlyph(v int) string {
	switch {
	case v >= ado.VoteApprovedWithSuggests:
		return styleGreen.Render("✓")
	case v <= ado.VoteRejected:
		return styleRed.Render("✗")
	case v <= ado.VoteWaitingForAuthor:
		return styleYellow.Render("!")
	}
	return styleDim.Render("○")
}

// --- Markdown ---

type markdownCache struct {
	renderers map[int]*glamour.TermRenderer
	out       map[string]string
}

func newMarkdownCache() *markdownCache {
	return &markdownCache{renderers: map[int]*glamour.TermRenderer{}, out: map[string]string{}}
}

func (m *markdownCache) render(src string, width int) string {
	key := fmt.Sprintf("%d\x00%s", width, src)
	if s, ok := m.out[key]; ok {
		return s
	}
	r, ok := m.renderers[width]
	if !ok {
		var err error
		r, err = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width))
		if err != nil {
			return src
		}
		m.renderers[width] = r
	}
	s, err := r.Render(src)
	if err != nil {
		s = src
	}
	s = strings.Trim(s, "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = trimStyledRight(ansi.Truncate(l, width, ""))
	}
	s = strings.Join(lines, "\n")
	m.out[key] = s
	return s
}

// trimStyledRight drops the trailing padding glamour adds to every line,
// spaces that are each wrapped in their own color codes, so the line is only
// as wide as its text.
func trimStyledRight(s string) string {
	for {
		switch {
		case strings.HasSuffix(s, " "):
			s = s[:len(s)-1]
		case strings.HasSuffix(s, "m"):
			i := strings.LastIndex(s, "\x1b[")
			if i < 0 || strings.Trim(s[i+2:len(s)-1], "0123456789;") != "" {
				return s + "\x1b[0m"
			}
			s = s[:i]
		default:
			return s + "\x1b[0m"
		}
	}
}
