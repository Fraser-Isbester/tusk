package views

import (
	"context"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// Databases is the databases view.
type Databases struct {
	table     *tview.Table
	db        *db.DB
	databases []db.DatabaseInfo
	mu        sync.Mutex
	ticker    *time.Ticker
	done      chan struct{}
}

// NewDatabasesView creates a new Databases view.
func NewDatabasesView(database *db.DB) *Databases {
	v := &Databases{
		table: tview.NewTable().
			SetSelectable(true, false).
			SetFixed(1, 0).
			SetSelectedStyle(theme.SelectedStyle),
		db: database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(false)
	return v
}

// Table returns the underlying tview.Table.
func (v *Databases) Table() *tview.Table { return v.table }

// ItemCount returns the number of databases.
func (v *Databases) ItemCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.databases)
}

// Start begins the background refresh loop.
func (v *Databases) Start(app *tview.Application) {
	v.done = make(chan struct{})
	v.ticker = time.NewTicker(5 * time.Second)
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

// Stop stops the background refresh loop.
func (v *Databases) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

func (v *Databases) refresh() {
	ctx := context.Background()
	databases, err := v.db.GetDatabases(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.databases = databases
	v.mu.Unlock()
}

func (v *Databases) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	selectedRow, _ := v.table.GetSelection()

	v.table.Clear()

	headers := []string{"NAME", "SIZE", "OWNER"}
	for i, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if i == 0 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, i, cell)
	}

	for i, d := range v.databases {
		row := i + 1
		color := tcell.ColorWhite

		v.table.SetCell(row, 0, tview.NewTableCell(d.Name).SetTextColor(color).SetExpansion(1))
		v.table.SetCell(row, 1, tview.NewTableCell(formatSize(d.Size)).SetTextColor(color))
		v.table.SetCell(row, 2, tview.NewTableCell(d.Owner).SetTextColor(color))
	}

	if selectedRow > 0 && selectedRow < v.table.GetRowCount() {
		v.table.Select(selectedRow, 0)
	}
}
