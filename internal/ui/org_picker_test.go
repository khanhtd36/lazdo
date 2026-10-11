package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
	"github.com/khanhtd36/lazdo/internal/prefs"
)

// fakeAccounts: az's own login is alex, who is in contoso and fabrikam;
// lazdo added sam, also in fabrikam.
func fakeAccounts(t *testing.T) {
	t.Helper()
	t.Cleanup(prefs.UseFile(filepath.Join(t.TempDir(), "config.json")))
	savedList, savedOrgs := listAccounts, accountOrgs
	t.Cleanup(func() { listAccounts, accountOrgs = savedList, savedOrgs })
	sam := ado.AccountDir("sam@fabrikam.com")
	listAccounts = func(context.Context) []ado.Account {
		return []ado.Account{{Email: "alex@contoso.com"}, {Dir: sam, Email: "sam@fabrikam.com"}}
	}
	accountOrgs = func(_ context.Context, dir string) ([]ado.OrgRef, error) {
		if dir == sam {
			return []ado.OrgRef{{Name: "fabrikam", Tenant: "t-fab"}}, nil
		}
		return []ado.OrgRef{{Name: "contoso", Tenant: "t-con"}, {Name: "fabrikam", Tenant: "t-con"}}, nil
	}
}

// run feeds a command's messages back into the model, batches included.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(t, m, c)
		}
	case nil:
	default:
		next, cmd := m.Update(msg)
		return run(t, next.(Model), cmd)
	}
	return m
}

func openedPicker(t *testing.T) (Model, *orgPicker) {
	t.Helper()
	fakeAccounts(t)
	_ = prefs.Prefs{}.Use(prefs.Pair{Org: "contoso"}).Save()
	m := New(ado.NewClient("contoso"), time.Minute)
	m.width, m.height, m.loading = 120, 30, false
	next, cmd := m.Update(keyMsg("O"))
	m = run(t, next.(Model), cmd)
	p, ok := m.modal.(*orgPicker)
	if !ok {
		t.Fatalf("O should open the org picker, got %T", m.modal)
	}
	return m, p
}

func TestOrgPickerListsEveryAccountsOrgs(t *testing.T) {
	_, p := openedPicker(t)
	view := ansi.Strip(p.view(120))
	var rows []string
	for _, l := range strings.Split(view, "\n") {
		if f := strings.Fields(strings.Trim(l, "│▌ ")); len(f) >= 2 && strings.Contains(f[1], "@") {
			rows = append(rows, f[0]+" "+f[1])
		}
	}
	want := []string{"contoso alex@contoso.com", "fabrikam alex@contoso.com", "fabrikam sam@fabrikam.com"}
	if strings.Join(rows, ",") != strings.Join(want, ",") {
		t.Fatalf("rows %q, want %q\n%s", rows, want, view)
	}
	if !strings.Contains(view, "(current)") || !strings.Contains(view, "+ Add account…") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestOrgPickerSwitchesAndRemembers(t *testing.T) {
	m, p := openedPicker(t)
	for p.list.cursor = 0; ; p.list.cursor++ {
		if it, _ := p.list.selected(); it.value.(orgChoice).email == "sam@fabrikam.com" {
			break
		}
	}
	old := m.client
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(Model)
	msg := cmd()
	sw, ok := msg.(orgSwitchMsg)
	if !ok || sw.pair != (prefs.Pair{Org: "fabrikam", Account: "sam@fabrikam.com", Tenant: "t-fab"}) {
		t.Fatalf("got %#v", msg)
	}
	next, _ = m.Update(sw)
	m = next.(Model)
	if m.client.Org != "fabrikam" || m.client.Account != ado.AccountDir("sam@fabrikam.com") || m.modal != nil || !m.loading {
		t.Fatalf("switch: org %q account %q modal %T loading %v", m.client.Org, m.client.Account, m.modal, m.loading)
	}
	if r := prefs.Load().Recent; len(r) == 0 || r[0] != sw.pair {
		t.Fatalf("remembered %+v", r)
	}
	// Answers for the old org arriving late are dropped.
	next, _ = m.Update(listMsg{from: old, sections: []ado.Section{{}}})
	if next.(Model).sections != nil {
		t.Fatal("a list from the old org should be dropped")
	}
}

func TestOrgPickerTypedOrgAndRemoveRules(t *testing.T) {
	m, p := openedPicker(t)
	for _, k := range []string{"/", "w", "i", "d", "g", "e", "t", "s", "x", "q"} {
		next, _ := m.Update(keyMsg(k))
		m = next.(Model)
	}
	if v := ansi.Strip(p.view(120)); !strings.Contains(v, "open widgetsxq with alex@contoso.com") {
		t.Fatalf("a typed org nothing matches should be offered:\n%s", v)
	}
	_, cmd := m.Update(keyMsg("enter"))
	if sw, ok := cmd().(orgSwitchMsg); !ok || sw.pair != (prefs.Pair{Org: "widgetsxq"}) {
		t.Fatalf("got %#v", sw)
	}

	// d on az's own login, or on the account in use, explains instead.
	m, p = openedPicker(t)
	p.list.cursor = 0 // contoso with alex, az's own and current
	_, cmd = m.Update(keyMsg("d"))
	if msg, ok := cmd().(statusMsg); !ok || !strings.Contains(string(msg), "az logout") {
		t.Fatalf("got %#v", cmd())
	}
	p.list.cursor = 2 // fabrikam with sam, an added account
	next, _ := m.Update(keyMsg("d"))
	if c, ok := next.(Model).modal.(*confirmModal); !ok || !strings.Contains(c.prompt, "Remove the account sam@fabrikam.com?") {
		t.Fatalf("d on an added account should ask, got %T", next.(Model).modal)
	}
}

func TestTitleNamesTheAccountWhenThereAreSeveral(t *testing.T) {
	m := New(ado.NewClient("contoso"), time.Minute)
	m.width, m.me = 160, ado.Identity{DisplayName: "Alex Kim", UniqueName: "alex@contoso.com"}
	m.manyAccounts = false
	if !strings.Contains(ansi.Strip(m.titleLine()), "contoso · Alex Kim") {
		t.Fatal(ansi.Strip(m.titleLine()))
	}
	m.manyAccounts = true
	line := ansi.Strip(m.titleLine())
	if !strings.Contains(line, "contoso · alex@contoso.com") {
		t.Fatal(line)
	}
	x := strings.Index(line, "contoso")
	if !m.onOrgName(x) || m.onOrgName(x-2) {
		t.Fatal("a click on the org name should count, beside it not")
	}
}
