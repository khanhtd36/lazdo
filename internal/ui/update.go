package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/update"
)

// buildVersion is this build's, for finding newer releases; "dev" finds none.
var buildVersion = "dev"

// SetVersion tells the UI which release is running.
func SetVersion(v string) { buildVersion = v }

// refreshUpdates asks GitHub for newer releases now and installUpdate puts
// one in place; tests replace them.
var (
	refreshUpdates = update.Refresh
	installUpdate  = update.Install
)

type (
	// updatesMsg brings the newer releases; manual when U asked for them.
	updatesMsg struct {
		releases []update.Release
		manual   bool
		err      error
	}
	// updateStartMsg is y in the update dialog.
	updateStartMsg struct {
		release update.Release
		exe     string
	}
	// updateTickMsg redraws the title while an update downloads.
	updateTickMsg struct{}
	updateDoneMsg struct {
		version string
		err     error
	}
)

// updateRun is an update in progress; the download writes its step, the
// title reads it.
type updateRun struct {
	version string
	step    atomic.Pointer[update.Step]
}

func (r *updateRun) String() string {
	if s := r.step.Load(); s != nil {
		return s.String()
	}
	return "starting"
}

// updateFailure is an update that failed, shown atop the next update dialog.
type updateFailure struct {
	version string
	err     error
}

// checkUpdates looks for newer releases in the background, asking GitHub
// at most once a day; a failed check stays silent.
func checkUpdates() tea.Cmd {
	return func() tea.Msg {
		rs, _ := update.Check(context.Background(), buildVersion)
		return updatesMsg{releases: rs}
	}
}

// updateNote is the title line's word on updates: one downloading, one
// installed, one failed, or a newer release out.
func (m Model) updateNote() string {
	var note string
	switch {
	case m.updating != nil:
		note = "updating v" + m.updating.version + " " + strings.TrimPrefix(m.updating.String(), "downloading ")
	case m.installed != "":
		note = "v" + m.installed + " installed, restart lazdo"
	case m.updateFailed != nil:
		return styleRed.Render("  update failed (U)")
	case len(m.updates) > 0:
		note = pick2(useSymbols, symPush+" ", "update ") + "v" + m.updates[0].Version() + " (U)"
	default:
		return ""
	}
	return styleCyan.Render("  " + note)
}

// checkUpdatesNow is U: it asks GitHub now, not the daily check's cache,
// which can be a day behind, then opens the update dialog or says lazdo
// is the latest. While a check or an update runs it says so instead.
func (m Model) checkUpdatesNow() (tea.Model, tea.Cmd) {
	switch {
	case m.checkingUpdates:
		return m, statusCmd("already checking for updates…")
	case m.updating != nil:
		return m, statusCmd("already updating to v" + m.updating.version + ": " + m.updating.String())
	case m.installed != "":
		return m, statusCmd("v" + m.installed + " is installed, restart lazdo to use it")
	}
	m.checkingUpdates, m.status = true, "checking for updates…"
	return m, func() tea.Msg {
		rs, err := refreshUpdates(context.Background(), buildVersion)
		return updatesMsg{releases: rs, manual: true, err: err}
	}
}

// onUpdates takes a check's answer: the title's hint follows it, and the
// answer to U opens the dialog, with the last failed update atop it.
func (m Model) onUpdates(msg updatesMsg) (tea.Model, tea.Cmd) {
	if !msg.manual {
		m.updates = msg.releases
		return m, nil
	}
	m.checkingUpdates, m.status = false, ""
	failed := m.updateFailed
	m.updateFailed = nil
	if msg.err != nil {
		return m.Update(resultMsg(fmt.Errorf("check for updates: %w", msg.err), ""))
	}
	m.updates = msg.releases
	return m.openUpdate(failed)
}

// openUpdate shows what the newer releases change and offers to install.
func (m Model) openUpdate(failed *updateFailure) (tea.Model, tea.Cmd) {
	if len(m.updates) == 0 {
		return m, statusCmd(fmt.Sprintf("lazdo %s is the latest", buildVersion))
	}
	exe, err := os.Executable()
	if err != nil {
		return m, statusCmd("error: " + err.Error())
	}
	m.modal = &updateModal{releases: m.updates, exe: exe, how: update.HowInstalled(exe), failed: failed}
	return m, nil
}

// startUpdate downloads and installs a release in the background; the
// title follows its progress.
func (m Model) startUpdate(msg updateStartMsg) (tea.Model, tea.Cmd) {
	if m.updating != nil {
		return m, nil
	}
	run := &updateRun{version: msg.release.Version()}
	m.updating, m.updateFailed = run, nil
	install := func() tea.Msg {
		err := installUpdate(context.Background(), msg.release, msg.exe, func(s update.Step) { run.step.Store(&s) })
		return updateDoneMsg{version: run.version, err: err}
	}
	return m, tea.Batch(install, updateTick())
}

func updateTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return updateTickMsg{} })
}

func (m Model) onUpdateDone(msg updateDoneMsg) (tea.Model, tea.Cmd) {
	m.updating = nil
	if msg.err != nil {
		m.updateFailed = &updateFailure{version: msg.version, err: msg.err}
		return m.Update(resultMsg(fmt.Errorf("update: %w", msg.err), ""))
	}
	m.installed, m.updates = msg.version, nil
	return m.Update(resultMsg(nil, "updated to v"+msg.version+", restart lazdo to use it"))
}

type updateModal struct {
	releases []update.Release
	exe      string
	how      update.Method
	failed   *updateFailure // the last try, when it failed
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
		start := updateStartMsg{release: u.releases[0], exe: u.exe}
		return nil, func() tea.Msg { return start }
	}
	return u, nil
}

func (u *updateModal) view(width int) string {
	w := min(100, max(40, width-10))
	latest := u.releases[0].Version()
	lines := []string{styleSection.Render(fmt.Sprintf("lazdo v%s → v%s", buildVersion, latest)), ""}
	if f := u.failed; f != nil {
		lines = append(lines, styleRed.Render(truncate(fmt.Sprintf("last try (v%s) failed: %v", f.version, f.err), w)), "")
	}
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

// versionLabel is the running build for display: "v0.2.19", or "dev".
func versionLabel() string {
	if buildVersion == "dev" || buildVersion == "" {
		return "dev"
	}
	return "v" + strings.TrimPrefix(buildVersion, "v")
}
