package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// TestProjectsLive drives the Projects page against the real organization;
// every call it makes is a read. Opt in: LAZDO_LIVE_PROJECTS=1 and
// optionally LAZDO_LIVE_KEYS (space separated, "|" prints the screen).
func TestProjectsLive(t *testing.T) {
	if os.Getenv("LAZDO_LIVE_PROJECTS") == "" {
		t.Skip("set LAZDO_LIVE_PROJECTS=1 to run")
	}
	org, err := ado.DefaultOrg()
	if err != nil {
		t.Fatal(err)
	}
	var m tea.Model = New(ado.NewClient(org), time.Hour)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 24})
	keys := strings.Fields(os.Getenv("LAZDO_LIVE_KEYS"))
	if len(keys) == 0 {
		keys = []string{"2", "|", "/", "l", "a", "z", "d", "o", "|", "esc", "enter", "|", "3", "|", "enter", "|", "enter", "|"}
	}
	for _, k := range keys {
		if k == "|" {
			fmt.Println(ansi.Strip(m.View()))
			fmt.Println("=====")
			continue
		}
		var cmd tea.Cmd
		m, cmd = m.Update(keyMsg(k))
		m = drainRoot(m, cmd, 0)
	}
}

// drainRoot runs commands synchronously, skipping timers so polling and
// auto-refresh don't loop forever.
func drainRoot(m tea.Model, cmd tea.Cmd, depth int) tea.Model {
	if cmd == nil || depth > 20 {
		return m
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(20 * time.Second):
		return m // a tea.Tick: don't wait for timers
	}
	switch msg := msg.(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range msg {
			m = drainRoot(m, c, depth+1)
		}
		return m
	case tickMsg, runTickMsg:
		return m
	}
	var next tea.Cmd
	m, next = m.Update(msg)
	return drainRoot(m, next, depth+1)
}
