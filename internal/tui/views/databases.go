package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/evertras/bubble-table/table"
	"github.com/fraser-isbester/tusk/internal/db"
)

const databasesRefreshInterval = 5 * time.Second

// databasesDataMsg carries fetched database data.
type databasesDataMsg struct {
	databases []db.DatabaseInfo
	err       error
}

// databasesTickMsg triggers the next fetch cycle.
type databasesTickMsg struct{}

// Databases is the databases view.
type Databases struct {
	db          *db.DB
	table       table.Model
	width       int
	height      int
	paused      bool
	filterValue string
	databases   []db.DatabaseInfo
	err         error
}

// NewDatabases creates a new Databases view.
func NewDatabases(database *db.DB) *Databases {
	cols := []table.Column{
		table.NewFlexColumn("name", "Name", 1),
		table.NewColumn("size", "Size", 10),
		table.NewColumn("owner", "Owner", 12),
	}

	t := table.New(cols).
		Focused(true).
		WithPageSize(20).
		Border(NoBorder).
		WithNoPagination().
		HeaderStyle(HeaderStyle).
		HighlightStyle(HighlightStyle)

	return &Databases{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Databases) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table = v.table.WithTargetWidth(w).WithPageSize(h - 2)
}

// ItemCount returns the number of databases.
func (v *Databases) ItemCount() int { return len(v.databases) }

// Init starts the first data fetch.
func (v *Databases) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *Databases) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case databasesDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.databases = msg.databases
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case databasesTickMsg:
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

// View renders the databases table.
func (v *Databases) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}

	return v.table.View()
}

func (v *Databases) updateRows() {
	var rows []table.Row
	for _, d := range v.databases {
		sizeStr := formatSize(d.Size)
		displayCols := []string{d.Name, sizeStr, d.Owner}
		if v.filterValue == "" || rowContains(displayCols, v.filterValue) {
			rows = append(rows, table.NewRow(table.RowData{
				"name":  d.Name,
				"size":  sizeStr,
				"owner": d.Owner,
			}))
		}
	}
	v.table = v.table.WithRows(rows)
}

func (v *Databases) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		databases, err := v.db.GetDatabases(ctx)
		if err != nil {
			return databasesDataMsg{err: err}
		}
		return databasesDataMsg{databases: databases}
	}
}

func (v *Databases) tick() tea.Cmd {
	return tea.Tick(databasesRefreshInterval, func(time.Time) tea.Msg {
		return databasesTickMsg{}
	})
}
