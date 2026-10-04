package ui

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// runView shows one run: its stages, jobs and steps on the left and the
// selected step's log on the right, following new lines while it runs.
type runView struct {
	client  *ado.Client
	project ado.ProjectInfo
	run     ado.Run

	records []ado.TimelineRecord
	tree    pickList
	loading bool
	err     error

	logID   int
	lines   []string
	logTop  int
	follow  bool // stick to the end as lines arrive
	logPane bool // focus is on the log
	logErr  error
}

type (
	runLoadedMsg struct {
		runID   int
		run     ado.Run
		records []ado.TimelineRecord
		err     error
	}
	logMsg struct {
		runID, logID int
		start        int // first line number fetched, 1-based
		lines        []string
		err          error
	}
	runTickMsg struct{ runID int }
)

func newRunView(client *ado.Client, p ado.ProjectInfo, r ado.Run) *runView {
	return &runView{client: client, project: p, run: r, follow: true}
}

func (v *runView) inProgress() bool { return v.run.Status != "completed" }

func (v *runView) refresh() tea.Cmd {
	v.loading = true
	client, projectID, id := v.client, v.project.ID, v.run.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		run, err := client.Run(ctx, projectID, id)
		if err != nil {
			return runLoadedMsg{runID: id, err: err}
		}
		records, err := client.Timeline(ctx, projectID, id)
		return runLoadedMsg{runID: id, run: run, records: records, err: err}
	}
}

// loadLog fetches the selected step's log from line start on.
func (v *runView) loadLog(start int) tea.Cmd {
	if v.logID == 0 {
		return nil
	}
	client, projectID, runID, logID := v.client, v.project.ID, v.run.ID, v.logID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		lines, err := client.LogLines(ctx, projectID, runID, logID, start)
		return logMsg{runID: runID, logID: logID, start: start, lines: lines, err: err}
	}
}

func (v *runView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case runLoadedMsg:
		if msg.runID != v.run.ID {
			return nil
		}
		v.loading, v.err = false, msg.err
		if msg.err != nil {
			return v.poll()
		}
		first := v.records == nil
		v.run, v.records = msg.run, msg.records
		v.tree.setItems(v.treeItems())
		if first {
			v.selectInteresting()
			return tea.Batch(v.selectLog(), v.poll())
		}
		// Live: fetch only lines after the ones shown.
		return tea.Batch(v.loadLog(len(v.lines)+1), v.poll())
	case logMsg:
		if msg.runID != v.run.ID || msg.logID != v.logID {
			return nil
		}
		v.logErr = msg.err
		if msg.err == nil {
			if msg.start <= 1 {
				v.lines = msg.lines
			} else if msg.start == len(v.lines)+1 {
				v.lines = append(v.lines, msg.lines...)
			}
			if v.follow {
				v.logTop = max(0, len(v.lines)-1)
			}
		}
	case runTickMsg:
		if msg.runID == v.run.ID && !v.loading {
			return v.refresh()
		}
	}
	return nil
}

// poll schedules the next refresh while the run is still going.
func (v *runView) poll() tea.Cmd {
	if !v.inProgress() {
		return nil
	}
	id := v.run.ID
	return tea.Tick(runPollInterval, func(time.Time) tea.Msg { return runTickMsg{runID: id} })
}

// selectInteresting puts the cursor on the first failed step, else the
// running one, else the last step with a log.
func (v *runView) selectInteresting() {
	failed, running, last := -1, -1, -1
	for i, idx := range v.tree.visible() {
		r, _ := v.tree.items[idx].value.(ado.TimelineRecord)
		if r.Log == nil || r.Type != "Task" {
			continue
		}
		last = i
		if r.Result == "failed" && failed < 0 {
			failed = i
		}
		if r.State == "inProgress" && running < 0 {
			running = i
		}
	}
	for _, i := range []int{failed, running, last} {
		if i >= 0 {
			v.tree.cursor = i
			return
		}
	}
}

// selectLog switches the log pane to the selected record's log.
func (v *runView) selectLog() tea.Cmd {
	it, ok := v.tree.selected()
	if !ok {
		return nil
	}
	r := it.value.(ado.TimelineRecord)
	if r.Log == nil || r.Log.ID == v.logID {
		return nil
	}
	v.logID, v.lines, v.logTop, v.follow = r.Log.ID, nil, 0, r.State == "inProgress"
	return v.loadLog(1)
}

// key handles run keys; handled false means the project should handle it.
func (v *runView) key(msg tea.KeyMsg, height int) (handled bool, cmd tea.Cmd) {
	k := msg.String()
	switch k {
	case "tab", "h", "l", "left", "right":
		v.logPane = k == "l" || k == "right" || (k == "tab" && !v.logPane)
		return true, nil
	}
	if !v.logPane {
		handled, activate := v.tree.key(msg, height)
		if activate {
			v.logPane = true
		}
		return handled, v.selectLog()
	}
	return v.logKey(k, height), nil
}

func (v *runView) logKey(k string, height int) bool {
	page := max(1, height/2)
	switch k {
	case "j", "down":
		v.scrollLog(1, height)
	case "k", "up":
		v.scrollLog(-1, height)
	case "ctrl+d", "pgdown":
		v.scrollLog(page, height)
	case "ctrl+u", "pgup":
		v.scrollLog(-page, height)
	case "g", "home":
		v.logTop, v.follow = 0, false
	case "G", "end":
		v.logTop, v.follow = max(0, len(v.lines)-height), true
	default:
		return false
	}
	return true
}

func (v *runView) scrollLog(delta, height int) {
	v.logTop = max(0, min(v.logTop+delta, len(v.lines)-1))
	v.follow = v.logTop >= len(v.lines)-height
}

// treeItems nests stages, jobs and steps; phases are an implementation
// detail of the timeline and are flattened into their stage.
func (v *runView) treeItems() []pickItem {
	children := map[string][]ado.TimelineRecord{}
	for _, r := range v.records {
		children[r.ParentID] = append(children[r.ParentID], r)
	}
	for k := range children {
		sort.SliceStable(children[k], func(i, j int) bool { return children[k][i].Order < children[k][j].Order })
	}
	var items []pickItem
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, r := range children[parent] {
			switch {
			case r.Type == "Checkpoint":
				continue
			case r.Type == "Phase" || (r.Type == "Stage" && r.Name == "__default"):
				walk(r.ID, depth) // flatten
				continue
			}
			items = append(items, pickItem{
				search: r.Name,
				value:  r,
				render: func(width int) string { return recordRow(r, depth, width) },
			})
			walk(r.ID, depth+1)
		}
	}
	walk("", 0)
	return items
}

func recordRow(r ado.TimelineRecord, depth, width int) string {
	name := r.Name
	if r.Type == "Stage" || r.Type == "Job" {
		name = styleTitle.Render(name)
	}
	return truncate(joinCols(strings.Repeat("  ", depth)+runGlyph(r.State, r.Result)+" "+name,
		styleDim.Render(duration(r.StartTime, r.FinishTime))), width)
}

var logTimestamp = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+Z `)

// logLine drops the timestamp and colors Azure Pipelines markers.
func logLine(s string) string {
	s = logTimestamp.ReplaceAllString(s, "")
	switch {
	case strings.HasPrefix(s, "##[error]"):
		return styleRed.Render(strings.TrimPrefix(s, "##[error]"))
	case strings.HasPrefix(s, "##[warning]"):
		return styleYellow.Render(strings.TrimPrefix(s, "##[warning]"))
	case strings.HasPrefix(s, "##[section]"):
		return styleHeader.Render(strings.TrimPrefix(s, "##[section]"))
	case strings.HasPrefix(s, "##[command]"):
		return styleCyan.Render(strings.TrimPrefix(s, "##[command]"))
	case strings.HasPrefix(s, "##[group]"):
		return styleSection.Render("▾ " + strings.TrimPrefix(s, "##[group]"))
	case strings.HasPrefix(s, "##[endgroup]"):
		return ""
	}
	return s
}

func (v *runView) view(width, height int) []string {
	treeW := min(48, max(24, width*35/100))
	logW := width - treeW - 1
	tree := v.tree.view(treeW, height)
	logLines := v.logView(logW, height)
	out := make([]string, height)
	for i := range height {
		out[i] = fit(tree[i], treeW) + styleDim.Render("│") + logLines[i]
	}
	return out
}

func (v *runView) logView(width, height int) []string {
	out := make([]string, height)
	switch {
	case v.err != nil && v.records == nil:
		out[0] = styleRed.Render(truncate("error: "+v.err.Error(), width))
		return out
	case v.logErr != nil:
		out[0] = styleRed.Render(truncate("error: "+v.logErr.Error(), width))
		return out
	case v.logID == 0:
		out[0] = styleDim.Render(" select a step with a log")
		return out
	case v.lines == nil:
		out[0] = styleDim.Render(" loading log…")
		return out
	}
	start := v.logTop
	if v.follow {
		start = max(0, len(v.lines)-height)
	}
	for i := range height {
		n := start + i
		if n >= len(v.lines) {
			break
		}
		out[i] = " " + truncate(logLine(v.lines[n]), width-1)
	}
	return out
}
