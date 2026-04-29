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

// Connections is the connections view.
type Connections struct {
	table *tview.Table
	db    *db.DB
	conns      []db.ConnectionGroup
	filterText string
	mu         sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
}

// NewConnectionsView creates a new Connections view.
func NewConnectionsView(database *db.DB) *Connections {
	v := &Connections{
		table: tview.NewTable().
			SetSelectable(true, false).
			SetFixed(1, 0).
			SetSelectedStyle(theme.SelectedStyle),
		db: database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
	return v
}

// Table returns the underlying tview.Table.
func (v *Connections) Table() *tview.Table { return v.table }

// ItemCount returns the number of connection groups.
func (v *Connections) ItemCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.conns)
}

// Start begins the background refresh loop.
func (v *Connections) Start(app *tview.Application) {
	v.done = make(chan struct{})
	v.ticker = time.NewTicker(2 * time.Second)
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
func (v *Connections) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

// SelectedUser returns the user from the currently selected connection group.
func (v *Connections) SelectedUser() (string, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.conns) {
		return "", false
	}
	return v.conns[idx].User, true
}

// SetFilter sets the filter text for searching across all columns.
func (v *Connections) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
}

func (v *Connections) refresh() {
	ctx := context.Background()
	conns, err := v.db.GetConnections(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.conns = conns
	v.mu.Unlock()
}

func (v *Connections) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	selectedRow, _ := v.table.GetSelection()

	v.table.Clear()

	headers := []string{"USER", "APPLICATION", "STATE", "COUNT"}
	for i, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if i == 1 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, i, cell)
	}

	row := 1
	for _, c := range v.conns {
		count := fmt.Sprintf("%d", c.Count)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{c.User, c.App, c.State, count} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		color := tcell.ColorWhite
		v.table.SetCell(row, 0, tview.NewTableCell(c.User).SetTextColor(color))
		v.table.SetCell(row, 1, tview.NewTableCell(c.App).SetTextColor(color).SetExpansion(1))
		v.table.SetCell(row, 2, tview.NewTableCell(c.State).SetTextColor(color))
		v.table.SetCell(row, 3, tview.NewTableCell(count).SetTextColor(color))
		row++
	}

	if selectedRow > 0 && selectedRow < v.table.GetRowCount() {
		v.table.Select(selectedRow, 0)
	}
}
