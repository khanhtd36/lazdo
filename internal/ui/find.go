package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// textFind is a / find box over rendered lines (Overview, run log): typing
// builds the query, n/N step through the matching lines.
type textFind struct {
	typing  bool
	query   string
	matches []int
	idx     int
}

func (f *textFind) active() bool { return f.query != "" }

// edit applies a key while typing: esc clears, enter keeps the query.
func (f *textFind) edit(msg tea.KeyMsg) {
	switch msg.String() {
	case "esc":
		*f = textFind{}
	case "enter":
		f.typing = false
	case "backspace":
		if r := []rune(f.query); len(r) > 0 {
			f.query = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			s := string(msg.Runes)
			if s == "" {
				s = " "
			}
			f.query += s
		}
	}
}

// search finds the lines containing the query, ignoring case and styling.
func (f *textFind) search(lines []string) {
	f.matches, f.idx = nil, 0
	if f.query == "" {
		return
	}
	q := strings.ToLower(f.query)
	for i, l := range lines {
		if strings.Contains(strings.ToLower(ansi.Strip(l)), q) {
			f.matches = append(f.matches, i)
		}
	}
}

func (f *textFind) current() (int, bool) {
	if len(f.matches) == 0 {
		return 0, false
	}
	return f.matches[f.idx], true
}

// next steps to the next (or previous) match, wrapping.
func (f *textFind) next(forward bool) (int, bool) {
	if len(f.matches) == 0 {
		return 0, false
	}
	if forward {
		f.idx = (f.idx + 1) % len(f.matches)
	} else {
		f.idx = (f.idx + len(f.matches) - 1) % len(f.matches)
	}
	return f.current()
}

// status is the find box as shown in a status line; ok is false when idle.
func (f *textFind) status() (string, bool) {
	switch {
	case f.typing:
		return styleSelected.Render("/"+f.query+"▏") + styleDim.Render(fmt.Sprintf("  %d lines · enter keep · esc clear", len(f.matches))), true
	case f.query != "":
		return styleDim.Render(fmt.Sprintf("/%s  %d/%d · n/N next/previous · esc clear", f.query, min(f.idx+1, len(f.matches)), len(f.matches))), true
	}
	return "", false
}
