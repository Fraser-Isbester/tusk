package views

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// SlowQueries is the slow queries view.
type SlowQueries struct {
	table  *tview.Table
	db     *db.DB
	data   []db.SlowQuery
	mu     sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
}

// NewSlowQueriesView creates a new SlowQueries view.
func NewSlowQueriesView(database *db.DB) *SlowQueries {
	v := &SlowQueries{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		db:    database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(false)
	return v
}

// Table returns the underlying tview.Table.
func (v *SlowQueries) Table() *tview.Table { return v.table }

// ItemCount returns the number of slow queries.
func (v *SlowQueries) ItemCount() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.data) }

// Start begins the periodic refresh loop.
func (v *SlowQueries) Start(app *tview.Application) {
	v.done = make(chan struct{})
	v.ticker = time.NewTicker(10 * time.Second)
	go func() {
		v.refresh()
		app.QueueUpdateDraw(func() { v.render() })
		for {
			select {
			case <-v.ticker.C:
				v.refresh()
				app.QueueUpdateDraw(func() { v.render() })
			case <-v.done:
				return
			}
		}
	}()
}

// Stop stops the periodic refresh loop.
func (v *SlowQueries) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

func (v *SlowQueries) refresh() {
	ctx := context.Background()
	data, err := v.db.GetSlowQueries(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.data = data
	v.mu.Unlock()
}

func (v *SlowQueries) render() {
	v.mu.Lock()
	defer v.mu.Unlock()
	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"QUERY", "CALLS", "TOTAL", "MEAN", "ROWS/CALL", "HIT%"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 0 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, col, cell)
	}

	for i, s := range v.data {
		row := i + 1
		rowsPerCall := int64(0)
		if s.Calls > 0 {
			rowsPerCall = s.Rows / s.Calls
		}

		var totalStr string
		if s.TotalTime >= 1000 {
			totalStr = formatDuration(time.Duration(s.TotalTime) * time.Millisecond)
		} else {
			totalStr = fmt.Sprintf("%.0fms", s.TotalTime)
		}

		var meanStr string
		if s.MeanTime >= 1000 {
			meanStr = formatDuration(time.Duration(s.MeanTime) * time.Millisecond)
		} else {
			meanStr = fmt.Sprintf("%.0fms", s.MeanTime)
		}

		values := []string{
			truncate(s.Query, 80),
			fmt.Sprintf("%d", s.Calls),
			totalStr,
			meanStr,
			fmt.Sprintf("%d", rowsPerCall),
			fmt.Sprintf("%.1f%%", s.HitRatio*100),
		}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetTextColor(theme.ColorFg)
			if col == 0 {
				cell.SetExpansion(1)
			}
			v.table.SetCell(row, col, cell)
		}
	}

	if sel > 0 && sel < v.table.GetRowCount() {
		v.table.Select(sel, 0)
	}
}
