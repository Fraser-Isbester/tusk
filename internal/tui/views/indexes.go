package views

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// Indexes is the index analysis view.
type Indexes struct {
	table      *tview.Table
	db         *db.DB
	data       []db.IndexInfo
	filterText string
	mu         sync.Mutex
	ticker     *time.Ticker
	done       chan struct{}
}

// NewIndexesView creates a new Indexes view.
func NewIndexesView(database *db.DB) *Indexes {
	v := &Indexes{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		db:    database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
	return v
}

// Table returns the underlying tview.Table.
func (v *Indexes) Table() *tview.Table { return v.table }

// ItemCount returns the number of indexes.
func (v *Indexes) ItemCount() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.data) }

// Start begins the periodic refresh loop.
func (v *Indexes) Start(app *tview.Application) {
	v.done = make(chan struct{})
	v.ticker = time.NewTicker(30 * time.Second)
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
		v.done = nil
	}
}

// SetFilter sets the filter text for searching across all columns.
func (v *Indexes) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
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

	row := 1
	for _, idx := range v.data {
		scans := fmt.Sprintf("%d", idx.Scans)
		size := formatSize(idx.Size)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{idx.Table, idx.IndexName, scans, size} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

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
			scans,
			size,
		}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetTextColor(rowColor)
			if col == 1 {
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
