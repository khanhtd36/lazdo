package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/khanhtd36/lazdo/internal/actions"
)

type (
	// checkoutRequestMsg asks the root to open the checkout dialog.
	checkoutRequestMsg struct {
		org, project, repo, branch string
		// detach, when set, checks out this tag or commit without a branch;
		// branch is then what to fetch for a commit (empty for a tag).
		detach, label string
	}
	checkoutDoneMsg struct {
		repoKey, path, text string
		err                 error
	}
)

func requestCheckout(org, project, repo, branch string) tea.Cmd {
	return func() tea.Msg { return checkoutRequestMsg{org: org, project: project, repo: repo, branch: branch} }
}

// requestDetached opens the checkout dialog for a tag or commit.
func requestDetached(org, project, repo, fetchBranch, target, label string) tea.Cmd {
	return func() tea.Msg {
		return checkoutRequestMsg{org: org, project: project, repo: repo, branch: fetchBranch, detach: target, label: label}
	}
}

// what names the checkout target for the dialog.
func (r checkoutRequestMsg) what() string {
	if r.detach != "" {
		return r.label + " (detached)"
	}
	return r.branch
}

// checkoutModal asks where to check a branch out: an existing clone of the
// repo is switched, a missing or empty folder gets a fresh clone, and
// anything else is refused. The preview line says which before enter.
type checkoutModal struct {
	req         checkoutRequestMsg
	key         string
	input       textinput.Model
	suggestions []string
	sug         int
	plan        actions.Plan
	planFor     string
	running     bool
}

// checkoutSuggestions lists, best first: the working directory if it is a
// clone of the repo, the last path used for it this session, and a new
// folder named after the repo.
func checkoutSuggestions(key, repo string, last map[string]string) []string {
	var out []string
	add := func(p string) {
		for _, s := range out {
			if strings.EqualFold(s, p) {
				return
			}
		}
		out = append(out, p)
	}
	wd, _ := os.Getwd()
	if k, ok := actions.RepoKeyAt(wd); ok && k == key {
		add(wd)
	}
	if p, ok := last[key]; ok {
		add(p)
	}
	add(filepath.Join(wd, repo))
	return out
}

func newCheckoutModal(req checkoutRequestMsg, suggestions []string, width int) *checkoutModal {
	in := textinput.New()
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Width = min(90, max(30, width-16))
	in.SetValue(suggestions[0])
	in.CursorEnd()
	in.Focus()
	m := &checkoutModal{
		req:         req,
		key:         actions.RepoKey(req.org, req.project, req.repo),
		input:       in,
		suggestions: suggestions,
	}
	m.replan()
	return m
}

func (c *checkoutModal) replan() {
	if v := c.input.Value(); v != c.planFor {
		c.planFor = v
		c.plan = actions.PlanCheckout(v, c.key)
	}
}

func (c *checkoutModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	if c.running {
		return c, nil
	}
	switch msg.String() {
	case "esc":
		return nil, nil
	case "ctrl+n", "ctrl+p":
		step := 1
		if msg.String() == "ctrl+p" {
			step = len(c.suggestions) - 1
		}
		c.sug = (c.sug + step) % len(c.suggestions)
		c.input.SetValue(c.suggestions[c.sug])
		c.input.CursorEnd()
	case "tab":
		c.input.SetValue(actions.CompleteDir(c.input.Value()))
		c.input.CursorEnd()
	case "enter":
		if c.plan.Kind == actions.PlanRefuse {
			return c, nil
		}
		c.running = true
		return c, c.run()
	default:
		c.input, _ = c.input.Update(msg)
	}
	c.replan()
	return c, nil
}

func (c *checkoutModal) run() tea.Cmd {
	plan, req, key := c.plan, c.req, c.key
	return func() tea.Msg {
		done := checkoutDoneMsg{repoKey: key, path: plan.Path}
		url := actions.CloneURL(req.org, req.project, req.repo)
		switch {
		case plan.Kind == actions.PlanClone && req.detach != "":
			done.err = actions.CloneDetached(url, plan.Path, req.branch, req.detach)
		case plan.Kind == actions.PlanClone:
			done.err = actions.Clone(url, plan.Path, req.branch)
		case req.detach != "":
			done.err = actions.CheckoutDetached(plan.Path, req.branch, req.detach)
		default:
			done.err = actions.CheckoutIn(plan.Path, req.branch)
		}
		if plan.Kind == actions.PlanClone {
			done.text = "cloned " + req.repo + " at " + req.what() + " into " + plan.Path
		} else {
			done.text = "switched " + plan.Path + " to " + req.what()
		}
		return done
	}
}

func (c *checkoutModal) preview() string {
	switch {
	case c.running && c.plan.Kind == actions.PlanClone:
		return styleYellow.Render("cloning…")
	case c.running:
		return styleYellow.Render("fetching and switching…")
	}
	switch c.plan.Kind {
	case actions.PlanSwitch:
		return styleGreen.Render("✓ switch this existing clone to " + c.req.what())
	case actions.PlanClone:
		return styleGreen.Render("✓ clone " + c.req.repo + " into a new folder here")
	case actions.PlanRefuse:
	}
	return styleRed.Render("✗ " + c.plan.Reason)
}

func (c *checkoutModal) view(int) string {
	title := styleSection.Render("Check out "+c.req.what()) + styleDim.Render("  "+c.req.repo)
	hint := "enter check out · tab complete folder · ctrl+n/ctrl+p suggestions · esc cancel"
	return styleModal.Render(strings.Join([]string{title, "", c.input.View(), c.preview(), "", styleDim.Render(hint)}, "\n"))
}

// place centers the dialog over the screen.
func placeModal(m modal, width, height int) string {
	return lipgloss.Place(width-1, height, lipgloss.Center, lipgloss.Center, m.view(width))
}
