package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
)

const connectionsRefreshInterval = 2 * time.Second

// connectionsDataMsg carries fetched connection group data.
type connectionsDataMsg struct {
	conns []db.ConnectionGroup
	err   error
}

// connectionsTickMsg triggers the next fetch cycle.
type connectionsTickMsg struct{}

// Connections is the connections view.
type Connections struct {
	db          *db.DB
	table       table.Model
	width       int
	height      int
	paused      bool
	filterValue string
	conns       []db.ConnectionGroup
	err         error
}

// NewConnections creates a new Connections view.
func NewConnections(database *db.DB) *Connections {
	cols := []table.Column{
		{Title: "User", Width: 16},
		{Title: "Application", Width: 20},
		{Title: "State", Width: 14},
		{Title: "Count", Width: 8},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)

	t.SetStyles(defaultTableStyles())

	return &Connections{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Connections) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table.SetWidth(w)
	v.table.SetHeight(h - 4)
}

// ItemCount returns the number of connection groups.
func (v *Connections) ItemCount() int { return len(v.conns) }

// Init starts the first data fetch.
func (v *Connections) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *Connections) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectionsDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.conns = msg.conns
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case connectionsTickMsg:
		if !v.paused {
			return v, v.fetchData()
		}
		return v, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "p":
			v.paused = !v.paused
			if !v.paused {
				return v, v.fetchData()
			}
			return v, nil
		}
	}

	var cmd tea.Cmd
	v.table, cmd = v.table.Update(msg)
	return v, cmd
}

// View renders the connections table.
func (v *Connections) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}

	return v.table.View()
}

func (v *Connections) updateRows() {
	var rows []table.Row
	for _, c := range v.conns {
		row := table.Row{
			c.User,
			c.AppName,
			c.State,
			fmt.Sprintf("%d", c.Count),
		}
		if v.filterValue == "" || rowContains(row, v.filterValue) {
			rows = append(rows, row)
		}
	}
	v.table.SetRows(rows)
}

func (v *Connections) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		conns, err := v.db.GetConnections(ctx)
		if err != nil {
			return connectionsDataMsg{err: err}
		}
		return connectionsDataMsg{conns: conns}
	}
}

func (v *Connections) tick() tea.Cmd {
	return tea.Tick(connectionsRefreshInterval, func(time.Time) tea.Msg {
		return connectionsTickMsg{}
	})
}
