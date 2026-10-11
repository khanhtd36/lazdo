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
	"github.com/khanhtd36/lazdo/internal/prefs"
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
	ctx := context.Background() // slow is fine; a stalled download stops on its own
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
	// One line, rewritten in place as the download goes.
	last := ""
	err = update.Install(ctx, rs[0], exe, func(s update.Step) {
		if line := s.String(); line != last {
			last = line
			fmt.Print("\r" + line + "\x1b[K")
		}
	})
	if last != "" {
		fmt.Print("\r\x1b[K")
	}
	if err != nil {
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
	pair, err := startPair(org, prefs.Load())
	if err != nil {
		return err
	}
	model := ui.New(ado.NewClientAs(pair.Org, ado.AccountDir(pair.Account), pair.Tenant), interval)
	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// startPair is the org to open and its account: --org, else the last one
// used in lazdo, else az's default org, with az's own login.
func startPair(org string, p prefs.Prefs) (prefs.Pair, error) {
	if org != "" {
		org = ado.OrgName(org)
		if r, ok := p.For(org); ok && accountExists(r.Account) {
			return r, nil
		}
		return prefs.Pair{Org: org}, nil
	}
	if len(p.Recent) > 0 && accountExists(p.Recent[0].Account) {
		return p.Recent[0], nil
	}
	org, err := ado.DefaultOrg()
	return prefs.Pair{Org: org}, err
}

// accountExists is whether a remembered account is still there.
func accountExists(name string) bool {
	if name == "" {
		return true
	}
	_, err := os.Stat(ado.AccountDir(name))
	return err == nil
}
