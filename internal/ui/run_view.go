package ui

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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
	find    textFind // / find in the log
	cur     int      // cursor line in the log
	anchor  int      // first line of a V selection, -1 when none
	drag    int      // line a mouse drag started on
	height  int      // log rows last shown, for following new output
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
	return &runView{client: client, project: p, run: r, follow: true, anchor: -1}
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
				v.cur = max(0, len(v.lines)-1)
				v.logTop = max(0, len(v.lines)-v.height)
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
	r, isRecord := it.value.(ado.TimelineRecord)
	if !ok || !isRecord || r.Log == nil || r.Log.ID == v.logID {
		return nil
	}
	v.logID, v.lines, v.logTop, v.follow = r.Log.ID, nil, 0, r.State == "inProgress"
	v.cur, v.anchor = 0, -1
	return v.loadLog(1)
}

// key handles run keys; handled false means the project should handle it.
func (v *runView) key(msg tea.KeyMsg, height int) (handled bool, cmd tea.Cmd) {
	k := msg.String()
	switch {
	case v.tree.typing: // letters go into the steps filter
		handled, activate := v.tree.key(msg, height)
		v.logPane = activate
		return handled, v.selectLog()
	case v.find.typing:
		v.find.edit(msg)
		v.find.search(v.lines)
		v.showMatch(height)
		return true, nil
	}
	switch k {
	case "tab", "shift+tab":
		v.logPane = !v.logPane
		return true, nil
	case "l", "right":
		v.logPane = true
		return true, nil
	case "h", "left":
		if v.logPane {
			v.logPane = false
			return true, nil
		}
		return false, nil
	}
	if !v.logPane {
		handled, activate := v.tree.key(msg, height)
		if activate {
			v.logPane = true
		}
		return handled, v.selectLog()
	}
	return v.logKey(k, height)
}

// showMatch scrolls the log to the current find match.
func (v *runView) showMatch(height int) {
	if line, ok := v.find.current(); ok {
		v.cur, v.logTop, v.follow = line, max(0, line-height/3), false
	}
}

func (v *runView) logKey(k string, height int) (bool, tea.Cmd) {
	page := max(1, height/2)
	switch k {
	case "esc":
		switch {
		case v.anchor >= 0:
			v.anchor = -1
		case v.find.active():
			v.find = textFind{}
		default:
			v.logPane = false // back to the steps, one level
		}
		return true, nil
	case "/":
		v.find = textFind{typing: true}
		return true, nil
	case "n", "N", "p":
		v.find.next(k == "n")
		v.showMatch(height)
		return true, nil
	case "V":
		if v.anchor >= 0 {
			v.anchor = -1
		} else {
			v.anchor = v.cur
		}
		return true, nil
	case "y":
		return true, v.copyLog()
	case "j", "down":
		v.moveCursor(1, height)
	case "k", "up":
		v.moveCursor(-1, height)
	case "ctrl+d", "pgdown":
		v.moveCursor(page, height)
	case "ctrl+u", "pgup":
		v.moveCursor(-page, height)
	case "g", "home":
		v.moveCursor(-len(v.lines), height)
	case "G", "end":
		v.moveCursor(len(v.lines), height)
	default:
		return false, nil
	}
	return true, nil
}

// moveCursor moves the log's cursor line; sitting on the last line follows
// new output as it arrives.
func (v *runView) moveCursor(delta, height int) {
	v.cur = max(0, min(v.cur+delta, len(v.lines)-1))
	v.follow = v.cur >= len(v.lines)-1
	if v.cur < v.logTop {
		v.logTop = v.cur
	}
	if v.cur >= v.logTop+height {
		v.logTop = v.cur - height + 1
	}
}

// scrollLog moves the view (mouse wheel) without moving the cursor.
func (v *runView) scrollLog(delta, height int) {
	v.logTop = max(0, min(v.logTop+delta, len(v.lines)-height))
	v.follow = false
}

// logText is a log line as shown: no timestamp, no styling.
func logText(line string) string { return ansi.Strip(logLine(line)) }

// copyLog copies the selected lines, or offers the Copy menu without one.
func (v *runView) copyLog() tea.Cmd {
	if v.anchor >= 0 {
		lo, hi := min(v.anchor, v.cur), max(v.anchor, v.cur)
		var out []string
		for i := lo; i <= hi && i < len(v.lines); i++ {
			out = append(out, logText(v.lines[i]))
		}
		v.anchor = -1
		return copyText(strings.Join(out, "\n"), fmt.Sprintf("copied %d %s", len(out), plural(len(out), "line", "lines")))
	}
	if len(v.lines) == 0 {
		return nil
	}
	shown := make([]string, len(v.lines))
	for i, l := range v.lines {
		shown[i] = logText(l)
	}
	return copyMenu("log",
		copyItem{"Current line", shown[min(v.cur, len(shown)-1)]},
		copyItem{"Whole log", strings.Join(shown, "\n")},
		copyItem{"Whole log with timestamps", strings.Join(v.lines, "\n")},
	)
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
				groupStart: r.Type == "Stage" || r.Type == "Job",
				search:     r.Name,
				value:      r,
				render:     func(width int) string { return recordRow(r, depth, width) },
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
	v.height = height
	start := v.logTop
	if v.follow {
		start = max(0, len(v.lines)-height)
		v.logTop = start
	}
	lo, hi := len(v.lines), -1
	if v.anchor >= 0 {
		lo, hi = min(v.anchor, v.cur), max(v.anchor, v.cur)
	}
	for i := range height {
		n := start + i
		if n >= len(v.lines) {
			break
		}
		mark := " "
		switch {
		case n == v.cur && v.logPane:
			mark = styleCursorLine.Render("▌")
		case n >= lo && n <= hi:
			mark = styleRangeLine.Render("┃")
		}
		out[i] = mark + truncate(logLine(v.lines[n]), width-1)
	}
	return out
}
