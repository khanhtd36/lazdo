package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/update"
)

type yesMsg struct{}

func yesCmd() tea.Msg { return yesMsg{} }

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// said reports whether pressing keys on a fresh question answers yes.
func said(c *confirmModal, keys ...string) (yes, open bool) {
	var m modal = c
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = m.update(key(k))
		if m == nil {
			break
		}
	}
	return cmd != nil, m != nil
}

func TestConfirmButtons(t *testing.T) {
	for _, tc := range []struct {
		name      string
		safe      bool
		keys      []string
		yes, open bool
	}{
		{"enter on a risky question is No", false, []string{"enter"}, false, false},
		{"enter on a safe question is Yes", true, []string{"enter"}, true, false},
		{"left then enter is Yes", false, []string{"left", "enter"}, true, false},
		{"tab walks to Yes", false, []string{"tab", "enter"}, true, false},
		{"right then enter is No", true, []string{"l", "enter"}, false, false},
		{"y still says yes", false, []string{"y"}, true, false},
		{"n still says no", true, []string{"n"}, false, false},
		{"esc is No", true, []string{"esc"}, false, false},
		{"other keys wait", true, []string{"x"}, false, true},
	} {
		c := newConfirm("Delete tag v1?", yesCmd)
		if tc.safe {
			c.focusYes()
		}
		yes, open := said(c, tc.keys...)
		if yes != tc.yes || open != tc.open {
			t.Errorf("%s: yes %v open %v", tc.name, yes, open)
		}
	}
}

func TestConfirmNoGoesBack(t *testing.T) {
	menu := &menuModal{title: "Re-queue"}
	c := newConfirm("Re-queue web?", yesCmd).focusYes()
	c.back = menu
	if next, _ := c.update(key("right")); next != c {
		t.Fatal("moving stays on the question")
	}
	if next, _ := c.update(key("enter")); next != menu {
		t.Fatalf("No should return to the menu, got %T", next)
	}
}

func TestConfirmShowsTwoButtons(t *testing.T) {
	c := newConfirm("Abandon !7 fix: something?", yesCmd)
	view := c.view(80)
	lines := strings.Split(ansi.Strip(view), "\n")
	last := lines[len(lines)-2] // above the bottom border
	if !strings.Contains(last, " y Yes ") || !strings.Contains(last, " n No ") {
		t.Fatalf("buttons row %q", last)
	}
	if !strings.Contains(view, styleButtonCTA.Render("n No")) || !strings.Contains(view, styleButton.Render("y Yes")) {
		t.Fatal("No is focused (blue), Yes is not (grey)")
	}
	// Centered under the question.
	text := strings.TrimRight(strings.Trim(last, "│"), " ")
	if pad := len(text) - len(strings.TrimLeft(text, " ")); pad < 5 {
		t.Fatalf("buttons should be centered: %q", last)
	}
}

func TestClickingConfirmButtonsInThePR(t *testing.T) {
	for _, tc := range []struct {
		label string
		yes   bool
	}{{"y Yes", true}, {"n No", false}} {
		d := fakeDetail(t, 120)
		d.modal = newConfirm("Delete this comment?", yesCmd)
		box := d.modal.view(d.width)
		h, w := strings.Count(box, "\n")+1, ansi.StringWidth(firstLine(box))
		top, left := d.bodyTop()+(d.bodyHeight()-h)/2, (d.width-w)/2
		row := ansi.Strip(strings.Split(box, "\n")[h-2])
		x := left + ansi.StringWidth(row[:strings.Index(row, tc.label)])
		cmd := d.onMouse(tea.MouseMsg{X: x + 1, Y: top + h - 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if (cmd != nil) != tc.yes || d.modal != nil {
			t.Errorf("click %s: yes %v, modal %T", tc.label, cmd != nil, d.modal)
		}
	}
}

func TestClickingConfirmButtonsOverAPage(t *testing.T) {
	m := dashboardWith(t, 7)
	m.modal = newConfirm("Delete branch fix/x?", yesCmd)
	box := m.modal.view(m.width)
	h, w := strings.Count(box, "\n")+1, ansi.StringWidth(firstLine(box))
	top, left := (m.height-h)/2, (m.width-w)/2
	row := ansi.Strip(strings.Split(box, "\n")[h-2])
	x := left + ansi.StringWidth(row[:strings.Index(row, "y Yes")])
	next, cmd := m.Update(tea.MouseMsg{X: x + 2, Y: top + h - 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil || next.(Model).modal != nil {
		t.Fatal("clicking Yes should answer yes and close")
	}
	m.modal = newConfirm("Delete branch fix/x?", yesCmd)
	next, cmd = m.Update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd != nil || next.(Model).modal != nil {
		t.Fatal("a click outside is No")
	}
}

func TestUpdateDialogButtons(t *testing.T) {
	u := &updateModal{releases: []update.Release{{Tag: "v9.9.9"}}, how: update.ByScript}
	if !strings.Contains(u.view(100), styleButtonCTA.Render("y Update")) {
		t.Fatalf("Update is focused:\n%s", ansi.Strip(u.view(100)))
	}
	if next, cmd := u.update(key("enter")); next != nil || cmd == nil {
		t.Fatal("enter starts the update")
	}
	u.update(key("right"))
	if next, cmd := u.update(key("enter")); next != nil || cmd != nil {
		t.Fatal("Later closes without updating")
	}
}
