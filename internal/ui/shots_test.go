package ui

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// TestShots renders the README screenshots from made-up data into SHOTS_DIR,
// as HTML pages scripts/shots.sh turns into images. No real people or projects.
func TestShots(t *testing.T) {
	dir := os.Getenv("SHOTS_DIR")
	if dir == "" {
		t.Skip("set SHOTS_DIR")
	}
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	UseSymbols(true)
	defer UseSymbols(false)
	const w, h = 142, 26
	save := func(name, view string) {
		if err := os.WriteFile(filepath.Join(dir, name+".html"), []byte(ansiHTML(view, w)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	client := ado.NewClient("demo")
	me := ado.Identity{ID: "me", DisplayName: "Alex Kim"}
	sam, priya, jo := ado.Identity{ID: "sam", DisplayName: "Sam Rivera"}, ado.Identity{ID: "priya", DisplayName: "Priya Shah"}, ado.Identity{ID: "jo", DisplayName: "Jo Tanaka"}
	repo := ado.Repository{ID: "r1", Name: "demo-app", Project: ado.Project{ID: "p1", Name: "Demo"}}
	now := time.Now()
	pr := func(id int, title string, by ado.Identity, draft, reviewing bool, age time.Duration) ado.PullRequest {
		p := ado.PullRequest{
			ID: id, Title: title, CreatedBy: by, IsDraft: draft, Repository: repo,
			SourceRefName: "refs/heads/feat/x", TargetRefName: "refs/heads/develop", CreationDate: now.Add(-age),
		}
		if reviewing {
			p.Reviewers = []ado.Reviewer{{Identity: me}}
		}
		return p
	}

	// --- Pull requests page ---
	waiting := pr(1190, "refactor(api): split the handlers by resource", sam, false, false, 2*24*time.Hour)
	waiting.Reviewers = []ado.Reviewer{{Identity: me, Vote: ado.VoteWaitingForAuthor}}
	prs := []ado.PullRequest{
		pr(1204, "feat(export): stream large exports instead of buffering them", sam, false, true, 3*time.Hour),
		pr(1199, "fix(auth): refresh the token before it expires", priya, false, true, 26*time.Hour),
		pr(1187, "perf(charts): render 1M points with downsampling", jo, true, true, 5*24*time.Hour),
		pr(1210, "docs: explain the release process", me, false, false, 40*time.Minute),
		waiting,
	}
	m := New(client, time.Minute)
	m.me, m.loading = me, false
	m.fetchedAt = time.Date(2026, 10, 10, 9, 41, 0, 0, time.Local) // the same clock each run
	m.sections = ado.Classify("me", prs, nil)
	m.stats = map[int]ado.Stats{
		1204: {Visited: false, Comments: 2},
		1199: {Visited: true, NewComments: 2, Comments: 5, ActiveComments: 1},
		1187: {Visited: true, NewPushes: 3, Comments: 0},
		1210: {Visited: true, Comments: 1},
		1190: {Visited: true, NewPushes: 1, Comments: 4, ActiveComments: 2},
	}
	m.builds = map[int]buildResult{1204: {state: ado.BuildRunning}, 1199: {state: ado.BuildPassed}, 1187: {state: ado.BuildNone}, 1210: {state: ado.BuildPassed}, 1190: {state: ado.BuildFailed}}
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	m.cursor = 2
	save("1-pull-requests", m.View())

	// --- Pull request detail ---
	d := fakeFiles(t, w)
	d.resize(w, h)
	d.pr.Title, d.pr.CreatedBy = "feat(export): stream large exports instead of buffering them", sam
	d.data.PullRequest = d.pr
	d.tab = tabOverview
	d.rebuildOverview()
	save("2-pr-overview", d.view())
	d.tab = tabFiles
	d.files.pane = paneDiff
	save("3-pr-files", d.view())

	// --- Projects page ---
	m.page = pageProjects
	m.projects = projectsPage{}
	m.projects.onLoaded(projectsLoadedMsg{
		projects: []ado.ProjectInfo{{ID: "p1", Name: "Demo", Description: "the demo app"}, {ID: "p2", Name: "Tools"}, {ID: "p3", Name: "Archive"}},
		recent:   []string{"p1"},
		repos:    []ado.Repo{{ID: "r1", Name: "demo-app", Project: ado.Project{ID: "p1", Name: "Demo"}, DefaultBranch: "refs/heads/develop"}},
		prs:      prs,
	})
	save("4-projects", m.View())

	// --- Repo browser ---
	r := ado.Repo{ID: "r1", Name: "demo-app", Project: ado.Project{ID: "p1", Name: "Demo"}, DefaultBranch: "refs/heads/develop"}
	pm := newProject(client, ado.ProjectInfo{ID: "p1", Name: "Demo"}, prs, []ado.Repo{r})
	pm.width, pm.height = w, h
	pm.level, pm.tab = levelTabs, projTabPRs
	b := newRepoBrowser(client, pm.project, r)
	b.update(branchesMsg{repoID: "r1", branches: []ado.Branch{{Name: "develop", IsBaseVersion: true}, {Name: "feat/streaming-export", AheadCount: 4, BehindCount: 1}, {Name: "fix/token-refresh", AheadCount: 1}, {Name: "release/2.4"}}})
	b.update(folderMsg{repoID: "r1", branch: "develop", folder: "/", items: []ado.RepoItem{
		{Path: "/cmd", IsFolder: true}, {Path: "/internal", IsFolder: true}, {Path: "/README.md"}, {Path: "/go.mod"}, {Path: "/main.go"},
	}})
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n// main streams the export to stdout.\nfunc main() {\n\tif err := run(os.Args[1:]); err != nil {\n\t\tfmt.Fprintln(os.Stderr, err)\n\t\tos.Exit(1)\n\t}\n}\n"
	b.file = "/main.go"
	b.contents[folderKey("develop", "/main.go")] = &fileContent{raw: splitLines(src), hl: highlight("main.go", src)}
	b.pane = paneContent
	pm.level, pm.repo, pm.browser = levelRepo, r, b
	save("5-repo-files", pm.view())

	// --- Pipeline run ---
	run := ado.Run{ID: 7, BuildNumber: "20261010.3", Status: "completed", Result: "succeeded", SourceBranch: "refs/heads/develop"}
	v := newRunView(client, pm.project, run)
	logRec := func(id, parent, typ, name string, order int, result string, log int) ado.TimelineRecord {
		rec := ado.TimelineRecord{ID: id, ParentID: parent, Type: typ, Name: name, Order: order, Result: result, State: "completed"}
		if log > 0 {
			rec.Log = &struct {
				ID int `json:"id"`
			}{ID: log}
		}
		return rec
	}
	v.update(runLoadedMsg{runID: 7, run: run, records: []ado.TimelineRecord{
		logRec("j1", "", "Job", "Build", 1, "succeeded", 0),
		logRec("t1", "j1", "Task", "Checkout", 1, "succeeded", 2),
		logRec("t2", "j1", "Task", "Restore packages", 2, "succeeded", 3),
		logRec("t3", "j1", "Task", "Build", 3, "succeeded", 4),
		logRec("t4", "j1", "Task", "Run tests", 4, "succeeded", 5),
		logRec("j2", "", "Job", "Publish", 2, "succeeded", 0),
		logRec("t5", "j2", "Task", "Upload artifact", 1, "succeeded", 6),
	}})
	v.update(logMsg{runID: 7, logID: v.logID, start: 1, lines: []string{
		"2026-10-10T08:03:40.2Z ==> Uploading artifact demo-app-2.4.0.zip",
		"2026-10-10T08:03:41.0Z Found 1 file, 18.4 MB",
		"2026-10-10T08:03:44.7Z Uploaded 18.4 MB in 3.7s",
		"2026-10-10T08:03:44.8Z Artifact demo-app published to the run",
		"2026-10-10T08:03:44.9Z Finishing: Upload artifact",
	}})
	pm.level, pm.run, pm.pipeline, pm.tab = levelRun, v, ado.Pipeline{ID: 3, Name: "CI"}, projTabPipelines
	save("6-pipeline-run", pm.view())

	// --- Settings ---
	s := newSettingsView(client, pm.project)
	policy := func(raw string) ado.PolicyConfig {
		var p ado.PolicyConfig
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	scope := `"scope":[{"refName":"refs/heads/develop","matchKind":"Exact","repositoryId":null}]`
	s.update(settingsLoadedMsg{projectID: "p1", data: &ado.ProjectSettings{
		Overview:  ado.Overview{Name: "Demo", Visibility: "private", Process: "Basic", VersionControl: "Git"},
		Teams:     []ado.Team{{ID: "t1", Name: "Demo Team"}},
		Groups:    []ado.Group{{Name: "Contributors"}, {Name: "Project Administrators"}, {Name: "Readers"}},
		Repos:     []ado.Repo{r},
		Names:     map[string]string{"sam": "Sam Rivera"},
		Pipelines: map[int]string{3: "CI"},
		Policies: []ado.PolicyConfig{
			policy(`{"isEnabled":true,"isBlocking":true,"type":{"id":"fa4e907d-c16b-4a4c-9dfa-4906e5d171dd"},"settings":{"minimumApproverCount":1,` + scope + `}}`),
			policy(`{"isEnabled":true,"isBlocking":true,"type":{"id":"0609b952-1397-4640-95ec-e00a01b2c241"},"settings":{"buildDefinitionId":3,"validDuration":720,` + scope + `}}`),
			policy(`{"isEnabled":true,"isBlocking":true,"type":{"id":"fa4e907d-c16b-4a4c-9dfa-4916e5d171ab"},"settings":{"allowSquash":true,"allowRebase":true,` + scope + `}}`),
			policy(`{"isEnabled":true,"isBlocking":false,"type":{"id":"fd2167ab-b0be-447a-8ec8-39368250530e"},"settings":{"requiredReviewerIds":["sam"],` + scope + `}}`),
			policy(`{"isEnabled":false,"isBlocking":true,"type":{"id":"c6a1889d-b943-4856-b76f-9e46bb6b0df2"},"settings":{` + scope + `}}`),
		},
		Errs: map[string]error{},
	}})
	s.section = secPolicies
	s.sections.cursor = int(secPolicies)
	s.content.setItems(s.contentItems())
	s.onRight = true
	pm.level, pm.tab, pm.settings = levelTabs, projTabSettings, s
	save("7-settings", pm.view())
}

// campbell is Windows Terminal's default palette for the 16 basic colors.
var campbell = [16]string{
	"#0C0C0C", "#C50F1F", "#13A10E", "#C19C00", "#0037DA", "#881798", "#3A96DD", "#CCCCCC",
	"#767676", "#E74856", "#16C60C", "#F9F1A5", "#3B78FF", "#B4009E", "#61D6D6", "#F2F2F2",
}

func color256(n int) string {
	switch {
	case n < 16:
		return campbell[n]
	case n < 232:
		n -= 16
		level := []int{0, 95, 135, 175, 215, 255}
		return fmt.Sprintf("#%02x%02x%02x", level[n/36], level[n/6%6], level[n%6])
	}
	g := 8 + (n-232)*10
	return fmt.Sprintf("#%02x%02x%02x", g, g, g)
}

// ansiHTML turns a rendered screen into HTML that looks like the terminal:
// Cascadia Mono, so box-drawing lines join as they do on screen.
func ansiHTML(view string, cols int) string {
	var b strings.Builder
	fg, bg, bold, under := "", "", false, false
	open := false
	flush := func() {
		if open {
			b.WriteString("</span>")
			open = false
		}
		var st []string
		if fg != "" {
			st = append(st, "color:"+fg)
		}
		if bg != "" {
			st = append(st, "background:"+bg)
		}
		if bold {
			st = append(st, "font-weight:bold")
		}
		if under {
			st = append(st, "text-decoration:underline")
		}
		if len(st) > 0 {
			b.WriteString(`<span style="` + strings.Join(st, ";") + `">`)
			open = true
		}
	}
	for i := 0; i < len(view); {
		if view[i] == 0x1b && i+1 < len(view) && view[i+1] == '[' {
			j := i + 2
			for j < len(view) && view[j] != 'm' {
				j++
			}
			codes := strings.Split(view[i+2:j], ";")
			for k := 0; k < len(codes); k++ {
				n, _ := strconv.Atoi(codes[k])
				switch {
				case n == 0:
					fg, bg, bold, under = "", "", false, false
				case n == 1:
					bold = true
				case n == 4:
					under = true
				case n == 22:
					bold = false
				case n == 24:
					under = false
				case n >= 30 && n <= 37:
					fg = campbell[n-30]
				case n >= 90 && n <= 97:
					fg = campbell[n-90+8]
				case n >= 40 && n <= 47:
					bg = campbell[n-40]
				case n == 39:
					fg = ""
				case n == 49:
					bg = ""
				case (n == 38 || n == 48) && k+2 < len(codes) && codes[k+1] == "5":
					c, _ := strconv.Atoi(codes[k+2])
					if n == 38 {
						fg = color256(c)
					} else {
						bg = color256(c)
					}
					k += 2
				}
			}
			flush()
			i = j + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(view[i:])
		if r > 0x7f { // one cell, whatever the fallback font says
			b.WriteString("<i>" + html.EscapeString(string(r)) + "</i>")
		} else {
			b.WriteString(html.EscapeString(string(r)))
		}
		i += size
	}
	if open {
		b.WriteString("</span>")
	}
	return `<html><head><style>body{margin:0;background:#0C0C0C}i{font-style:normal;display:inline-block;width:1ch;height:1em;vertical-align:top;overflow:visible}pre{margin:0;padding:14px 18px;color:#CCCCCC;` +
		`font:15px/1.0 'Cascadia Mono',monospace;width:` + strconv.Itoa(cols) + `ch}</style></head><body><pre>` + b.String() + `</pre></body></html>`
}
