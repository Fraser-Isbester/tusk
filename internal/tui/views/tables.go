package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
)

const tablesRefreshInterval = 5 * time.Second

// tablesDataMsg carries fetched table data.
type tablesDataMsg struct {
	tables []db.TableInfo
	err    error
}

// tablesTickMsg triggers the next fetch cycle.
type tablesTickMsg struct{}

// Tables is the tables view.
type Tables struct {
	db          *db.DB
	table       table.Model
	width       int
	height      int
	paused      bool
	filterValue string
	tables      []db.TableInfo
	err         error
}

// NewTables creates a new Tables view.
func NewTables(database *db.DB) *Tables {
	cols := []table.Column{
		{Title: "Schema", Width: 10},
		{Title: "Name", Width: 20},
		{Title: "Size", Width: 10},
		{Title: "Live Rows", Width: 12},
		{Title: "Dead Rows", Width: 12},
		{Title: "Seq Scan", Width: 10},
		{Title: "Idx Scan", Width: 10},
		{Title: "Last Vacuum", Width: 14},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)

	t.SetStyles(defaultTableStyles())

	return &Tables{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Tables) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table.SetWidth(w)
	v.table.SetHeight(h - 2)
}

// ItemCount returns the number of tables.
func (v *Tables) ItemCount() int { return len(v.tables) }

// Init starts the first data fetch.
func (v *Tables) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *Tables) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tablesDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.tables = msg.tables
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case tablesTickMsg:
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

// View renders the tables table.
func (v *Tables) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}

	return TableBorder.Render(v.table.View())
}

func (v *Tables) updateRows() {
	var rows []table.Row
	for _, t := range v.tables {
		lastVac := timeAgo(t.LastVacuum)
		if t.LastAutoVacuum != nil && (t.LastVacuum == nil || t.LastAutoVacuum.After(*t.LastVacuum)) {
			lastVac = timeAgo(t.LastAutoVacuum) + " (auto)"
		}

		row := table.Row{
			t.Schema,
			t.Name,
			formatSize(t.TotalSize),
			fmt.Sprintf("%d", t.LiveTuples),
			fmt.Sprintf("%d", t.DeadTuples),
			fmt.Sprintf("%d", t.SeqScan),
			fmt.Sprintf("%d", t.IdxScan),
			lastVac,
		}
		if v.filterValue == "" || rowContains(row, v.filterValue) {
			rows = append(rows, row)
		}
	}
	v.table.SetRows(rows)
}

func (v *Tables) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		tables, err := v.db.GetTables(ctx)
		if err != nil {
			return tablesDataMsg{err: err}
		}
		return tablesDataMsg{tables: tables}
	}
}

func (v *Tables) tick() tea.Cmd {
	return tea.Tick(tablesRefreshInterval, func(time.Time) tea.Msg {
		return tablesTickMsg{}
	})
}
