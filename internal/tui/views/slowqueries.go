package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
)

const slowQueriesRefreshInterval = 10 * time.Second

// slowQueriesDataMsg carries fetched slow query data.
type slowQueriesDataMsg struct {
	queries []db.SlowQuery
	err     error
}

// slowQueriesTickMsg triggers the next fetch cycle.
type slowQueriesTickMsg struct{}

// SlowQueries is the slow queries view.
type SlowQueries struct {
	db     *db.DB
	table  table.Model
	width  int
	height int
	paused bool
	data   []db.SlowQuery
	err    error
}

// NewSlowQueries creates a new SlowQueries view.
func NewSlowQueries(database *db.DB) *SlowQueries {
	cols := []table.Column{
		{Title: "QUERY", Width: 30},
		{Title: "CALLS", Width: 8},
		{Title: "TOTAL", Width: 10},
		{Title: "MEAN", Width: 10},
		{Title: "ROWS/CALL", Width: 10},
		{Title: "HIT%", Width: 6},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)
	t.SetStyles(defaultTableStyles())
	return &SlowQueries{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *SlowQueries) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table.SetWidth(w)
	v.table.SetHeight(h - 2)
}

// ItemCount returns the number of slow queries.
func (v *SlowQueries) ItemCount() int { return len(v.data) }

// Init starts the first data fetch.
func (v *SlowQueries) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *SlowQueries) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case slowQueriesDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.data = msg.queries
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case slowQueriesTickMsg:
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

// View renders the slow queries table.
func (v *SlowQueries) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}
	return TableBorder.Render(v.table.View())
}

func (v *SlowQueries) updateRows() {
	var rows []table.Row
	for _, sq := range v.data {
		rowsPerCall := int64(0)
		if sq.Calls > 0 {
			rowsPerCall = sq.Rows / sq.Calls
		}
		row := table.Row{
			truncate(sq.Query, 30),
			fmt.Sprintf("%d", sq.Calls),
			formatDuration(time.Duration(sq.TotalTime * float64(time.Millisecond))),
			formatDuration(time.Duration(sq.MeanTime * float64(time.Millisecond))),
			fmt.Sprintf("%d", rowsPerCall),
			fmt.Sprintf("%.1f%%", sq.HitRatio*100),
		}
		rows = append(rows, row)
	}
	v.table.SetRows(rows)
}

func (v *SlowQueries) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		queries, err := v.db.GetSlowQueries(ctx)
		if err != nil {
			return slowQueriesDataMsg{err: err}
		}
		return slowQueriesDataMsg{queries: queries}
	}
}

func (v *SlowQueries) tick() tea.Cmd {
	return tea.Tick(slowQueriesRefreshInterval, func(time.Time) tea.Msg {
		return slowQueriesTickMsg{}
	})
}
