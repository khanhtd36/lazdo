package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// --- Vote ---

func (d *detailModel) voteMenu() modal {
	current := ado.VoteNone
	if me, ok := d.data.ReviewerFor(d.me.ID); ok {
		current = me.Vote
	}
	item := func(label string, vote int, confirm bool) menuItem {
		if vote == current && vote != ado.VoteNone {
			label += styleDim.Render("  (current)")
		}
		cast := d.act("voted: "+strings.ToLower(voteText(vote)), false, func(ctx context.Context) error {
			return d.client.Vote(ctx, d.pr, d.me.ID, vote)
		})
		return menuItem{label: label, run: func() (modal, tea.Cmd) {
			if confirm {
				return newConfirm("Reject this pull request?", cast), nil
			}
			return nil, cast
		}}
	}
	return &menuModal{title: "Vote", items: []menuItem{
		item("Approve", ado.VoteApproved, false),
		item("Approve with suggestions", ado.VoteApprovedWithSuggests, false),
		item("Wait for author", ado.VoteWaitingForAuthor, false),
		item("Reject", ado.VoteRejected, true),
		item("Reset feedback", ado.VoteNone, false),
	}}
}

// --- Complete ---

func (d *detailModel) completeMenu() modal {
	pr := d.data
	autoSet := pr.AutoCompleteSetBy != nil && pr.AutoCompleteSetBy.ID != ""
	items := []menuItem{{
		label: "Complete",
		run:   func() (modal, tea.Cmd) { return d.newCompleteDialog(false), nil },
	}}
	if autoSet {
		items = append(items, menuItem{label: "Cancel auto-complete", run: func() (modal, tea.Cmd) {
			return nil, d.act("auto-complete cancelled", false, func(ctx context.Context) error {
				return d.client.CancelAutoComplete(ctx, d.pr)
			})
		}})
	} else {
		items = append(items, menuItem{label: "Set auto-complete", run: func() (modal, tea.Cmd) {
			return d.newCompleteDialog(true), nil
		}})
	}
	draftLabel, draftDone := "Mark as draft", "marked as draft"
	if pr.IsDraft {
		draftLabel, draftDone = "Publish", "published"
		items[0].disabled = "publish the draft before completing it"
		items[1].disabled = items[0].disabled
	}
	items = append(items,
		menuItem{label: draftLabel, run: func() (modal, tea.Cmd) {
			return nil, d.act(draftDone, false, func(ctx context.Context) error {
				return d.client.SetDraft(ctx, d.pr, !pr.IsDraft)
			})
		}},
		menuItem{label: "Abandon", run: func() (modal, tea.Cmd) {
			return newConfirm(fmt.Sprintf("Abandon !%d %s?", d.pr.ID, d.pr.Title), d.act("abandoned", true, func(ctx context.Context) error {
				return d.client.Abandon(ctx, d.pr)
			})), nil
		}},
	)
	return &menuModal{title: "Complete", items: items}
}

// blockers are the blocking policies that keep the PR from completing.
func (d *detailModel) blockers() []string {
	var out []string
	for _, p := range d.distinctPolicies(d.data.Policies) {
		if p.Configuration.IsBlocking && p.Configuration.Type.ID != policyTypeMergeStrategy && p.Status != "approved" {
			out = append(out, d.policyText(p))
		}
	}
	return out
}

// distinctPolicies drops a policy that reads and stands exactly like an
// earlier one: the same rule set on the branch and on the repo, say, shows
// as one line, not two identical ones.
func (d *detailModel) distinctPolicies(ps []ado.Policy) []ado.Policy {
	seen := map[string]bool{}
	out := make([]ado.Policy, 0, len(ps))
	for _, p := range ps {
		key := fmt.Sprintf("%t|%s|%s", p.Configuration.IsBlocking, p.Status, d.policyText(p))
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

type completeField int

const (
	fieldMergeType completeField = iota
	fieldWorkItems
	fieldDeleteBranch
	fieldCustomMessage
	fieldMessage
	fieldOverride
	fieldReason
	fieldSubmit
)

// completeDialog is both "Complete pull request" and "Set auto-complete":
// the same options, but only Complete can override policies.
type completeDialog struct {
	d        *detailModel
	auto     bool
	types    []ado.MergeType
	typeIdx  int
	opts     ado.CompletionOptions
	custom   bool
	message  textarea.Model
	override bool
	reason   textinput.Model
	blockers []string
	focus    completeField
}

func (d *detailModel) newCompleteDialog(auto bool) *completeDialog {
	msg := textarea.New()
	msg.ShowLineNumbers = false
	msg.CharLimit = 0
	msg.SetWidth(min(80, max(40, d.width-14)))
	msg.SetHeight(6)
	msg.SetValue(d.mergeMessage())
	msg.Cursor.SetMode(cursor.CursorStatic)
	reason := textinput.New()
	reason.Cursor.SetMode(cursor.CursorStatic)
	reason.Placeholder = "reason for overriding policies"
	reason.Width = min(76, max(36, d.width-18))
	// Default: the first type the branch policy allows; squash without a policy.
	types := d.data.AllowedMergeTypes()
	idx := 0
	if len(types) == len(ado.AllMergeTypes) {
		idx = int(ado.MergeSquash)
	}
	return &completeDialog{
		d: d, auto: auto, types: types, typeIdx: idx,
		opts:     ado.CompletionOptions{DeleteSourceBranch: true},
		message:  msg,
		reason:   reason,
		blockers: d.blockers(),
	}
}

// mergeMessage is the merge commit message the web UI writes. The web page
// composes it and sends it; completing through the API without one gets the
// server's "Merge pull request N from <branch> into <target>" instead.
func (d *detailModel) mergeMessage() string {
	msg := fmt.Sprintf("Merged PR %d: %s", d.pr.ID, d.pr.Title)
	if desc := strings.TrimSpace(d.data.Description); desc != "" {
		msg += "\n\n" + desc
	}
	return msg
}

func (c *completeDialog) fields() []completeField {
	f := []completeField{fieldMergeType, fieldWorkItems, fieldDeleteBranch, fieldCustomMessage}
	if c.custom {
		f = append(f, fieldMessage)
	}
	if !c.auto {
		f = append(f, fieldOverride)
		if c.override {
			f = append(f, fieldReason)
		}
	}
	return append(f, fieldSubmit)
}

func (c *completeDialog) move(delta int) {
	fs := c.fields()
	i := 0
	for j, f := range fs {
		if f == c.focus {
			i = j
		}
	}
	c.focus = fs[(i+delta+len(fs))%len(fs)]
	c.message.Blur()
	c.reason.Blur()
	switch c.focus {
	case fieldMessage:
		c.message.Focus()
	case fieldReason:
		c.reason.Focus()
	case fieldMergeType, fieldWorkItems, fieldDeleteBranch, fieldCustomMessage, fieldOverride, fieldSubmit:
	}
}

func (c *completeDialog) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		return nil, nil
	case "tab":
		c.move(1)
		return c, nil
	case "shift+tab":
		c.move(-1)
		return c, nil
	case "ctrl+s":
		return c.submit()
	}
	switch c.focus {
	case fieldMessage:
		var cmd tea.Cmd
		c.message, cmd = c.message.Update(msg)
		return c, cmd
	case fieldReason:
		if key == "enter" {
			c.move(1)
			return c, nil
		}
		var cmd tea.Cmd
		c.reason, cmd = c.reason.Update(msg)
		return c, cmd
	case fieldMergeType, fieldWorkItems, fieldDeleteBranch, fieldCustomMessage, fieldOverride, fieldSubmit:
	}
	switch key {
	case "j", "down":
		c.move(1)
	case "k", "up":
		c.move(-1)
	case "left", "h":
		if c.focus == fieldMergeType {
			c.typeIdx = (c.typeIdx + len(c.types) - 1) % len(c.types)
		}
	case "right", "l":
		if c.focus == fieldMergeType {
			c.typeIdx = (c.typeIdx + 1) % len(c.types)
		}
	case " ", "x":
		c.toggle()
	case "enter":
		if c.focus == fieldSubmit {
			return c.submit()
		}
		c.toggle()
	}
	return c, nil
}

func (c *completeDialog) toggle() {
	switch c.focus {
	case fieldWorkItems:
		c.opts.TransitionWorkItems = !c.opts.TransitionWorkItems
	case fieldDeleteBranch:
		c.opts.DeleteSourceBranch = !c.opts.DeleteSourceBranch
	case fieldCustomMessage:
		c.custom = !c.custom
	case fieldOverride:
		c.override = !c.override
	case fieldMergeType:
		c.typeIdx = (c.typeIdx + 1) % len(c.types)
	case fieldMessage, fieldReason, fieldSubmit:
	}
}

// submitBlocked explains why the submit button is disabled, if it is.
func (c *completeDialog) submitBlocked() string {
	if over := c.options().EncodedLength() - completionBudget; c.custom && over > 0 {
		return fmt.Sprintf("the merge commit message is about %d characters too long", over)
	}
	switch {
	case c.auto:
		return ""
	case c.override && strings.TrimSpace(c.reason.Value()) == "":
		return "enter a reason to override policies"
	case len(c.blockers) > 0 && !c.override:
		return "blocking policies have not passed"
	}
	return ""
}

// options is what completing sends; the message always goes along, the
// web UI's unless customized.
func (c *completeDialog) options() ado.CompletionOptions {
	o := c.opts
	o.MergeType = c.types[c.typeIdx]
	if c.custom {
		o.MergeCommitMessage = c.message.Value()
		return o
	}
	o.MergeCommitMessage = c.d.mergeMessage()
	return fitMessage(o)
}

// completionBudget is what the options may measure, with room for the
// server counting a little more than json.Marshal does.
const completionBudget = ado.MaxCompletionOptionsLength - 150

// fitMessage shortens the merge commit message, ending it with "…", until
// the options fit Azure DevOps' limit. A long description would otherwise
// get the completion refused.
func fitMessage(o ado.CompletionOptions) ado.CompletionOptions {
	for over := o.EncodedLength() - completionBudget; over > 0; over = o.EncodedLength() - completionBudget {
		r := []rune(strings.TrimSuffix(o.MergeCommitMessage, "…"))
		if len(r) == 0 {
			break // the other options alone are too long; the server will say so
		}
		o.MergeCommitMessage = strings.TrimRight(string(r[:max(0, len(r)-over-1)]), " \n") + "…"
	}
	return o
}

func (c *completeDialog) submit() (modal, tea.Cmd) {
	if why := c.submitBlocked(); why != "" {
		return c, statusCmd(why)
	}
	d := c.d
	o := c.options()
	if c.auto {
		return nil, d.act("auto-complete set", false, func(ctx context.Context) error {
			return d.client.SetAutoComplete(ctx, d.pr, d.me.ID, o)
		})
	}
	if c.override {
		o.BypassReason = strings.TrimSpace(c.reason.Value())
	}
	complete := d.act(fmt.Sprintf("completed !%d", d.pr.ID), true, func(ctx context.Context) error {
		return d.client.Complete(ctx, d.data, o)
	})
	if c.override {
		return newConfirm("Override branch policies and complete?", complete), nil
	}
	return nil, complete
}

func (c *completeDialog) view(int) string {
	check := func(on bool) string {
		if on {
			return "[x]"
		}
		return "[ ]"
	}
	row := func(f completeField, text string) string {
		if c.focus == f {
			return styleSelected.Render("› ") + text
		}
		return "  " + text
	}
	title := "Complete pull request"
	if c.auto {
		title = "Set auto-complete"
	}
	lines := []string{styleSection.Render(title), ""}
	if len(c.blockers) > 0 {
		label := styleRed
		if c.auto {
			label = styleYellow
		}
		for _, b := range c.blockers {
			lines = append(lines, label.Render("✗ "+b))
		}
		if c.auto {
			lines = append(lines, styleDim.Render("  completes automatically once these pass"))
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		styleDim.Render("Merge type"),
		row(fieldMergeType, "‹ "+c.types[c.typeIdx].Title()+" ›"),
		"",
		styleDim.Render("Post-completion options"),
		row(fieldWorkItems, check(c.opts.TransitionWorkItems)+" Complete associated work items after merging"),
		row(fieldDeleteBranch, check(c.opts.DeleteSourceBranch)+" Delete "+c.d.pr.SourceBranch()+" after merging"),
		row(fieldCustomMessage, check(c.custom)+" Customize merge commit message"),
	)
	if c.custom {
		lines = append(lines, c.message.View())
	}
	if !c.auto {
		lines = append(lines, "", styleDim.Render("Policy override options"),
			row(fieldOverride, check(c.override)+" Override branch policies and enable merge"))
		if c.override {
			lines = append(lines, "  "+c.reason.View())
		}
	}
	button := "Set auto-complete"
	if !c.auto {
		button = "Complete merge"
		if c.override {
			button = "Override and complete"
		}
	}
	if c.submitBlocked() != "" {
		button = styleDim.Render(button)
	}
	lines = append(lines, "", row(fieldSubmit, "[ "+button+" ]"), "",
		styleDim.Render("tab/j/k move · space toggle · ←/→ merge type · ctrl+s submit · esc cancel"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

// --- Threads ---

func (d *detailModel) threadStatusMenu(t ado.Thread) modal {
	items := make([]menuItem, 0, len(ado.ThreadStatuses))
	for _, s := range ado.ThreadStatuses {
		label := ado.ThreadStatusTitle(s)
		if s == t.Status {
			label += styleDim.Render("  (current)")
		}
		items = append(items, menuItem{label: label, run: func() (modal, tea.Cmd) {
			return nil, d.act("thread set to "+strings.ToLower(ado.ThreadStatusTitle(s)), false, func(ctx context.Context) error {
				return d.client.SetThreadStatus(ctx, d.pr, t, s)
			})
		}})
	}
	return &menuModal{title: "Thread status", items: items}
}

func (d *detailModel) newCommentEditor() modal {
	return newEditor("New comment", "", d.width, func(content string) tea.Cmd {
		return d.act("comment posted", false, func(ctx context.Context) error {
			return d.client.NewThread(ctx, d.pr, content)
		})
	})
}

func (d *detailModel) replyEditor(t ado.Thread) modal {
	return newEditor("Reply", "", d.width, func(content string) tea.Cmd {
		return d.act("reply posted", false, func(ctx context.Context) error {
			return d.client.Reply(ctx, d.pr, t, content)
		})
	})
}

func (d *detailModel) editCommentEditor(t ado.Thread, cm ado.Comment) modal {
	return newEditor("Edit comment", cm.Content, d.width, func(content string) tea.Cmd {
		return d.act("comment updated", false, func(ctx context.Context) error {
			return d.client.EditComment(ctx, d.pr, t, cm, content)
		})
	})
}
