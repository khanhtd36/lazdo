package ui

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// @ in a comment: typing @ at the start of a word lists people and teams,
// fuzzy over name and email; the one picked goes into the text as
// "@Sam Rivera" and is posted as Azure DevOps' mention, "@<id>".

const mentionRows = 6

type mentionPerson struct {
	id, name, mail string
	team           bool
}

// mentionBox is a comment editor's mention list and the names it put in.
type mentionBox struct {
	people func() []mentionPerson // the pull request's people first
	live   bool                   // the org's users couldn't be listed: search as you type
	picked map[string]string      // "@Sam Rivera" → id
	query  string
	open   bool
	cursor int
	found  []mentionPerson // a live search's answer
	cache  *peopleCache
	seq    int
	prID   int
	client *ado.Client
}

type (
	mentionTickMsg  struct{ prID, seq int }
	mentionFoundMsg struct {
		prID, seq int
		people    []mentionPerson
	}
)

func (m mentionTickMsg) pullRequest() int  { return m.prID }
func (m mentionFoundMsg) pullRequest() int { return m.prID }

// mentionEditor is a comment editor that can mention: the pull request's
// people, its project's teams and the organization's users.
func (d *detailModel) mentionEditor(title, initial string, onSubmit func(string) tea.Cmd) *editorModal {
	box := &mentionBox{people: d.mentionPeople, picked: map[string]string{}, prID: d.pr.ID, client: d.client, cache: d.people}
	box.live = d.people != nil && d.people.loaded && d.people.err != nil
	shown := box.shrink(initial, d.mentionPeople())
	e := newEditor(title, shown, d.width, func(content string) tea.Cmd { return onSubmit(box.expand(content)) })
	e.mention = box
	return e
}

// mentionPeople are who can be mentioned, the pull request's own first:
// author, reviewers, commenters, then teams, then the organization.
func (d *detailModel) mentionPeople() []mentionPerson {
	var out []mentionPerson
	seen := map[string]bool{}
	add := func(p mentionPerson) {
		if id := strings.ToLower(p.id); p.id != "" && !seen[id] {
			seen[id] = true
			out = append(out, p)
		}
	}
	add(mentionPerson{id: d.pr.CreatedBy.ID, name: d.pr.CreatedBy.DisplayName, mail: d.pr.CreatedBy.UniqueName})
	if d.data != nil {
		for _, r := range d.data.Reviewers {
			add(mentionPerson{id: r.ID, name: r.DisplayName, mail: r.UniqueName, team: r.IsContainer})
		}
		for _, t := range d.data.Threads {
			for _, c := range t.Comments {
				add(mentionPerson{id: c.Author.ID, name: c.Author.DisplayName, mail: c.Author.UniqueName})
			}
		}
	}
	for _, t := range d.teams {
		add(mentionPerson{id: t.ID, name: t.Name, team: true})
	}
	if d.people != nil {
		for _, u := range d.people.users {
			add(mentionPerson{id: u.ID, name: u.Name, mail: u.Mail})
		}
	}
	return out
}

// shrink shows "@<id>" mentions as "@Name", remembering them for expand.
func (b *mentionBox) shrink(text string, people []mentionPerson) string {
	names := map[string]string{}
	for _, p := range people {
		names[strings.ToLower(p.id)] = p.name
	}
	return mentionPattern.ReplaceAllStringFunc(text, func(m string) string {
		id := mentionPattern.FindStringSubmatch(m)[1]
		name, ok := names[strings.ToLower(id)]
		if !ok {
			return m // unknown: left as Azure DevOps wrote it
		}
		b.picked["@"+name] = id
		return "@" + name
	})
}

// expand turns the names picked back into mentions, longest first so
// "@Sam Rivera" isn't caught by "@Sam".
func (b *mentionBox) expand(text string) string {
	names := make([]string, 0, len(b.picked))
	for n := range b.picked {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, n := range names {
		text = strings.ReplaceAll(text, n, "@<"+b.picked[n]+">")
	}
	return text
}

// matches are the people the query finds, best first.
func (b *mentionBox) matches() ([]mentionPerson, [][]int) {
	all := b.people()
	if b.live {
		all = append(all, b.found...)
	}
	if b.query == "" {
		n := min(len(all), mentionRows)
		return all[:n], make([][]int, n)
	}
	texts := make([]string, len(all))
	for i, p := range all {
		texts[i] = p.name + "  " + p.mail
	}
	var out []mentionPerson
	var hits [][]int
	for _, m := range rankFuzzy(b.query, texts) {
		out = append(out, all[m.Index])
		hits = append(hits, m.MatchedIndexes)
		if len(out) == mentionRows {
			break
		}
	}
	return out, hits
}

// mentionQuery is the word after an @ the cursor is in, if any: the @
// starts a word, so a@b.com isn't one.
func (e *editorModal) mentionQuery() (string, bool) {
	lines := strings.Split(e.ta.Value(), "\n")
	row := e.ta.Line()
	if row >= len(lines) {
		return "", false
	}
	li := e.ta.LineInfo()
	line := []rune(lines[row])
	pos := min(len(line), li.StartColumn+li.ColumnOffset)
	before := line[:pos]
	at := -1
	// The @ that starts a word: one inside an email typed after it is
	// part of the query.
	for i := len(before) - 1; i >= 0 && pos-i <= 40; i-- {
		if before[i] == '@' && (i == 0 || unicode.IsSpace(before[i-1])) {
			at = i
			break
		}
	}
	if at < 0 {
		return "", false
	}
	q := string(before[at+1:])
	if strings.Contains(q, "  ") || strings.HasPrefix(q, "<") || strings.HasPrefix(q, " ") {
		return "", false
	}
	return q, true
}

// mentionKey takes the keys the open mention list uses; false for the
// rest, which go to the text.
func (e *editorModal) mentionKey(msg tea.KeyMsg) bool {
	b := e.mention
	if b == nil || !b.open {
		return false
	}
	people, _ := b.matches()
	switch msg.String() {
	case "esc":
		b.open = false
		return true
	case "up", "ctrl+p":
		b.cursor = max(0, b.cursor-1)
		return true
	case "down", "ctrl+n":
		b.cursor = min(max(0, len(people)-1), b.cursor+1)
		return true
	case "tab", "enter":
		if len(people) == 0 {
			b.open = false
			return msg.String() == "tab"
		}
		e.insertMention(people[min(b.cursor, len(people)-1)])
		return true
	}
	return false
}

// insertMention replaces "@que" with "@Sam Rivera ".
func (e *editorModal) insertMention(p mentionPerson) {
	b := e.mention
	for range len([]rune(b.query)) + 1 {
		e.ta, _ = e.ta.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	name := "@" + p.name
	if id, taken := b.picked[name]; taken && !strings.EqualFold(id, p.id) && p.mail != "" {
		name = "@" + p.name + " (" + p.mail + ")" // two people, one name
	}
	b.picked[name] = p.id
	e.ta.InsertString(name + " ")
	b.open, b.query, b.cursor = false, "", 0
}

// afterTyping opens, narrows or closes the mention list for the text now
// before the cursor; a live search waits for a pause.
func (e *editorModal) afterTyping() tea.Cmd {
	b := e.mention
	if b == nil {
		return nil
	}
	q, ok := e.mentionQuery()
	if !ok {
		b.open = false
		return nil
	}
	if !b.open || q != b.query {
		b.cursor = 0
	}
	b.open, b.query = true, q
	if !b.live || q == "" {
		return nil
	}
	b.seq++
	seq, prID := b.seq, b.prID
	return tea.Tick(peopleSearchPause, func(time.Time) tea.Msg { return mentionTickMsg{prID: prID, seq: seq} })
}

func (e *editorModal) onMentionTick(msg mentionTickMsg) tea.Cmd {
	b := e.mention
	if b == nil || msg.seq != b.seq || !b.open {
		return nil
	}
	client, q, seq, prID := b.client, b.query, b.seq, b.prID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		found, _ := searchPeople(ctx, client, q)
		people := make([]mentionPerson, 0, len(found))
		for _, p := range found {
			people = append(people, mentionPerson{id: p.ID, name: p.Name, mail: p.Mail, team: p.IsGroup})
		}
		return mentionFoundMsg{prID: prID, seq: seq, people: people}
	}
}

func (e *editorModal) onMentionFound(msg mentionFoundMsg) {
	if b := e.mention; b != nil && msg.seq == b.seq {
		b.found = msg.people
	}
}

// mentionView is the list under the text box while an @ is typed.
func (e *editorModal) mentionView(width int) string {
	b := e.mention
	if b == nil || !b.open {
		return ""
	}
	people, hits := b.matches()
	if len(people) == 0 {
		note := "no one matches @" + b.query
		if b.cache != nil && !b.cache.loaded {
			note = "loading people…"
		}
		return "\n" + styleDim.Render("  "+note)
	}
	rows := make([]string, 0, len(people))
	for i, p := range people {
		prefix := "  "
		if i == b.cursor {
			prefix = styleSelected.Render("› ")
		}
		text := p.name + "  " + p.mail
		if p.team {
			text = p.name + "  team"
		}
		rows = append(rows, prefix+truncate(markMatches(text, hits[i], len(p.name)), width-2))
	}
	return "\n" + strings.Join(rows, "\n")
}

// markMatches highlights the matched bytes, the name plain and the rest
// dimmed.
func markMatches(text string, hit []int, nameLen int) string {
	on := map[int]bool{}
	for _, i := range hit {
		on[i] = true
	}
	var sb strings.Builder
	for i, r := range text {
		s := string(r)
		switch {
		case on[i]:
			sb.WriteString(styleSelected.Render(s))
		case i >= nameLen:
			sb.WriteString(styleDim.Render(s))
		default:
			sb.WriteString(s)
		}
	}
	return sb.String()
}
