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

// Indexes is the index analysis view.
type Indexes struct {
	table  *tview.Table
	db     *db.DB
	data   []db.IndexInfo
	mu     sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
}

// NewIndexesView creates a new Indexes view.
func NewIndexesView(database *db.DB) *Indexes {
	v := &Indexes{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		db:    database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(false)
	return v
}

// Table returns the underlying tview.Table.
func (v *Indexes) Table() *tview.Table { return v.table }

// ItemCount returns the number of indexes.
func (v *Indexes) ItemCount() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.data) }

// Start begins the periodic refresh loop.
func (v *Indexes) Start(app *tview.Application) {
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
func (v *Indexes) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

func (v *Indexes) refresh() {
	ctx := context.Background()
	data, err := v.db.GetIndexes(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.data = data
	v.mu.Unlock()
}

func (v *Indexes) render() {
	v.mu.Lock()
	defer v.mu.Unlock()
	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"TABLE", "INDEX", "SCANS", "SIZE"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 1 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, col, cell)
	}

	for i, idx := range v.data {
		row := i + 1

		var rowColor tcell.Color
		switch {
		case idx.Scans == 0:
			rowColor = theme.ColorRed
		case idx.Scans < 100:
			rowColor = theme.ColorYellow
		default:
			rowColor = theme.ColorFg
		}

		values := []string{
			idx.Table,
			idx.IndexName,
			fmt.Sprintf("%d", idx.Scans),
			formatSize(idx.Size),
		}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetTextColor(rowColor)
			if col == 1 {
				cell.SetExpansion(1)
			}
			v.table.SetCell(row, col, cell)
		}
	}

	if sel > 0 && sel < v.table.GetRowCount() {
		v.table.Select(sel, 0)
	}
}
