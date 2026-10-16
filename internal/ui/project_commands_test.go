package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// inDemo is the dashboard with the Demo project open on its Repos tab.
func inDemo(t *testing.T) Model {
	t.Helper()
	m := dashboardWith(t, 7)
	m.projects = fakeProjectsPage()
	m.project = m.projects.newProject(m.client, ado.ProjectInfo{ID: "p1", Name: "Demo"})
	m.project.width, m.project.height = m.width, m.height
	return m
}

func commandLabels(g commandGroup) string {
	labels := make([]string, 0, len(g.commands))
	for _, c := range g.commands {
		labels = append(labels, ansi.Strip(c.label))
	}
	return strings.Join(labels, " | ")
}

func TestPaletteOnAProjectRepo(t *testing.T) {
	m := inDemo(t)
	m, _ = sendKeys(m, ":")
	p := openedPalette(t, m)
	if titles := groupTitles(p); strings.Join(titles, " | ") != "Repo Demo | Project Demo | Organization org" {
		t.Fatalf("groups: %v", titles)
	}
	if got := commandLabels(p.groups[1]); !strings.Contains(got, "Go to Pipelines") || strings.Contains(got, "Go to Repos") {
		t.Fatalf("the project offers its other tabs: %s", got)
	}
	m = typeText(m, "go to pipelines")
	m, cmd := sendKeys(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.project.tab != projTabPipelines {
		t.Fatalf("the command switched tab, on %v", m.project.tab)
	}
}

func TestPaletteOnAPipelineOffersItsLatestRun(t *testing.T) {
	m := inDemo(t)
	m.project.tab = projTabPipelines
	m.project.pipelinesLoaded = true
	run := ado.Run{ID: 41, BuildNumber: "2026.41", Status: "completed", Result: "failed"}
	m.project.pipelines = []ado.Pipeline{{ID: 3, Name: "web", LatestBuild: &run}}
	m.project.lists[projTabPipelines].setItems(m.project.pipelineItems())
	m, _ = sendKeys(m, ":")
	titles := groupTitles(openedPalette(t, m))
	if titles[0] != "Pipeline web" || titles[1] != "Latest run 2026.41 · failed" {
		t.Fatalf("the pipeline, then its latest run: %v", titles)
	}
	m = typeText(m, "open the run")
	m, cmd := sendKeys(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.project.level != levelRun || m.project.run == nil || m.project.run.run.ID != 41 || m.project.pipeline.ID != 3 {
		t.Fatalf("the latest run opens (level %v)", m.project.level)
	}
}

func TestPaletteInTheRepoBrowser(t *testing.T) {
	m := inDemo(t)
	m.project.level, m.project.repo = levelRepo, ado.Repo{ID: "r", Name: "repo"}
	m.project.browser = fakeBrowser(t)
	m.project.browser.pane = paneFiles
	m, _ = sendKeys(m, ":")
	titles := groupTitles(openedPalette(t, m))
	want := []string{"Folder /src", "Branch develop", "Files", "Repo repo", "Project Demo"}
	if strings.Join(titles[:5], " | ") != strings.Join(want, " | ") {
		t.Fatalf("groups: %v", titles)
	}
	m, _ = sendKeys(m, "esc")
	m.project.browser.pane = paneBranches
	m, _ = sendKeys(m, ":")
	p := openedPalette(t, m)
	if p.groups[0].title != "Branch develop" {
		t.Fatalf("on the branch pane, the branch: %v", groupTitles(p))
	}
	for _, c := range p.groups[0].commands {
		if c.id == "Delete branch" && c.disabled == "" {
			t.Fatal("the default branch can't be deleted")
		}
	}
}

func TestPaletteOnSettings(t *testing.T) {
	m := inDemo(t)
	m.project.tab = projTabSettings
	m.project.settings.data = &ado.ProjectSettings{}
	m.project.settings.section = secPolicies
	m, _ = sendKeys(m, ":")
	p := openedPalette(t, m)
	if p.groups[0].title != "Settings · "+secPolicies.title() || !strings.Contains(commandLabels(p.groups[0]), "New branch policy…") {
		t.Fatalf("the section's commands: %v / %s", groupTitles(p), commandLabels(p.groups[0]))
	}
}
