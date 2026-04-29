package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
)

const rolesRefreshInterval = 10 * time.Second

// rolesDataMsg carries fetched role data.
type rolesDataMsg struct {
	roles []db.RoleInfo
	err   error
}

// rolesTickMsg triggers the next fetch cycle.
type rolesTickMsg struct{}

// Roles is the roles view.
type Roles struct {
	db          *db.DB
	table       table.Model
	width       int
	height      int
	paused      bool
	filterValue string
	roles       []db.RoleInfo
	err         error
}

// NewRoles creates a new Roles view.
func NewRoles(database *db.DB) *Roles {
	cols := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Superuser", Width: 10},
		{Title: "Create Role", Width: 12},
		{Title: "Create DB", Width: 10},
		{Title: "Login", Width: 8},
		{Title: "Conn Limit", Width: 11},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)

	t.SetStyles(defaultTableStyles())

	return &Roles{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Roles) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table.SetWidth(w)
	v.table.SetHeight(h - 2)
}

// ItemCount returns the number of roles.
func (v *Roles) ItemCount() int { return len(v.roles) }

// Init starts the first data fetch.
func (v *Roles) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *Roles) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case rolesDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.roles = msg.roles
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case rolesTickMsg:
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

// View renders the roles table.
func (v *Roles) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}

	return TableBorder.Render(v.table.View())
}

func (v *Roles) updateRows() {
	var rows []table.Row
	for _, r := range v.roles {
		connLimit := fmt.Sprintf("%d", r.ConnLimit)
		if r.ConnLimit == -1 {
			connLimit = "unlimited"
		}

		row := table.Row{
			r.Name,
			boolIcon(r.IsSuperuser),
			boolIcon(r.CanCreateRole),
			boolIcon(r.CanCreateDB),
			boolIcon(r.CanLogin),
			connLimit,
		}
		if v.filterValue == "" || rowContains(row, v.filterValue) {
			rows = append(rows, row)
		}
	}
	v.table.SetRows(rows)
}

func (v *Roles) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		roles, err := v.db.GetRoles(ctx)
		if err != nil {
			return rolesDataMsg{err: err}
		}
		return rolesDataMsg{roles: roles}
	}
}

func (v *Roles) tick() tea.Cmd {
	return tea.Tick(rolesRefreshInterval, func(time.Time) tea.Msg {
		return rolesTickMsg{}
	})
}
