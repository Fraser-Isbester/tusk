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

// Locks is the lock contention view.
type Locks struct {
	table  *tview.Table
	db     *db.DB
	data       []db.Lock
	filterText string
	mu         sync.Mutex
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
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
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
		v.done = nil
	}
}

// SelectedLock returns the lock info at the currently selected row.
func (v *Locks) SelectedLock() (db.Lock, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.data) {
		return db.Lock{}, false
	}
	return v.data[idx], true
}

// SetFilter sets the filter text for searching across all columns.
func (v *Locks) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
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

	row := 1
	for _, l := range v.data {
		blockedPid := fmt.Sprintf("%d", l.BlockedPID)
		blockerPid := fmt.Sprintf("%d", l.BlockingPID)
		wait := formatDuration(l.WaitDuration)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{blockedPid, blockerPid, l.LockType, l.Mode, wait, l.BlockingApp} {
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
			blockedPid,
			blockerPid,
			l.LockType,
			l.Mode,
			wait,
			l.BlockingApp,
		}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetTextColor(theme.ColorYellow)
			if col == 5 {
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
