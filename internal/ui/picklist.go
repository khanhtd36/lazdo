package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

// rankFuzzy matches query against texts, tightest matches first: "cmpl"
// should find "complete" before "new comment on the pull request", which
// fuzzy alone scores higher for hitting word starts.
func rankFuzzy(query string, texts []string) []fuzzy.Match {
	if isNumberQuery(query) {
		return findLiteral(query, texts)
	}
	span := func(m fuzzy.Match) int {
		return m.MatchedIndexes[len(m.MatchedIndexes)-1] - m.MatchedIndexes[0]
	}
	// Letters scattered across a long description are noise, not a match.
	limit := max(12, 3*len([]rune(query)))
	var matches []fuzzy.Match
	for _, m := range fuzzy.Find(query, texts) {
		if span(m) <= limit {
			matches = append(matches, m)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return span(matches[i]) < span(matches[j]) })
	return matches
}

// isNumberQuery reports a PR number, like "15050" or "!15050": digits
// scattered through titles and branches would fuzzy-match it, so it is
// matched as written instead.
func isNumberQuery(query string) bool {
	digits := strings.TrimPrefix(query, "!")
	return digits != "" && strings.Trim(digits, "0123456789") == ""
}

func findLiteral(query string, texts []string) []fuzzy.Match {
	var matches []fuzzy.Match
	for i, t := range texts {
		at := strings.Index(t, query)
		if at < 0 {
			continue
		}
		idx := make([]int, len(query))
		for j := range idx {
			idx[j] = at + j
		}
		matches = append(matches, fuzzy.Match{Str: t, Index: i, MatchedIndexes: idx})
	}
	return matches
}

// pickItem is one row of a pickList. Headers group rows and can't be
// selected; a groupStart row starts a group itself (a job above its
// steps); searchOnly rows appear only while a filter is typed.
type pickItem struct {
	header     bool
	groupStart bool
	searchOnly bool
	search     string // what / matches against
	render     func(width int) string
	value      any
}

// pickList is a scrollable, selectable list with / fuzzy filtering, shared
// by the Projects page and the views inside a project.
type pickList struct {
	items  []pickItem
	filter string
	typing bool
	cursor int // index into visible()
	offset int
	// unfocused draws the cursor faintly: the list is beside the one keys
	// go to.
	unfocused bool
}

func (l *pickList) setItems(items []pickItem) {
	l.items = items
	l.clamp(1)
}

// visible returns the indexes of the items to show, in display order.
func (l *pickList) visible() []int {
	var out []int
	if l.filter == "" {
		for i, it := range l.items {
			if !it.searchOnly {
				out = append(out, i)
			}
		}
		return out
	}
	var texts []string
	var idx []int
	for i, it := range l.items {
		if !it.header && it.search != "" {
			texts = append(texts, it.search)
			idx = append(idx, i)
		}
	}
	for _, m := range rankFuzzy(l.filter, texts) {
		out = append(out, idx[m.Index])
	}
	return out
}

func (l *pickList) selected() (pickItem, bool) {
	vis := l.visible()
	if l.cursor < 0 || l.cursor >= len(vis) {
		return pickItem{}, false
	}
	it := l.items[vis[l.cursor]]
	return it, !it.header
}

// move steps the cursor, skipping headers in the direction of travel.
func (l *pickList) move(delta int) {
	l.cursor += delta
	l.clamp(sign(delta))
}

func (l *pickList) clamp(dir int) {
	vis := l.visible()
	l.cursor = max(0, min(l.cursor, len(vis)-1))
	for l.cursor >= 0 && l.cursor < len(vis) && l.items[vis[l.cursor]].header {
		l.cursor += dir
	}
	if l.cursor < 0 || l.cursor >= len(vis) {
		// Ran off the end looking for a row: search the other way.
		l.cursor = max(0, min(l.cursor, len(vis)-1))
		for l.cursor >= 0 && l.cursor < len(vis) && l.items[vis[l.cursor]].header {
			l.cursor -= dir
		}
		l.cursor = max(0, min(l.cursor, len(vis)-1))
	}
}

// key handles list keys. activate reports enter on a row; handled is false
// for keys the list doesn't use, so the caller can act on them.
func (l *pickList) key(msg tea.KeyMsg, height int) (handled, activate bool) {
	k := msg.String()
	if l.typing {
		switch k {
		case "esc":
			l.filter, l.typing, l.cursor = "", false, 0
		case "enter":
			// Like fzf: stop typing and open the selected match.
			l.typing = false
			_, ok := l.selected()
			return true, ok
		case "backspace":
			if r := []rune(l.filter); len(r) > 0 {
				l.filter, l.cursor = string(r[:len(r)-1]), 0
			}
		case "up", "ctrl+k", "ctrl+p":
			l.move(-1)
		case "down", "ctrl+j", "ctrl+n":
			l.move(1)
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				s := string(msg.Runes)
				if s == "" {
					s = " "
				}
				l.filter, l.cursor = l.filter+s, 0
			}
		}
		l.clamp(1)
		return true, false
	}
	switch k {
	case "/":
		l.typing = true
	case "esc":
		if l.filter == "" {
			return false, false
		}
		l.filter, l.cursor = "", 0
		l.clamp(1)
	case "j", "down":
		l.move(1)
	case "k", "up":
		l.move(-1)
	case "ctrl+d", "pgdown":
		l.move(max(1, height/2))
	case "ctrl+u", "pgup":
		l.move(-max(1, height/2))
	case "g", "home":
		l.cursor = 0
		l.clamp(1)
	case "G", "end":
		l.cursor = len(l.visible()) - 1
		l.clamp(-1)
	case "J":
		l.jumpGroup(1)
	case "K":
		l.jumpGroup(-1)
	case "enter":
		_, ok := l.selected()
		return true, ok
	default:
		return false, false
	}
	return true, false
}

// jumpGroup moves to the first row of the next (dir 1) or current/previous
// (dir -1) group; a group starts after a header or at a groupStart row.
func (l *pickList) jumpGroup(dir int) {
	vis := l.visible()
	starts := func(i int) bool {
		it := l.items[vis[i]]
		return !it.header && (it.groupStart || i > 0 && l.items[vis[i-1]].header)
	}
	if dir > 0 {
		for i := l.cursor + 1; i < len(vis); i++ {
			if starts(i) {
				l.cursor = i
				return
			}
		}
		return
	}
	// Back to this group's first row, or the previous group's if already there.
	start := l.cursor
	for start > 0 && !starts(start) {
		start--
	}
	if start == l.cursor {
		for i := start - 1; i >= 0; i-- {
			if starts(i) {
				start = i
				break
			}
		}
	}
	l.cursor = start
	l.clamp(1)
}

// filterLine is shown above the rows while a filter is typed or kept.
func (l *pickList) filterLine() (string, bool) {
	switch {
	case l.typing:
		return styleSelected.Render("/"+l.filter+"▏") + styleDim.Render(fmt.Sprintf("  %d matches · enter open · esc clear", len(l.visible()))), true
	case l.filter != "":
		return styleDim.Render(fmt.Sprintf("/%s  %d matches · / edit · esc clear", l.filter, len(l.visible()))), true
	}
	return "", false
}

// view renders height lines.
func (l *pickList) view(width, height int) []string {
	out := make([]string, 0, height)
	if line, ok := l.filterLine(); ok {
		out = append(out, truncate(line, width))
		height--
	}
	vis := l.visible()
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+height {
		l.offset = l.cursor - height + 1
	}
	l.offset = max(0, min(l.offset, max(0, len(vis)-height)))
	for i := l.offset; i < min(len(vis), l.offset+height); i++ {
		it := l.items[vis[i]]
		prefix := "  "
		if i == l.cursor && !it.header {
			prefix = styleCursor.Render("▌ ")
			if l.unfocused {
				prefix = styleDim.Render("▏ ")
			}
		}
		out = append(out, truncate(prefix+it.render(width-2), width))
	}
	if len(vis) == 0 {
		msg := "nothing here"
		if l.filter != "" {
			msg = "no match"
		}
		out = append(out, styleDim.Render("  "+msg))
	}
	for len(out) < cap(out) {
		out = append(out, "")
	}
	return out
}

// click selects the row at screen row y of the list's view; a click on the
// selected row activates it.
func (l *pickList) click(y int) (activate bool) {
	if _, ok := l.filterLine(); ok {
		y--
	}
	vis := l.visible()
	i := l.offset + y
	if y < 0 || i >= len(vis) || l.items[vis[i]].header {
		return false
	}
	if i == l.cursor {
		return true
	}
	l.cursor = i
	return false
}

func (l *pickList) wheel(delta int) { l.move(delta) }

// text joins rendered strings for a row, dimming the secondary parts.
func joinCols(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "  ")
}
