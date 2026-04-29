package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
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
		{Title: "TABLE", Width: 16},
		{Title: "INDEX", Width: 24},
		{Title: "SCANS", Width: 8},
		{Title: "SIZE", Width: 10},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)
	t.SetStyles(defaultTableStyles())
	return &Indexes{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Indexes) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table.SetWidth(w)
	v.table.SetHeight(h - 2)
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
	return TableBorder.Render(v.table.View())
}

func (v *Indexes) updateRows() {
	var rows []table.Row
	for _, idx := range v.data {
		row := table.Row{
			idx.Table,
			idx.IndexName,
			fmt.Sprintf("%d", idx.Scans),
			formatSize(idx.Size),
		}
		rows = append(rows, row)
	}
	v.table.SetRows(rows)
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
