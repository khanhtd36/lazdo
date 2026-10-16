package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// setReviewer, removeReviewer and searchPeople reach Azure DevOps; tests
// replace them.
var (
	setReviewer = func(ctx context.Context, c *ado.Client, pr ado.PullRequest, id string, required bool, vote int) error {
		return c.SetReviewer(ctx, pr, id, required, vote)
	}
	removeReviewer = func(ctx context.Context, c *ado.Client, pr ado.PullRequest, id string) error {
		return c.RemoveReviewer(ctx, pr, id)
	}
	searchPeople = func(ctx context.Context, c *ado.Client, query string) ([]ado.Person, error) {
		return c.SearchPeople(ctx, query)
	}
)

type (
	// peopleTickMsg is a pause in typing a name: the search starts unless
	// typing went on (seq moved).
	peopleTickMsg struct {
		prID, seq int
	}
	peopleFoundMsg struct {
		prID, seq int
		people    []ado.Person
		err       error
	}
)

func (m peopleTickMsg) pullRequest() int  { return m.prID }
func (m peopleFoundMsg) pullRequest() int { return m.prID }

const peopleSearchPause = 300 * time.Millisecond

// --- The list ---

// reviewersModal is w: the pull request's reviewers, required first, to
// add to, make required or optional, and remove. It reads the reviewers
// as they load, so a change shows once the pull request reloads.
type reviewersModal struct {
	d      *detailModel
	cursor int
}

func (d *detailModel) openReviewers() tea.Cmd {
	if d.data == nil {
		return nil
	}
	d.modal = &reviewersModal{d: d}
	return nil
}

// reviewers is the list's order, the side pane's: required, then optional.
func (r *reviewersModal) reviewers() []ado.Reviewer {
	var required, optional []ado.Reviewer
	for _, rv := range r.d.data.Reviewers {
		if rv.IsRequired {
			required = append(required, rv)
		} else {
			optional = append(optional, rv)
		}
	}
	return append(required, optional...)
}

func (r *reviewersModal) selected() (ado.Reviewer, bool) {
	rs := r.reviewers()
	if len(rs) == 0 {
		return ado.Reviewer{}, false
	}
	r.cursor = max(0, min(r.cursor, len(rs)-1))
	return rs[r.cursor], true
}

// closed says why a pull request that isn't active takes no changes.
func (d *detailModel) closed() string {
	if s := d.data.Status; s != "" && s != "active" {
		return "pull request is " + s
	}
	return ""
}

func (r *reviewersModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	d := r.d
	switch msg.String() {
	case "esc":
		return nil, nil
	case "j", "down":
		r.cursor = min(r.cursor+1, max(0, len(r.reviewers())-1))
	case "k", "up":
		r.cursor = max(r.cursor-1, 0)
	case "a", "A":
		if why := d.closed(); why != "" {
			return r, statusCmd(why)
		}
		return newPeoplePicker(d, r, msg.String() == "A"), nil
	case "t":
		rv, ok := r.selected()
		if !ok {
			return r, nil
		}
		if why := d.closed(); why != "" {
			return r, statusCmd(why)
		}
		return r, d.toggleRequired(rv)
	case "d":
		rv, ok := r.selected()
		if !ok {
			return r, nil
		}
		if why := d.closed(); why != "" {
			return r, statusCmd(why)
		}
		return d.confirmRemoveReviewer(rv, r), nil
	}
	return r, nil
}

// toggleRequired makes a reviewer required or optional, their vote kept.
func (d *detailModel) toggleRequired(rv ado.Reviewer) tea.Cmd {
	done := rv.DisplayName + " is now " + pick2(rv.IsRequired, "optional", "required")
	return d.act(done, false, func(ctx context.Context, pr ado.PullRequest) error {
		return setReviewer(ctx, d.client, pr, rv.ID, !rv.IsRequired, rv.Vote)
	})
}

// confirmRemoveReviewer asks before removing a reviewer; either answer
// returns to back.
func (d *detailModel) confirmRemoveReviewer(rv ado.Reviewer, back modal) modal {
	prompt := fmt.Sprintf("Remove %s from the reviewers?", rv.DisplayName)
	if rv.Vote != ado.VoteNone {
		prompt += "\n" + styleYellow.Render("Their vote ("+voteText(rv.Vote)+") goes with them.")
	}
	c := newConfirm(prompt, d.act("removed "+rv.DisplayName, false, func(ctx context.Context, pr ado.PullRequest) error {
		return removeReviewer(ctx, d.client, pr, rv.ID)
	}))
	c.back, c.then = back, back
	return c
}

func (r *reviewersModal) view(width int) string {
	w := max(40, min(width-8, 70))
	lines := []string{styleSection.Render("Reviewers"), ""}
	rs := r.reviewers()
	if len(rs) == 0 {
		lines = append(lines, styleDim.Render("  No reviewers"))
	}
	for i, rv := range rs {
		prefix := "  "
		if i == r.cursor {
			prefix = styleSelected.Render("› ")
		}
		note := styleDim.Render("  " + pick2(rv.IsRequired, "required", "optional") + " · " + voteText(rv.Vote))
		lines = append(lines, prefix+voteGlyph(rv.Vote)+" "+truncate(rv.DisplayName, w-30)+note)
	}
	lines = append(lines, "", styleDim.Render("a add · A add required · t required/optional · d remove · esc close"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

// clickRow selects the clicked reviewer; rows 0-1 are the title and a blank.
func (r *reviewersModal) clickRow(row int) (modal, tea.Cmd) { //nolint:unparam // clickableModal
	if i := row - 2; i >= 0 && i < len(r.reviewers()) {
		r.cursor = i
	}
	return r, nil
}

// --- Adding ---

// peoplePicker finds people and groups as a name or email is typed, after
// a pause, and adds the one picked.
type peoplePicker struct {
	d        *detailModel
	back     *reviewersModal
	required bool
	input    textinput.Model
	query    string // what the results are for
	people   []ado.Person
	cursor   int
	seq      int
	busy     bool
	err      string
}

func newPeoplePicker(d *detailModel, back *reviewersModal, required bool) *peoplePicker {
	in := textinput.New()
	in.Placeholder = "name or email"
	in.CharLimit = 100
	in.Width = 40
	in.Focus()
	return &peoplePicker{d: d, back: back, required: required, input: in}
}

func (p *peoplePicker) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return p.back, nil
	case "down", "ctrl+n", "tab":
		p.cursor = min(p.cursor+1, max(0, len(p.people)-1))
		return p, nil
	case "up", "ctrl+p", "shift+tab":
		p.cursor = max(p.cursor-1, 0)
		return p, nil
	case "enter":
		return p.add()
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() == before {
		return p, cmd
	}
	p.seq++
	seq, prID := p.seq, p.d.pr.ID
	return p, tea.Batch(cmd, tea.Tick(peopleSearchPause, func(time.Time) tea.Msg { return peopleTickMsg{prID: prID, seq: seq} }))
}

// onTick searches once typing has paused on the latest text.
func (p *peoplePicker) onTick(msg peopleTickMsg) tea.Cmd {
	query := strings.TrimSpace(p.input.Value())
	if msg.seq != p.seq {
		return nil
	}
	if query == "" {
		p.people, p.query, p.err, p.busy = nil, "", "", false
		return nil
	}
	p.busy = true
	client, seq, prID := p.d.client, p.seq, p.d.pr.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		people, err := searchPeople(ctx, client, query)
		return peopleFoundMsg{prID: prID, seq: seq, people: people, err: err}
	}
}

// onFound shows a search's results unless typing has moved on since.
func (p *peoplePicker) onFound(msg peopleFoundMsg) {
	if msg.seq != p.seq {
		return
	}
	p.busy, p.query, p.cursor, p.err = false, strings.TrimSpace(p.input.Value()), 0, ""
	p.people = msg.people
	if msg.err != nil {
		p.people, p.err = nil, msg.err.Error()
	}
}

func (p *peoplePicker) already(id string) bool {
	for _, r := range p.d.data.Reviewers {
		if strings.EqualFold(r.ID, id) {
			return true
		}
	}
	return false
}

func (p *peoplePicker) add() (modal, tea.Cmd) {
	if len(p.people) == 0 {
		return p, nil
	}
	who := p.people[min(p.cursor, len(p.people)-1)]
	if p.already(who.ID) {
		return p, statusCmd(who.Name + " is already a reviewer (t makes them required or optional)")
	}
	d, required := p.d, p.required
	done := "added " + who.Name + pick2(required, " as required", "")
	return p.back, d.act(done, false, func(ctx context.Context, pr ado.PullRequest) error {
		return setReviewer(ctx, d.client, pr, who.ID, required, ado.VoteNone)
	})
}

func (p *peoplePicker) view(width int) string {
	w := max(40, min(width-8, 70))
	title := pick2(p.required, "Add required reviewer", "Add optional reviewer")
	lines := []string{styleSection.Render(title), "", p.input.View(), ""}
	switch {
	case p.err != "":
		lines = append(lines, styleRed.Render(truncate("error: "+p.err, w)))
	case p.busy && len(p.people) == 0:
		lines = append(lines, styleDim.Render("searching…"))
	case p.query == "":
		lines = append(lines, styleDim.Render("type a name or an email"))
	case len(p.people) == 0:
		lines = append(lines, styleDim.Render("no one matches "+p.query))
	}
	for i, who := range p.people[:min(len(p.people), pickerRows)] {
		prefix := "  "
		if i == p.cursor {
			prefix = styleSelected.Render("› ")
		}
		name := who.Name
		if who.IsGroup {
			name += " (group)"
		}
		note := who.Mail
		if p.already(who.ID) {
			note = "already a reviewer"
		}
		lines = append(lines, prefix+truncate(name, w/2)+styleDim.Render("  "+truncate(note, w/2-4)))
	}
	lines = append(lines, "", styleDim.Render("↑/↓ pick · enter add · esc back"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

// clickRow adds the clicked person; rows 0-3 are the title, a blank, the
// input and a blank.
func (p *peoplePicker) clickRow(row int) (modal, tea.Cmd) {
	if i := row - 4; i >= 0 && i < min(len(p.people), pickerRows) {
		p.cursor = i
		return p.add()
	}
	return p, nil
}
