package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
	"github.com/khanhtd36/lazdo/internal/prefs"
)

// The org picker, O: every org of every account, last used first, to
// switch lazdo to; accounts are az logins, added and removed here.

type (
	// accountsMsg brings the accounts and who is logged in to each.
	accountsMsg struct{ accounts []ado.Account }
	// accountOrgsMsg brings the orgs of one account.
	accountOrgsMsg struct {
		dir  string
		orgs []ado.OrgRef
		err  error
	}
	accountAddedMsg struct {
		acct    ado.Account
		existed bool
		err     error
	}
	accountRemovedMsg struct {
		name string
		err  error
	}
	// orgSwitchMsg starts lazdo over on another org or account.
	orgSwitchMsg struct{ pair prefs.Pair }
)

// Tests replace these: they run az.
var (
	listAccounts = ado.Accounts
	accountOrgs  = ado.AccountOrgs
)

// orgChoice is a row: an org with an account.
type orgChoice struct {
	pair  prefs.Pair
	email string
}

// addAccountItem is "+ Add account…"; typedOrg is an org typed in the
// filter that no list has.
type (
	addAccountItem struct{}
	typedOrg       string
)

type orgPicker struct {
	current  prefs.Pair // the org and account lazdo is on
	recent   []prefs.Pair
	accounts []ado.Account // nil until loaded
	orgs     map[string][]ado.OrgRef
	errs     map[string]error
	list     pickList
}

// currentPair is the org and account of a client.
func currentPair(c *ado.Client) prefs.Pair {
	return prefs.Pair{Org: c.Org, Account: ado.Account{Dir: c.Account}.Name()}
}

func (m Model) openOrgPicker() (tea.Model, tea.Cmd) {
	p := &orgPicker{current: currentPair(m.client), recent: prefs.Load().Recent, orgs: map[string][]ado.OrgRef{}, errs: map[string]error{}}
	p.refresh()
	m.modal = p
	return m, loadAccounts()
}

func loadAccounts() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return accountsMsg{accounts: listAccounts(ctx)}
	}
}

// onAccounts takes the accounts and asks each for its orgs.
func (p *orgPicker) onAccounts(msg accountsMsg) tea.Cmd {
	p.accounts = msg.accounts
	p.orgs, p.errs = map[string][]ado.OrgRef{}, map[string]error{}
	cmds := make([]tea.Cmd, 0, len(msg.accounts))
	for _, a := range msg.accounts {
		dir := a.Dir
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			orgs, err := accountOrgs(ctx, dir)
			return accountOrgsMsg{dir: dir, orgs: orgs, err: err}
		})
	}
	p.refresh()
	return tea.Batch(cmds...)
}

func (p *orgPicker) onOrgs(msg accountOrgsMsg) {
	if msg.err != nil {
		p.errs[msg.dir] = msg.err
	} else {
		p.orgs[msg.dir] = msg.orgs
	}
	p.refresh()
}

// account is the loaded account named name, "" for az's own.
func (p *orgPicker) account(name string) (ado.Account, bool) {
	for _, a := range p.accounts {
		if a.Name() == name {
			return a, true
		}
	}
	return ado.Account{}, false
}

// refresh rebuilds the rows: the recent pairs, each account's other orgs,
// adding an account, and an org typed that nothing matches.
func (p *orgPicker) refresh() {
	var choices []orgChoice
	seen := map[prefs.Pair]bool{}
	add := func(pair prefs.Pair, email string) {
		key := prefs.Pair{Org: strings.ToLower(pair.Org), Account: pair.Account}
		if !seen[key] {
			seen[key] = true
			choices = append(choices, orgChoice{pair: pair, email: email})
		}
	}
	for _, r := range p.recent {
		a, ok := p.account(r.Account)
		switch {
		case ok:
			add(r, a.Email)
		case p.accounts == nil: // not loaded yet: show what was remembered
			add(r, r.Account)
		}
	}
	var notes []pickItem
	for _, a := range p.accounts {
		for _, o := range p.orgs[a.Dir] {
			add(prefs.Pair{Org: o.Name, Account: a.Name(), Tenant: o.Tenant}, a.Email)
		}
		who := a.Email
		if who == "" {
			who = pick2(a.Dir == "", "az's own login", a.Name())
		}
		if err := p.errs[a.Dir]; err != nil {
			notes = append(notes, settingsHeader(styleRed.Render(who+": "+err.Error())))
		} else if _, ok := p.orgs[a.Dir]; !ok {
			notes = append(notes, settingsHeader(styleDim.Render(who+": loading orgs…")))
		}
	}
	if p.accounts == nil {
		notes = append(notes, settingsHeader(styleDim.Render("loading accounts…")))
	}
	orgW := 0
	for _, c := range choices {
		orgW = max(orgW, len(c.pair.Org))
	}
	items := make([]pickItem, 0, len(choices)+len(notes)+2)
	for _, c := range choices {
		note := ""
		if strings.EqualFold(c.pair.Org, p.current.Org) && c.pair.Account == p.current.Account {
			note = "  (current)"
		}
		text := styleAccent.Render(padRight(c.pair.Org, orgW)) + "  " + styleDim.Render(c.email+note)
		items = append(items, pickItem{search: c.pair.Org + " " + c.email, value: c, render: func(w int) string { return truncate(text, w) }})
	}
	items = append(items, notes...)
	items = append(items, pickItem{search: "add account", value: addAccountItem{}, render: func(w int) string {
		return truncate(styleCyan.Render("+ Add account…"), w)
	}})
	p.list.setItems(items)
	if f := strings.TrimSpace(p.list.filter); f != "" && p.list.matches() == 0 {
		who := p.current.Account
		if a, ok := p.account(p.current.Account); ok && a.Email != "" {
			who = a.Email
		}
		org := ado.OrgName(f)
		p.list.setItems(append(items, pickItem{search: f, value: typedOrg(org), render: func(w int) string {
			return truncate("open "+styleAccent.Render(org)+styleDim.Render(" with "+pick2(who == "", "az's own login", who)), w)
		}}))
	}
}

func (p *orgPicker) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	k := msg.String()
	if !p.list.typing && k == "d" {
		return p.remove()
	}
	handled, activate := p.list.key(msg, pickerRows)
	p.refresh()
	switch {
	case activate:
		return p.pick()
	case !handled && k == "esc":
		return nil, nil
	}
	return p, nil
}

// pick switches to the selected org, or adds an account.
func (p *orgPicker) pick() (modal, tea.Cmd) {
	it, ok := p.list.selected()
	if !ok {
		return p, nil
	}
	switch v := it.value.(type) {
	case orgChoice:
		if strings.EqualFold(v.pair.Org, p.current.Org) && v.pair.Account == p.current.Account {
			return nil, statusCmd("already on " + v.pair.Org)
		}
		return nil, func() tea.Msg { return orgSwitchMsg{pair: v.pair} }
	case typedOrg:
		pair := prefs.Pair{Org: string(v), Account: p.current.Account}
		return nil, func() tea.Msg { return orgSwitchMsg{pair: pair} }
	case addAccountItem:
		cmd, dir, err := ado.NewAccountLogin()
		if err != nil {
			return p, statusCmd("error: add account: " + err.Error())
		}
		return p, tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				_ = ado.RemoveAccount(dir)
				return accountAddedMsg{err: err}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			acct, existed, err := ado.FinishAccount(ctx, dir)
			return accountAddedMsg{acct: acct, existed: existed, err: err}
		})
	}
	return p, nil
}

// remove asks, then deletes the selected row's account, if lazdo added it.
func (p *orgPicker) remove() (modal, tea.Cmd) {
	it, ok := p.list.selected()
	c, isOrg := it.value.(orgChoice)
	if !ok || !isOrg {
		return p, nil
	}
	name := c.pair.Account
	switch name {
	case "":
		return p, statusCmd("az's own login is yours to manage with az logout")
	case p.current.Account:
		return p, statusCmd("lazdo is using " + c.email + "; switch to another account first")
	}
	dir := ado.AccountDir(name)
	confirm := newConfirm(fmt.Sprintf("Remove the account %s?\nlazdo deletes its az login, which logs it out here; az's own login is untouched.", c.email), func() tea.Msg {
		return accountRemovedMsg{name: name, err: ado.RemoveAccount(dir)}
	})
	confirm.back = p
	return confirm, nil
}

// onAdded reloads the accounts after az login.
func (p *orgPicker) onAdded(msg accountAddedMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		return statusCmd("error: add account: " + msg.err.Error())
	case msg.existed:
		return tea.Batch(statusCmd(msg.acct.Email+" was added already"), loadAccounts())
	}
	return tea.Batch(statusCmd("added "+msg.acct.Email), loadAccounts())
}

func (p *orgPicker) onRemoved(msg accountRemovedMsg) tea.Cmd {
	if msg.err != nil {
		return statusCmd("error: remove account: " + msg.err.Error())
	}
	_ = prefs.Load().Forget(msg.name).Save()
	p.recent = prefs.Load().Recent
	return tea.Batch(statusCmd("removed "+msg.name), loadAccounts())
}

func (p *orgPicker) view(width int) string {
	w := max(40, min(width-8, 90))
	rows := min(pickerRows, max(1, len(p.list.visible())))
	if _, ok := p.list.filterLine(); ok {
		rows++
	}
	lines := make([]string, 0, rows+4)
	lines = append(lines, styleSection.Render("Switch organization")+styleDim.Render("  or account"), "")
	lines = append(lines, p.list.view(w, rows)...)
	lines = append(lines, "", styleDim.Render("enter switch · / filter or type an org · d remove account · esc close"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

func (p *orgPicker) clickRow(row int) (modal, tea.Cmd) {
	if p.list.click(row - 2) { // below the title and a blank line
		return p.pick()
	}
	return p, nil
}

// switchOrg starts lazdo over on another org or account, keeping only
// the screen size and the state of updates.
func (m Model) switchOrg(pair prefs.Pair) (tea.Model, tea.Cmd) {
	_ = prefs.Load().Use(pair).Save()
	n := New(ado.NewClientAs(pair.Org, ado.AccountDir(pair.Account), pair.Tenant), m.interval)
	n.width, n.height = m.width, m.height
	n.updates, n.checkingUpdates, n.updating, n.installed, n.updateFailed = m.updates, m.checkingUpdates, m.updating, m.installed, m.updateFailed
	n.updateRestart = m.updateRestart
	n.lastCheckout = m.lastCheckout
	n.status = "switched to " + pair.Org
	return n, n.fetchList()
}

// onAccountMsg hands the picker its answers; once it is closed they don't
// matter, but an added or removed account still changes the title.
func (m Model) onAccountMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case accountAddedMsg, accountRemovedMsg:
		m.manyAccounts = len(ado.ExtraAccounts()) > 0
	}
	p, ok := m.modal.(*orgPicker)
	if !ok {
		return m, nil
	}
	switch msg := msg.(type) {
	case accountsMsg:
		return m, p.onAccounts(msg)
	case accountOrgsMsg:
		p.onOrgs(msg)
	case accountAddedMsg:
		return m, p.onAdded(msg)
	case accountRemovedMsg:
		return m, p.onRemoved(msg)
	}
	return m, nil
}

// onOrgName is whether x falls on the org's name in the title line.
func (m Model) onOrgName(x int) bool {
	line := ansi.Strip(m.titleLine())
	i := strings.Index(line, "   "+m.client.Org)
	if i < 0 {
		return false
	}
	i += 3
	return x >= i && x < i+len(m.client.Org)
}
