package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/lazdo/internal/ado"
)

// The organization's users, fetched at start and every half hour, so @ in
// a comment finds anyone at once, without a call per key.

const peopleEvery = 30 * time.Minute

// peopleCache is shared by the root and every pull request it opens.
type peopleCache struct {
	users  []ado.Person
	loaded bool
	err    error // listing refused: mentions search live instead
}

type (
	orgPeopleMsg struct {
		from  *ado.Client
		users []ado.Person
		err   error
	}
	orgPeopleTickMsg struct{ from *ado.Client }
	// teamsMsg brings a pull request's project teams, to mention.
	teamsMsg struct {
		prID  int
		teams []ado.Team
	}
)

func (m teamsMsg) pullRequest() int { return m.prID }

// orgUsers lists the organization's users; tests replace it.
var orgUsers = func(ctx context.Context, c *ado.Client) ([]ado.Person, error) { return c.OrgUsers(ctx) }

func loadPeople(client *ado.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		users, err := orgUsers(ctx, client)
		return orgPeopleMsg{from: client, users: users, err: err}
	}
}

func (m Model) onPeople(msg orgPeopleMsg) (tea.Model, tea.Cmd) {
	if msg.from != m.client {
		return m, nil // from the org before a switch
	}
	if msg.err == nil {
		m.people.users, m.people.err = msg.users, nil
	} else if !m.people.loaded {
		m.people.err = msg.err // a failed refresh keeps the list it had
	}
	m.people.loaded = true
	client := m.client
	return m, tea.Tick(peopleEvery, func(time.Time) tea.Msg { return orgPeopleTickMsg{from: client} })
}

// loadTeams fetches the project's teams once, for mentions.
func (d *detailModel) loadTeams() tea.Cmd {
	client, pr := d.client, d.pr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		teams, _ := client.Teams(ctx, pr.Repository.Project.ID)
		return teamsMsg{prID: pr.ID, teams: teams}
	}
}
