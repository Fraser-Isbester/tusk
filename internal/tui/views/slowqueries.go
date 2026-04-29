package views

import (
	"context"
	"fmt"
	"strings"
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
	data       []db.SlowQuery
	filterText string
	mu         sync.Mutex
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
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
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

// SetFilter sets the filter text for searching across all columns.
func (v *SlowQueries) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
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

	row := 1
	for _, s := range v.data {
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

		calls := fmt.Sprintf("%d", s.Calls)
		rowsStr := fmt.Sprintf("%d", rowsPerCall)
		hitStr := fmt.Sprintf("%.1f%%", s.HitRatio*100)
		queryStr := truncate(s.Query, 80)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{queryStr, calls, totalStr, meanStr, rowsStr, hitStr} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		values := []string{
			queryStr,
			calls,
			totalStr,
			meanStr,
			rowsStr,
			hitStr,
		}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetTextColor(theme.ColorFg)
			if col == 0 {
				cell.SetExpansion(1)
			}
			v.table.SetCell(row, col, cell)
		}
		row++
	}

	if sel > 0 && sel < v.table.GetRowCount() {
		v.table.Select(sel, 0)
	}
}
