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

// Locks is the lock contention view.
type Locks struct {
	table  *tview.Table
	db     *db.DB
	data   []db.LockInfo
	mu     sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
}

// NewLocksView creates a new Locks view.
func NewLocksView(database *db.DB) *Locks {
	v := &Locks{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		db:    database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(false)
	v.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 't' {
			v.terminateBlocker()
			return nil
		}
		return event
	})
	return v
}

// Table returns the underlying tview.Table.
func (v *Locks) Table() *tview.Table { return v.table }

// ItemCount returns the number of lock entries.
func (v *Locks) ItemCount() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.data) }

// Start begins the periodic refresh loop.
func (v *Locks) Start(app *tview.Application) {
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

// Stop stops the periodic refresh loop.
func (v *Locks) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

func (v *Locks) refresh() {
	ctx := context.Background()
	data, err := v.db.GetLocks(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.data = data
	v.mu.Unlock()
}

func (v *Locks) render() {
	v.mu.Lock()
	defer v.mu.Unlock()
	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"BLOCKED", "BLOCKER", "TYPE", "MODE", "WAIT", "BLOCKER APP"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 5 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, col, cell)
	}

	for i, l := range v.data {
		row := i + 1
		values := []string{
			fmt.Sprintf("%d", l.BlockedPID),
			fmt.Sprintf("%d", l.BlockingPID),
			l.LockType,
			l.Mode,
			formatDuration(l.WaitDuration),
			l.BlockingApp,
		}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetTextColor(theme.ColorYellow)
			if col == 5 {
				cell.SetExpansion(1)
			}
			v.table.SetCell(row, col, cell)
		}
	}

	if sel > 0 && sel < v.table.GetRowCount() {
		v.table.Select(sel, 0)
	}
}

func (v *Locks) terminateBlocker() {
	row, _ := v.table.GetSelection()
	if row < 1 || row >= v.table.GetRowCount() {
		return
	}
	blockerCell := v.table.GetCell(row, 1)
	if blockerCell == nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(blockerCell.Text, "%d", &pid); err != nil {
		return
	}
	go func() {
		ctx := context.Background()
		_ = v.db.TerminateBackend(ctx, pid)
	}()
}
