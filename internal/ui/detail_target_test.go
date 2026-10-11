package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func TestTargetPickerListsBranchesAndConfirms(t *testing.T) {
	d := fakeDetail(t, 120)
	m, _ := d.openTargetPicker()
	p := m.(*targetPicker)
	d.modal = p
	d.update(targetBranchesMsg{prID: d.pr.ID, names: []string{"release/2.4", "fix/x", "develop", "main"}, defaultBranch: "main"})

	vis := p.list.visible()
	names := make([]string, 0, len(vis))
	for _, i := range vis {
		names = append(names, p.list.items[i].value.(string))
	}
	if got := strings.Join(names, " "); got != "main develop release/2.4" {
		t.Fatalf("branches = %q, want the default first, then A–Z, without the source", got)
	}
	if it, _ := p.list.selected(); it.value != "develop" {
		t.Fatalf("the current target should be selected, got %v", it.value)
	}

	// Typing filters; enter asks before changing anything.
	for _, r := range "rel" {
		m, _ = p.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	c, ok := m.(*confirmModal)
	if !ok {
		t.Fatalf("enter should ask first, got %T", m)
	}
	if v := ansi.Strip(c.view(120)); !strings.Contains(v, "from develop to release/2.4") || strings.Contains(v, "Auto-complete") {
		t.Fatalf("confirmation = %q", v)
	}
	if back, _ := c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}); back != p {
		t.Fatal("n should return to the picker")
	}

	d.data.AutoCompleteSetBy = &ado.Identity{ID: "me"}
	m, _ = p.pick()
	if v := ansi.Strip(m.view(120)); !strings.Contains(v, "it will merge into release/2.4") {
		t.Fatalf("with auto-complete set the confirmation should warn: %q", v)
	}
}

func TestTargetChangeReloadsTheDetail(t *testing.T) {
	d := fakeDetail(t, 120)
	d.onTargetChanged(targetChangedMsg{branch: "main", err: errors.New("denied")})
	if d.status != "error: change target branch: denied" {
		t.Fatalf("status = %q", d.status)
	}
	if cmd := d.onTargetChanged(targetChangedMsg{branch: "main"}); cmd == nil || d.status != "target changed to main" || !d.loading {
		t.Fatalf("a change should reload: status %q, loading %v", d.status, d.loading)
	}
}

func TestClickTargetBranchOpensPicker(t *testing.T) {
	d := fakeDetail(t, 120)
	line := ansi.Strip(d.subtitleLine())
	x := strings.LastIndex(line, "develop")
	if d.onTarget(x - 2) {
		t.Fatal("left of the target branch should not count")
	}
	d.onMouse(tea.MouseMsg{X: x + 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if _, ok := d.modal.(*targetPicker); !ok {
		t.Fatalf("a click on the target branch should open the picker, got %T", d.modal)
	}
}
