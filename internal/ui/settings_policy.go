package ui

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// Editing branch policies: e edits, n creates, space turns on or off, d
// deletes. Every write reloads the settings.

const (
	policyTypeFileSize = "2e26e725-8201-4edd-8bf5-978563c34a80"
	allRepos           = "All repositories"
)

// policyTypes are the kinds n can create, in the web's order.
var policyTypes = []struct{ id, name string }{
	{policyTypeMinReviewers, "Minimum number of reviewers"},
	{policyTypeRequiredReviewer, "Required reviewers"},
	{ado.PolicyTypeBuild, "Build validation"},
	{ado.PolicyTypeStatus, "Status check"},
	{policyTypeMergeStrategy, "Limit merge types"},
	{policyTypeComments, "Comment resolution"},
	{policyTypeWorkItems, "Linked work items"},
	{policyTypeFileSize, "File size restriction"},
}

// settingsSavedMsg reports a settings write; the settings reload after it.
type settingsSavedMsg struct {
	text string
	err  error
}

func (s *settingsView) write(done string, f func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return settingsSavedMsg{text: done, err: f(ctx)}
	}
}

// policyKey handles the edit keys on a policy in the Branch policies list.
func (s *settingsView) policyKey(k string) (bool, tea.Cmd) {
	it, ok := s.content.selected()
	p, isPolicy := it.value.(ado.PolicyConfig)
	switch k {
	case "n":
		return true, showModal(s.policyTypeMenu())
	case "e":
		if ok && isPolicy {
			return true, showModal(s.policyForm(&p, p.Type.ID))
		}
	case " ":
		if ok && isPolicy {
			p.IsEnabled = !p.IsEnabled
			state := map[bool]string{true: "turned on", false: "turned off"}[p.IsEnabled]
			client, project := s.client, s.project.ID
			return true, s.write(state+": "+policyName(p), func(ctx context.Context) error {
				return client.UpdatePolicy(ctx, project, p)
			})
		}
	case "d":
		if ok && isPolicy {
			client, project := s.client, s.project.ID
			prompt := "Delete this branch policy?\n" + stripStyle(policySentence(p, s.data.Names, s.data.Pipelines))
			return true, showModal(newConfirm(prompt, s.write("deleted: "+policyName(p), func(ctx context.Context) error {
				return client.DeletePolicy(ctx, project, p.ID)
			})))
		}
	default:
		return false, nil
	}
	return true, nil
}

func policyName(p ado.PolicyConfig) string {
	branch, _ := p.Scope()
	return p.Type.DisplayName + " on " + cmpOr(branch, "every branch")
}

func stripStyle(s string) string { return ansi.Strip(s) }

func (s *settingsView) policyTypeMenu() modal {
	items := make([]menuItem, 0, len(policyTypes))
	for _, t := range policyTypes {
		items = append(items, menuItem{label: t.name, run: func() (modal, tea.Cmd) {
			return s.policyForm(nil, t.id), nil
		}})
	}
	return &menuModal{title: "New branch policy", items: items}
}

// policyForm edits p, or creates a policy of typeID when p is nil.
func (s *settingsView) policyForm(p *ado.PolicyConfig, typeID string) *formModal {
	creating := p == nil
	if creating {
		p = &ado.PolicyConfig{IsEnabled: true, IsBlocking: true, Settings: map[string]any{}}
		p.Type.ID = typeID
	}
	st := p.Settings
	branch, repoID := p.Scope()
	repoChoices := make([]string, 0, 1+len(s.data.Repos))
	repoChoices = append(repoChoices, allRepos)
	repoIdx := 0
	for i, r := range s.data.Repos {
		repoChoices = append(repoChoices, r.Name)
		if strings.EqualFold(r.ID, repoID) {
			repoIdx = i + 1
		}
	}
	fields := []*formField{formBool("enabled", "Enabled", p.IsEnabled), formBool("blocking", "Required (blocks completing)", p.IsBlocking)}
	if typeID != policyTypeFileSize {
		fields = append(fields, formText("branch", "Branch", branch, "end with /* for every branch under a folder"))
	}
	fields = append(fields, formChoice("repo", "Repository", repoChoices, repoIdx))

	b := func(key string) bool { v, _ := st[key].(bool); return v }
	str := func(key string) string { v, _ := st[key].(string); return v }
	switch typeID {
	case policyTypeMinReviewers:
		fields = append(fields,
			formNumber("count", "Minimum number of reviewers", max(1, intSetting(st, "minimumApproverCount")), ""),
			formBool("creatorVoteCounts", "Allow requestors to approve their own changes", b("creatorVoteCounts")),
			formBool("allowDownvotes", "Allow completion even if some reviewers vote to wait or reject", b("allowDownvotes")),
			formBool("resetOnSourcePush", "Reset all approval votes on new pushes", b("resetOnSourcePush")))
	case policyTypeRequiredReviewer:
		fields = append(fields,
			formText("reviewers", "Reviewers", strings.Join(s.reviewerNames(st), ", "), "names or emails, comma separated"),
			formText("paths", "Path filter", joinPatterns(st), "e.g. /DAS/*; empty for all files"),
			formText("message", "Activity feed message", str("message"), ""))
	case ado.PolicyTypeBuild:
		names := make([]string, 0, 1+len(s.data.AllPipelines))
		names, idx := append(names, "(pick a pipeline)"), 0
		id := intSetting(st, "buildDefinitionId")
		for i, pl := range s.data.AllPipelines {
			names = append(names, pl.Name)
			if pl.ID == id {
				idx = i + 1
			}
		}
		fields = append(fields,
			formChoice("pipeline", "Pipeline", names, idx),
			formText("displayName", "Display name", str("displayName"), "empty: the pipeline's name"),
			formNumber("expiry", "Expires after (minutes, 0 = never)", intSetting(st, "validDuration"), "720 = 12h"),
			formBool("manual", "Trigger manually only", b("manualQueueOnly")),
			formText("paths", "Path filter", joinPatterns(st), "empty for all files"))
	case ado.PolicyTypeStatus:
		fields = append(fields,
			formText("genre", "Status genre", str("statusGenre"), "e.g. github-actions"),
			formText("name", "Status name", str("statusName"), ""),
			formText("displayName", "Display name", str("defaultDisplayName"), ""),
			formBool("invalidate", "Reset status on new pushes", b("invalidateOnSourceUpdate")))
	case policyTypeMergeStrategy:
		fields = append(fields,
			formBool("allowNoFastForward", "Basic merge (no fast-forward)", b("allowNoFastForward")),
			formBool("allowSquash", "Squash merge", b("allowSquash")),
			formBool("allowRebase", "Rebase and fast-forward", b("allowRebase")),
			formBool("allowRebaseMerge", "Rebase with merge commit (semi-linear)", b("allowRebaseMerge")))
	case policyTypeFileSize:
		mb := intSetting(st, "maximumGitBlobSizeInBytes") / (1 << 20)
		fields = append(fields,
			formNumber("maxMB", "Largest file allowed (MB)", max(1, mb), ""),
			formBool("uncompressed", "Count the uncompressed size", b("useUncompressedSize")))
	}
	title := "Edit " + cmpOr(p.Type.DisplayName, "policy")
	if creating {
		for _, t := range policyTypes {
			if t.id == typeID {
				title = "New " + t.name
			}
		}
	}
	return newForm(title, fields, func(f *formModal) (tea.Cmd, string) { return s.savePolicy(f, *p, creating) })
}

// savePolicy checks the form, then writes the policy. Reviewer names are
// looked up while saving, so the write runs in the returned command.
func (s *settingsView) savePolicy(f *formModal, p ado.PolicyConfig, creating bool) (tea.Cmd, string) {
	p, reviewers, refused := s.policyFromForm(f, p)
	if refused != "" {
		return nil, refused
	}
	st := p.Settings
	client, project, verb := s.client, s.project.ID, "saved"
	if creating {
		verb = "created"
	}
	return s.write(verb+": "+p.Type.DisplayName, func(ctx context.Context) error {
		if reviewers != "" {
			ids, err := resolvePeople(ctx, client, reviewers)
			if err != nil {
				return err
			}
			st["requiredReviewerIds"] = ids
		}
		if creating {
			return client.CreatePolicy(ctx, project, p)
		}
		return client.UpdatePolicy(ctx, project, p)
	}), ""
}

// policyFromForm is p as the form now has it, keeping settings the form
// doesn't show; reviewers are the names still to look up.
func (s *settingsView) policyFromForm(f *formModal, p ado.PolicyConfig) (_ ado.PolicyConfig, reviewers, refused string) {
	st := maps.Clone(p.Settings)
	if st == nil {
		st = map[string]any{}
	}
	p.IsEnabled, p.IsBlocking = f.get("enabled").on, f.get("blocking").on
	repoID := any(nil)
	if r := f.get("repo"); r.choice > 0 {
		repoID = s.data.Repos[r.choice-1].ID
	}
	if p.Type.ID == policyTypeFileSize {
		st["scope"] = []any{map[string]any{"repositoryId": repoID}}
	} else {
		branch := strings.TrimPrefix(f.get("branch").value(), "refs/heads/")
		if branch == "" {
			return p, "", "a policy needs a branch"
		}
		match := "Exact"
		if strings.HasSuffix(branch, "/*") {
			branch, match = strings.TrimSuffix(branch, "/*"), "Prefix"
		}
		st["scope"] = []any{map[string]any{"refName": "refs/heads/" + branch, "matchKind": match, "repositoryId": repoID}}
	}
	num := func(key string) int { n, _ := strconv.Atoi(f.get(key).value()); return n }
	switch p.Type.ID {
	case policyTypeMinReviewers:
		if num("count") < 1 {
			return p, "", "at least 1 reviewer"
		}
		st["minimumApproverCount"] = num("count")
		for _, k := range []string{"creatorVoteCounts", "allowDownvotes", "resetOnSourcePush"} {
			st[k] = f.get(k).on
		}
	case policyTypeRequiredReviewer:
		field := f.get("reviewers")
		if field.value() == "" {
			return p, "", "name at least one reviewer"
		}
		if field.value() != field.initial { // unchanged keeps the IDs it has
			reviewers = field.value()
		}
		setPatterns(st, f.get("paths").value())
		st["message"] = f.get("message").value()
	case ado.PolicyTypeBuild:
		pick := f.get("pipeline").choice
		if pick == 0 {
			return p, "", "pick a pipeline"
		}
		st["buildDefinitionId"] = s.data.AllPipelines[pick-1].ID
		st["displayName"] = nilIfEmpty(f.get("displayName").value())
		st["validDuration"] = num("expiry")
		st["manualQueueOnly"] = f.get("manual").on
		st["queueOnSourceUpdateOnly"] = true
		setPatterns(st, f.get("paths").value())
	case ado.PolicyTypeStatus:
		if f.get("name").value() == "" {
			return p, "", "a status check needs the status name"
		}
		st["statusGenre"], st["statusName"] = f.get("genre").value(), f.get("name").value()
		st["defaultDisplayName"] = nilIfEmpty(f.get("displayName").value())
		st["invalidateOnSourceUpdate"] = f.get("invalidate").on
	case policyTypeMergeStrategy:
		any1 := false
		for _, k := range []string{"allowNoFastForward", "allowSquash", "allowRebase", "allowRebaseMerge"} {
			st[k] = f.get(k).on
			any1 = any1 || f.get(k).on
		}
		if !any1 {
			return p, "", "allow at least one merge type"
		}
	case policyTypeFileSize:
		if num("maxMB") < 1 {
			return p, "", "the size limit must be at least 1 MB"
		}
		st["maximumGitBlobSizeInBytes"] = num("maxMB") << 20
		st["useUncompressedSize"] = f.get("uncompressed").on
	}
	p.Settings = st
	return p, reviewers, ""
}

// resolvePeople turns "Hung Nguyen, an@arbin.com" into identity IDs; a name
// that matches nobody, or several people, is refused with who it matched.
func resolvePeople(ctx context.Context, client *ado.Client, names string) ([]string, error) {
	var ids []string
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		found, err := client.SearchPeople(ctx, name)
		if err != nil {
			return nil, err
		}
		var exact []ado.Person
		for _, p := range found {
			if strings.EqualFold(p.Name, name) || strings.EqualFold(p.Mail, name) {
				exact = append(exact, p)
			}
		}
		if len(exact) == 0 {
			exact = found
		}
		switch len(exact) {
		case 0:
			return nil, fmt.Errorf("no one matches %q", name)
		case 1:
			ids = append(ids, exact[0].ID)
		default:
			var who []string
			for _, p := range exact[:min(4, len(exact))] {
				who = append(who, p.Name)
			}
			return nil, fmt.Errorf("%q matches %s: use a full name or an email", name, strings.Join(who, ", "))
		}
	}
	return ids, nil
}

func (s *settingsView) reviewerNames(st map[string]any) []string {
	ids, _ := st["requiredReviewerIds"].([]any)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		v, _ := id.(string)
		out = append(out, cmpOr(s.data.Names[strings.ToLower(v)], v))
	}
	return out
}

func joinPatterns(st map[string]any) string {
	pats, _ := st["filenamePatterns"].([]any)
	out := make([]string, 0, len(pats))
	for _, p := range pats {
		if v, ok := p.(string); ok {
			out = append(out, v)
		}
	}
	return strings.Join(out, "; ")
}

func setPatterns(st map[string]any, text string) {
	var pats []string
	for _, p := range strings.Split(text, ";") {
		if p = strings.TrimSpace(p); p != "" {
			pats = append(pats, p)
		}
	}
	if len(pats) == 0 {
		delete(st, "filenamePatterns")
		return
	}
	st["filenamePatterns"] = pats
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
