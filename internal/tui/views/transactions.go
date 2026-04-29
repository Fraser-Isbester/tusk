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

// Transactions is the transaction monitor view.
type Transactions struct {
	table       *tview.Table
	db          *db.DB
	data        []db.Transaction
	visibleData []db.Transaction
	mu          sync.Mutex
	ticker      *time.Ticker
	done        chan struct{}
}

// NewTransactionsView creates a new Transactions view.
func NewTransactionsView(database *db.DB) *Transactions {
	v := &Transactions{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		db:    database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(false)
	v.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 't' {
			v.terminateSelected()
			return nil
		}
		return event
	})
	return v
}

// Table returns the underlying tview.Table.
func (v *Transactions) Table() *tview.Table { return v.table }

// ItemCount returns the number of transactions.
func (v *Transactions) ItemCount() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.data) }

// Start begins the periodic refresh loop.
func (v *Transactions) Start(app *tview.Application) {
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
func (v *Transactions) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

func (v *Transactions) refresh() {
	ctx := context.Background()
	data, err := v.db.GetTransactions(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.data = data
	v.mu.Unlock()
}

func (v *Transactions) render() {
	v.mu.Lock()
	defer v.mu.Unlock()
	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"PID", "USER", "APP", "STATE", "TXN AGE", "Q AGE"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 2 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, col, cell)
	}

	v.visibleData = v.visibleData[:0]
	row := 1
	for _, txn := range v.data {
		if txn.User == "(system)" {
			continue
		}
		v.visibleData = append(v.visibleData, txn)

		var rowColor tcell.Color
		switch {
		case txn.State == "idle in transaction":
			rowColor = theme.ColorRed
		case txn.XactDuration >= 60*time.Second:
			rowColor = theme.ColorRed
		case txn.XactDuration >= 30*time.Second:
			rowColor = theme.ColorYellow
		default:
			rowColor = theme.ColorFg
		}

		values := []string{
			fmt.Sprintf("%d", txn.PID),
			txn.User,
			txn.AppName,
			txn.State,
			formatDuration(txn.XactDuration),
			formatDuration(txn.QueryDuration),
		}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetTextColor(rowColor)
			if col == 2 {
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

// SelectedTransaction returns the transaction at the currently selected row.
func (v *Transactions) SelectedTransaction() (db.Transaction, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.visibleData) {
		return db.Transaction{}, false
	}
	return v.visibleData[idx], true
}

func (v *Transactions) terminateSelected() {
	row, _ := v.table.GetSelection()
	if row < 1 || row >= v.table.GetRowCount() {
		return
	}
	pidCell := v.table.GetCell(row, 0)
	if pidCell == nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(pidCell.Text, "%d", &pid); err != nil {
		return
	}
	go func() {
		ctx := context.Background()
		_ = v.db.TerminateBackend(ctx, pid)
	}()
}
