package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
	"github.com/khanhtd36/lazdo/internal/termprobe"
	"github.com/khanhtd36/lazdo/internal/ui"
	"github.com/khanhtd36/lazdo/internal/update"
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
	symbols := flag.Bool("symbols", false, "draw markers as symbols (↑ “ ★ ● ◌) without checking the terminal")
	ascii := flag.Bool("ascii", false, "draw markers as words ([+3 pushes] [new] [draft])")
	flag.Parse()

	if *showVersion {
		fmt.Printf("lazdo %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	exe, _ := os.Executable()
	update.Cleanup(exe) // the copy a previous update moved aside
	if flag.Arg(0) == "update" {
		if err := selfUpdate(exe); err != nil {
			fmt.Fprintln(os.Stderr, "lazdo:", err)
			os.Exit(1)
		}
		return
	}
	ui.SetVersion(version)
	ui.UseSymbols(chooseSymbols(*symbols, *ascii, os.Getenv("LAZDO_SYMBOLS")))
	if err := run(*org, *interval); err != nil {
		fmt.Fprintln(os.Stderr, "lazdo:", err)
		os.Exit(1)
	}
}

// selfUpdate is `lazdo update`: it lists what newer releases change and
// installs the latest, unless Homebrew or go install manage this copy.
func selfUpdate(exe string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rs, err := update.Newer(ctx, version)
	if err != nil {
		return err
	}
	if len(rs) == 0 {
		fmt.Printf("lazdo %s is the latest.\n", version)
		return nil
	}
	fmt.Printf("lazdo %s → %s\n", version, rs[0].Version())
	for _, c := range update.Changes(rs) {
		fmt.Println("  " + c)
	}
	how := update.HowInstalled(exe)
	if cmd := how.Command(); cmd != "" {
		fmt.Println("This copy is managed elsewhere; update it with:\n  " + cmd)
		return nil
	}
	if err := update.Install(ctx, rs[0], exe); err != nil {
		return err
	}
	fmt.Printf("Updated to %s.\n", rs[0].Version())
	return nil
}

// chooseSymbols decides between marker symbols and words: a flag, then
// LAZDO_SYMBOLS (1 or 0), then whether the terminal draws the symbols one
// column each.
func chooseSymbols(symbols, ascii bool, env string) bool {
	switch {
	case ascii:
		return false
	case symbols:
		return true
	case env == "1":
		return true
	case env == "0":
		return false
	}
	w, ok := termprobe.Width(ui.SymbolSample, 300*time.Millisecond)
	return ok && w == utf8.RuneCountInString(ui.SymbolSample)
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
