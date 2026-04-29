package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/evertras/bubble-table/table"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

const indexesRefreshInterval = 10 * time.Second

// indexesDataMsg carries fetched index data.
type indexesDataMsg struct {
	indexes []db.IndexInfo
	err     error
}

// indexesTickMsg triggers the next fetch cycle.
type indexesTickMsg struct{}

// Indexes is the index analysis view.
type Indexes struct {
	db     *db.DB
	table  table.Model
	width  int
	height int
	paused bool
	data   []db.IndexInfo
	err    error
}

// NewIndexes creates a new Indexes view.
func NewIndexes(database *db.DB) *Indexes {
	cols := []table.Column{
		table.NewFlexColumn("table", "TABLE", 1),
		table.NewFlexColumn("index", "INDEX", 2),
		table.NewColumn("scans", "SCANS", 8),
		table.NewColumn("size", "SIZE", 10),
	}
	t := table.New(cols).
		Focused(true).
		WithPageSize(20).
		Border(NoBorder).
		WithNoPagination().
		HeaderStyle(HeaderStyle).
		HighlightStyle(HighlightStyle)
	return &Indexes{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Indexes) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table = v.table.WithTargetWidth(w).WithPageSize(h - 2)
}

// ItemCount returns the number of indexes.
func (v *Indexes) ItemCount() int { return len(v.data) }

// Init starts the first data fetch.
func (v *Indexes) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *Indexes) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case indexesDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.data = msg.indexes
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case indexesTickMsg:
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

// View renders the indexes table.
func (v *Indexes) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}
	return v.table.View()
}

func (v *Indexes) updateRows() {
	var rows []table.Row
	for _, idx := range v.data {
		rows = append(rows, table.NewRow(table.RowData{
			"table":  idx.Table,
			"index":  idx.IndexName,
			"scans":  fmt.Sprintf("%d", idx.Scans),
			"size":   formatSize(idx.Size),
			"_scans": idx.Scans,
		}))
	}
	v.table = v.table.WithRows(rows).WithRowStyleFunc(func(input table.RowStyleFuncInput) lipgloss.Style {
		scans, _ := input.Row.Data["_scans"].(int64)
		if scans == 0 {
			return lipgloss.NewStyle().Foreground(theme.ColorRed)
		}
		if scans < 100 {
			return lipgloss.NewStyle().Foreground(theme.ColorYellow)
		}
		return lipgloss.NewStyle()
	})
}

func (v *Indexes) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		indexes, err := v.db.GetIndexes(ctx)
		if err != nil {
			return indexesDataMsg{err: err}
		}
		return indexesDataMsg{indexes: indexes}
	}
}

func (v *Indexes) tick() tea.Cmd {
	return tea.Tick(indexesRefreshInterval, func(time.Time) tea.Msg {
		return indexesTickMsg{}
	})
}
