package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/update"
)

// buildVersion is this build's, for finding newer releases; "dev" finds none.
var buildVersion = "dev"

// SetVersion tells the UI which release is running.
func SetVersion(v string) { buildVersion = v }

type (
	updatesMsg    struct{ releases []update.Release }
	updateDoneMsg struct {
		text string
		err  error
	}
)

// checkUpdates looks for newer releases in the background, asking GitHub
// at most once a day; a failed check stays silent.
func checkUpdates() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rs, _ := update.Check(ctx, buildVersion)
		return updatesMsg{releases: rs}
	}
}

// updateNote is the title line's hint that a newer release is out.
func (m Model) updateNote() string {
	if len(m.updates) == 0 {
		return ""
	}
	note := pick2(useSymbols, symPush+" ", "update ") + "v" + m.updates[0].Version() + " (U)"
	return styleCyan.Render("  " + note)
}

// openUpdate shows what the newer releases change and offers to install.
func (m Model) openUpdate() (tea.Model, tea.Cmd) {
	if len(m.updates) == 0 {
		return m, statusCmd(fmt.Sprintf("lazdo %s is the latest", buildVersion))
	}
	exe, err := os.Executable()
	if err != nil {
		return m, statusCmd("error: " + err.Error())
	}
	m.modal = &updateModal{releases: m.updates, exe: exe, how: update.HowInstalled(exe)}
	return m, nil
}

type updateModal struct {
	releases []update.Release
	exe      string
	how      update.Method
}

const updateChangeRows = 15

func (u *updateModal) update(msg tea.KeyMsg) (modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "n", "N":
		return nil, nil
	case "y", "Y":
		if u.how != update.ByScript {
			return u, nil
		}
		latest, exe := u.releases[0], u.exe
		return nil, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := update.Install(ctx, latest, exe); err != nil {
				return updateDoneMsg{err: fmt.Errorf("update: %w", err)}
			}
			return updateDoneMsg{text: "updated to v" + latest.Version() + ", restart lazdo to use it"}
		}
	}
	return u, nil
}

func (u *updateModal) view(width int) string {
	w := min(100, max(40, width-10))
	latest := u.releases[0].Version()
	lines := []string{styleSection.Render(fmt.Sprintf("lazdo v%s → v%s", buildVersion, latest)), ""}
	changes := update.Changes(u.releases)
	if len(changes) == 0 {
		lines = append(lines, styleDim.Render("No change list was published with this release."))
	}
	for i, c := range changes {
		if i == updateChangeRows {
			lines = append(lines, styleDim.Render(fmt.Sprintf("  …and %d more", len(changes)-i)))
			break
		}
		lines = append(lines, "  "+truncate(c, w-2))
	}
	lines = append(lines, "")
	if cmd := u.how.Command(); cmd != "" {
		lines = append(lines, "This copy is managed elsewhere; update it with:", "  "+styleCyan.Render(cmd), "", styleDim.Render("esc close"))
	} else {
		lines = append(lines, styleDim.Render("y update · n not now"))
	}
	return styleModal.Render(strings.Join(lines, "\n"))
}
