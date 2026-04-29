package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/evertras/bubble-table/table"
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
		table.NewFlexColumn("name", "Name", 1),
		table.NewColumn("superuser", "Superuser", 10),
		table.NewColumn("createrole", "Create Role", 12),
		table.NewColumn("createdb", "Create DB", 10),
		table.NewColumn("login", "Login", 8),
		table.NewColumn("connlimit", "Conn Limit", 11),
	}

	t := table.New(cols).
		Focused(true).
		WithPageSize(20).
		Border(NoBorder).
		WithNoPagination().
		HeaderStyle(HeaderStyle).
		HighlightStyle(HighlightStyle)

	return &Roles{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Roles) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table = v.table.WithTargetWidth(w).WithPageSize(h - 2)
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

	return v.table.View()
}

func (v *Roles) updateRows() {
	var rows []table.Row
	for _, r := range v.roles {
		connLimit := fmt.Sprintf("%d", r.ConnLimit)
		if r.ConnLimit == -1 {
			connLimit = "unlimited"
		}

		displayCols := []string{r.Name, boolIcon(r.IsSuperuser), boolIcon(r.CanCreateRole), boolIcon(r.CanCreateDB), boolIcon(r.CanLogin), connLimit}
		if v.filterValue == "" || rowContains(displayCols, v.filterValue) {
			rows = append(rows, table.NewRow(table.RowData{
				"name":       r.Name,
				"superuser":  boolIcon(r.IsSuperuser),
				"createrole": boolIcon(r.CanCreateRole),
				"createdb":   boolIcon(r.CanCreateDB),
				"login":      boolIcon(r.CanLogin),
				"connlimit":  connLimit,
			}))
		}
	}
	v.table = v.table.WithRows(rows)
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
