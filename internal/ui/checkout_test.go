package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/actions"
	"github.com/khanhtd36/lazdo/internal/ado"
)

func TestCheckoutDialogPlansAndSuggests(t *testing.T) {
	m := New(ado.NewClient("org"), time.Minute)
	m.width, m.height = 120, 30
	next, _ := m.Update(checkoutRequestMsg{org: "org", project: "proj", repo: "repo", branch: "feat/x"})
	m = next.(Model)
	c, ok := m.modal.(*checkoutModal)
	if !ok {
		t.Fatalf("checkout request should open the dialog, got %T", m.modal)
	}
	wd, _ := os.Getwd()
	if got := c.input.Value(); got != filepath.Join(wd, "repo") {
		t.Fatalf("outside a clone the suggestion is ./repo, got %q", got)
	}
	if c.plan.Kind != actions.PlanClone || !strings.Contains(ansi.Strip(m.View()), "clone repo into a new folder") {
		t.Fatalf("a missing folder should preview a clone:\n%s", ansi.Strip(m.View()))
	}

	// A non-empty folder that isn't a clone is refused and enter does nothing.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.input.SetValue(dir)
	c.replan()
	next, cmd := m.Update(keyMsg("enter"))
	if cmd != nil || next.(Model).modal == nil || !strings.Contains(ansi.Strip(next.View()), "not a clone") {
		t.Fatal("a refused plan must not run")
	}

	// The last path used for a repo is remembered and suggested.
	next, _ = next.Update(checkoutDoneMsg{repoKey: c.key, path: dir, text: "switched"})
	m = next.(Model)
	if m.modal != nil {
		t.Fatal("done closes the dialog")
	}
	got := checkoutSuggestions(c.key, "repo", m.lastCheckout)
	if len(got) < 2 || got[0] != dir {
		t.Fatalf("last path should be suggested first outside a clone: %v", got)
	}
}
