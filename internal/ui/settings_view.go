package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// The project's Settings tab: a section list (Overview, Teams, …) beside
// the selected section's content, read-only for now.

type settingsSection int

const (
	secOverview settingsSection = iota
	secTeams
	secGroups
	secPermissions
	secRepos
	secPolicies
	secQueues
	secConnections
	secVarGroups
	secCount
)

func (s settingsSection) title() string {
	return [...]string{
		"Overview", "Teams", "Security groups", "Permissions", "Repositories",
		"Branch policies", "Agent pools", "Service connections", "Variable groups",
	}[s]
}

// errKey is the section's key in ProjectSettings.Errs.
func (s settingsSection) errKey() string {
	return [...]string{"overview", "teams", "groups", "permissions", "", "policies", "queues", "connections", "variable groups"}[s]
}

const settingsSidebar = 27 // fits "Service connections (12)"

type settingsView struct {
	client  *ado.Client
	project ado.ProjectInfo
	repos   []ado.Repo

	data    *ado.ProjectSettings
	loading bool

	section  settingsSection
	sections pickList
	content  pickList
	onRight  bool

	// detail is a drilled-in list: a team's or group's members, or a
	// group's permissions; nil shows the section.
	detail *settingsDetail
}

type settingsDetail struct {
	title   string
	list    pickList
	loading bool
	err     error
}

type (
	settingsLoadedMsg struct {
		projectID string
		data      *ado.ProjectSettings
	}
	settingsMembersMsg struct {
		title   string
		members []ado.Member
		err     error
	}
)

func newSettingsView(client *ado.Client, p ado.ProjectInfo, repos []ado.Repo) *settingsView {
	s := &settingsView{client: client, project: p, repos: repos}
	s.rebuild()
	return s
}

func (s *settingsView) load() tea.Cmd {
	if s.loading {
		return nil
	}
	s.loading = true
	client, p, repos := s.client, s.project, s.repos
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return settingsLoadedMsg{projectID: p.ID, data: client.ProjectSettings(ctx, p, repos)}
	}
}

func (s *settingsView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case settingsLoadedMsg:
		if msg.projectID == s.project.ID {
			s.loading, s.data = false, msg.data
			s.rebuild()
		}
	case settingsMembersMsg:
		if s.detail != nil && s.detail.title == msg.title {
			s.detail.loading, s.detail.err = false, msg.err
			s.detail.list.setItems(memberItems(msg.members))
		}
	}
	return nil
}

// rebuild refreshes the section list (with counts) and the content.
func (s *settingsView) rebuild() {
	items := make([]pickItem, 0, secCount)
	for sec := range secCount {
		label := sec.title()
		if n, ok := s.count(sec); ok {
			label += fmt.Sprintf(" (%d)", n)
		}
		items = append(items, pickItem{search: label, value: sec, render: func(w int) string { return truncate(label, w) }})
	}
	cursor := s.sections.cursor
	s.sections.setItems(items)
	s.sections.cursor = cursor
	s.content.setItems(s.contentItems())
}

func (s *settingsView) count(sec settingsSection) (int, bool) {
	if s.data == nil {
		return 0, false
	}
	switch sec {
	case secTeams:
		return len(s.data.Teams), true
	case secGroups, secPermissions:
		return len(s.data.Groups), true
	case secRepos:
		return len(s.data.Repos), true
	case secPolicies:
		return len(s.data.Policies), true
	case secQueues:
		return len(s.data.Queues), true
	case secConnections:
		return len(s.data.Connections), true
	case secVarGroups:
		return len(s.data.VarGroups), true
	case secOverview, secCount:
	}
	return 0, false
}

func (s *settingsView) typing() bool {
	if s.detail != nil {
		return s.detail.list.typing
	}
	return s.content.typing || s.sections.typing
}

// key handles a key; false leaves it to the project (tabs, quit, esc out).
func (s *settingsView) key(msg tea.KeyMsg, height int) (bool, tea.Cmd) {
	k := msg.String()
	if s.detail != nil {
		handled, _ := s.detail.list.key(msg, height)
		if handled {
			return true, nil
		}
		if k == "esc" || k == "h" || k == "left" {
			s.detail = nil
			return true, nil
		}
		return k == "enter", nil
	}
	switch k {
	case "tab", "shift+tab":
		s.onRight = !s.onRight
		return true, nil
	case "l", "right":
		s.onRight = true
		return true, nil
	case "h", "left":
		s.onRight = false
		return true, nil
	case "r":
		return true, s.load()
	}
	if !s.onRight {
		before := s.sections.cursor
		handled, activate := s.sections.key(msg, height)
		if it, ok := s.sections.selected(); ok && s.sections.cursor != before {
			s.section = it.value.(settingsSection)
			s.content.cursor = 0
			s.content.setItems(s.contentItems()) // lands on the first row, past any heading
		}
		if activate {
			s.onRight = true
		}
		return handled, nil
	}
	handled, activate := s.content.key(msg, height)
	if activate {
		return true, s.open()
	}
	if !handled && k == "esc" {
		s.onRight = false
		return true, nil
	}
	return handled, nil
}

// open drills into the selected team, group or group's permissions.
func (s *settingsView) open() tea.Cmd {
	it, ok := s.content.selected()
	if !ok {
		return nil
	}
	switch v := it.value.(type) {
	case ado.Team:
		s.detail = &settingsDetail{title: "Members of " + v.Name, loading: true}
		client, p, title := s.client, s.project, s.detail.title
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ms, err := client.TeamMembers(ctx, p.ID, v.ID)
			return settingsMembersMsg{title: title, members: ms, err: err}
		}
	case ado.Group:
		if s.section == secPermissions {
			s.detail = &settingsDetail{title: "Project permissions of " + v.Name}
			s.detail.list.setItems(permissionItems(s.data.Permissions[v.SID()]))
			return nil
		}
		s.detail = &settingsDetail{title: "Members of " + v.Name, loading: true}
		client, title := s.client, s.detail.title
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ms, err := client.GroupMembers(ctx, v.Descriptor)
			return settingsMembersMsg{title: title, members: ms, err: err}
		}
	}
	return nil
}

func (s *settingsView) view(width, height int) []string {
	s.sections.unfocused = s.onRight || s.detail != nil
	s.content.unfocused = !s.onRight
	left := s.sections.view(settingsSidebar, height)
	right := s.rightView(width-settingsSidebar-1, height)
	out := make([]string, height)
	for i := range height {
		out[i] = fit(left[i], settingsSidebar) + styleDim.Render("│") + right[i]
	}
	return out
}

func (s *settingsView) rightView(width, height int) []string {
	if s.detail != nil {
		head := styleSection.Render(s.detail.title) + styleDim.Render("  esc back")
		var body []string
		switch {
		case s.detail.err != nil:
			body = []string{styleRed.Render("  error: " + s.detail.err.Error())}
		case s.detail.loading:
			body = []string{styleDim.Render("  loading…")}
		case len(s.detail.list.items) == 0:
			body = []string{styleDim.Render("  none")}
		default:
			body = s.detail.list.view(width, height-1)
		}
		return padLines(append([]string{truncate(head, width)}, body...), height)
	}
	switch {
	case s.data == nil && s.loading:
		return padLines([]string{styleDim.Render("  loading settings…")}, height)
	case s.data == nil:
		return padLines(nil, height)
	}
	if err := s.data.Errs[s.section.errKey()]; err != nil {
		return padLines([]string{styleRed.Render(truncate("  error: "+err.Error(), width))}, height)
	}
	if len(s.content.items) == 0 {
		return padLines([]string{styleDim.Render("  none")}, height)
	}
	return padLines(s.content.view(width, height), height)
}

// --- Section content ---

func settingsRow(text string, value any) pickItem {
	return pickItem{search: ansi.Strip(text), value: value, render: func(w int) string { return truncate(text, w) }}
}

func settingsHeader(text string) pickItem {
	return pickItem{header: true, render: func(w int) string { return truncate(styleHeader.Render(text), w) }}
}

func (s *settingsView) contentItems() []pickItem {
	if s.data == nil {
		return nil
	}
	d := s.data
	var items []pickItem
	switch s.section {
	case secOverview:
		o := d.Overview
		items = append(items,
			settingsRow(styleTitle.Render(o.Name)+styleDim.Render("  "+o.Visibility), nil),
			settingsRow(o.Description, nil),
			settingsRow(styleDim.Render("Process  ")+o.Process+styleDim.Render("   Version control  ")+o.VersionControl, nil),
		)
		var services []string
		for _, name := range ado.ServiceOrder {
			if on, ok := o.Services[name]; ok {
				mark := styleGreen.Render("✓ ")
				if !on {
					mark = styleDim.Render("○ ")
				}
				services = append(services, mark+name)
			}
		}
		if len(services) > 0 {
			items = append(items, settingsRow(styleDim.Render("Services  ")+strings.Join(services, "  "), nil))
		}
		if !o.LastUpdate.IsZero() {
			items = append(items, settingsRow(styleDim.Render("Last changed  "+o.LastUpdate.Local().Format("2006-01-02")), nil))
		}
	case secTeams:
		for _, t := range d.Teams {
			items = append(items, settingsRow(t.Name+styleDim.Render("  "+t.Description), t))
		}
	case secGroups, secPermissions:
		for _, g := range d.Groups {
			items = append(items, settingsRow(g.Name+styleDim.Render("  "+firstLine(g.Description)), g))
		}
	case secRepos:
		for _, r := range d.Repos {
			text := r.Name + styleDim.Render("  ⎇ "+r.DefaultBranchName()+"  "+humanSize(r.Size))
			if r.IsDisabled {
				text += styleYellow.Render("  disabled")
			}
			items = append(items, settingsRow(text, r))
		}
	case secPolicies:
		items = s.policyItems()
	case secQueues:
		for _, q := range d.Queues {
			kind := "self-hosted"
			if q.Hosted {
				kind = "Microsoft-hosted"
			}
			items = append(items, settingsRow(q.Name+styleDim.Render("  "+kind), q))
		}
	case secConnections:
		for _, c := range d.Connections {
			state := styleGreen.Render("ready")
			if !c.IsReady {
				state = styleYellow.Render("not ready")
			}
			items = append(items, settingsRow(c.Name+styleDim.Render("  "+c.Type+"  "+c.URL+"  ")+state, c))
		}
	case secVarGroups:
		for _, g := range d.VarGroups {
			items = append(items, settingsHeader(g.Name+"  "+g.Description))
			for _, v := range g.Variables {
				value := v.Value
				if v.Secret {
					value = styleDim.Render("•••••• (secret)")
				}
				items = append(items, settingsRow("  "+v.Name+styleDim.Render(" = ")+value, v))
			}
		}
	case secCount:
	}
	return items
}

// policyItems groups the branch policies by branch, then by repo (all
// repos first), each policy as the sentence it enforces.
func (s *settingsView) policyItems() []pickItem {
	d := s.data
	repoNames := map[string]string{}
	for _, r := range d.Repos {
		repoNames[strings.ToLower(r.ID)] = r.Name
	}
	type group struct {
		branch, repo string
		policies     []ado.PolicyConfig
	}
	groups := map[string]*group{}
	var keys []string
	for _, p := range d.Policies {
		branch, repoID := p.Scope()
		if branch == "" {
			branch = "every branch"
		}
		repo := "all repos"
		if repoID != "" {
			repo = cmpOr(repoNames[strings.ToLower(repoID)], repoID)
		}
		key := branch + "\x00" + repo
		if groups[key] == nil {
			groups[key] = &group{branch: branch, repo: repo}
			keys = append(keys, key)
		}
		groups[key].policies = append(groups[key].policies, p)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := groups[keys[i]], groups[keys[j]]
		if a.branch != b.branch {
			return a.branch < b.branch
		}
		return a.repo == "all repos" || (b.repo != "all repos" && a.repo < b.repo)
	})
	var items []pickItem
	for _, k := range keys {
		g := groups[k]
		items = append(items, settingsHeader(g.branch+"  ("+g.repo+")"))
		for _, p := range g.policies {
			items = append(items, settingsRow("  "+policySentence(p, d.Names, d.Pipelines), p))
		}
	}
	return items
}

// policySentence says what a configured policy enforces, like the web's
// branch policy page: "✓ At least 2 reviewers must approve · blocking".
func policySentence(p ado.PolicyConfig, names map[string]string, pipelines map[int]string) string {
	st := p.Settings
	text := p.Type.DisplayName
	switch p.Type.ID {
	case policyTypeMinReviewers:
		n := intSetting(st, "minimumApproverCount")
		text = fmt.Sprintf("At least %d %s must approve", n, plural(n, "reviewer", "reviewers"))
		if b, _ := st["creatorVoteCounts"].(bool); b {
			text += ", creator's vote counts"
		}
		if b, _ := st["resetOnSourcePush"].(bool); b {
			text += ", reset on new pushes"
		}
	case policyTypeRequiredReviewer:
		ids, _ := st["requiredReviewerIds"].([]any)
		who := make([]string, 0, len(ids))
		for _, id := range ids {
			s, _ := id.(string)
			who = append(who, cmpOr(names[strings.ToLower(s)], s))
		}
		text = "Required reviewers: " + strings.Join(who, ", ")
		if pats, _ := st["filenamePatterns"].([]any); len(pats) > 0 {
			text += fmt.Sprintf(" (for %d path %s)", len(pats), plural(len(pats), "pattern", "patterns"))
		}
	case ado.PolicyTypeBuild:
		// The policy's own name when it has one, else its pipeline's, which
		// often says "Build" already ("Build BE validation on PR").
		name, _ := st["displayName"].(string)
		if id, ok := st["buildDefinitionId"].(float64); ok && name == "" {
			name = pipelines[int(id)]
		}
		text = cmpOr(name, "The build") + " must pass"
		if mins := intSetting(st, "validDuration"); mins > 0 {
			text += ", expires after " + minutesText(mins)
		}
	case policyTypeMergeStrategy:
		var types []string
		for key, label := range map[string]string{"allowNoFastForward": "merge", "allowSquash": "squash", "allowRebase": "rebase", "allowRebaseMerge": "semi-linear"} {
			if b, _ := st[key].(bool); b {
				types = append(types, label)
			}
		}
		sort.Strings(types)
		text = "Merge types: " + strings.Join(types, ", ")
	case policyTypeComments:
		text = "All comments must be resolved"
	case policyTypeWorkItems:
		text = "Work items must be linked"
	case ado.PolicyTypeStatus:
		name, _ := st["defaultDisplayName"].(string)
		genre, _ := st["statusGenre"].(string)
		status, _ := st["statusName"].(string)
		text = "Status " + cmpOr(name, strings.Trim(genre+"/"+status, "/")) + " must succeed"
	}
	mark, how := styleGreen.Render("✓ "), "blocking"
	if !p.IsBlocking {
		how = "optional"
	}
	if !p.IsEnabled {
		return styleDim.Render("○ " + text + " · " + how + " · off")
	}
	return mark + text + styleDim.Render(" · "+how)
}

// minutesText writes 720 as "12h" and 2880 as "2d", like the web does.
func minutesText(m int) string {
	switch {
	case m%(24*60) == 0:
		return fmt.Sprintf("%dd", m/(24*60))
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dm", m)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// --- Details ---

func memberItems(ms []ado.Member) []pickItem {
	items := make([]pickItem, 0, len(ms))
	for _, m := range ms {
		text := m.Name
		switch {
		case m.IsGroup:
			text = styleCyan.Render("group ") + text
		case m.Admin:
			text += styleYellow.Render("  admin")
		}
		items = append(items, settingsRow(text+styleDim.Render("  "+m.Detail), m))
	}
	return items
}

func permissionItems(ps []ado.Permission) []pickItem {
	if len(ps) == 0 {
		return []pickItem{settingsRow(styleDim.Render("Nothing set on this group at project level; it inherits."), nil)}
	}
	items := make([]pickItem, 0, len(ps))
	for _, p := range ps {
		state := styleDim.Render("Not set")
		switch p.State {
		case "Allow":
			state = styleGreen.Render("Allow")
		case "Deny":
			state = styleRed.Render("Deny")
		}
		if p.Inherited {
			state += styleDim.Render(" (inherited)")
		}
		items = append(items, settingsRow(fit(p.Name, 44)+" "+state, p))
	}
	return items
}
