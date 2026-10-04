package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
	"github.com/khanhtd36/lazdo/internal/ui"
)

// Set by goreleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	org := flag.String("org", "", "Azure DevOps organization name or URL (default: az devops configure default)")
	interval := flag.Duration("interval", 2*time.Minute, "auto-refresh interval")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("lazdo %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	if err := run(*org, *interval); err != nil {
		fmt.Fprintln(os.Stderr, "lazdo:", err)
		os.Exit(1)
	}
}

func run(org string, interval time.Duration) error {
	if org == "" {
		var err error
		if org, err = ado.DefaultOrg(); err != nil {
			return err
		}
	}
	model := ui.New(ado.NewClient(ado.OrgName(org)), interval)
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
