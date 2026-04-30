package views

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Transactions is the transaction monitor view.
type Transactions struct {
	table        *tview.Table
	db           *db.DB
	data         []db.Transaction
	completed    []db.Transaction
	visibleData  []db.Transaction
	engine       *rules.Engine
	filterText   string
	queryHistory *db.QueryHistory
	prevPIDs     map[int]db.Transaction
	sortCol      string
	sortAsc      bool
	mu           sync.Mutex
	ticker       *time.Ticker
	done         chan struct{}
}

// SetQueryHistory sets the shared query history tracker.
func (v *Transactions) SetQueryHistory(h *db.QueryHistory) {
	v.queryHistory = h
}

// SetEngine sets the rules engine for violation indicators.
func (v *Transactions) SetEngine(e *rules.Engine) {
	v.engine = e
}

// NewTransactionsView creates a new Transactions view.
func NewTransactionsView(database *db.DB) *Transactions {
	v := &Transactions{
		table:   tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		db:      database,
		sortCol: "TXN AGE",
		sortAsc: false, // longest first
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
	v.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Modifiers()&tcell.ModShift != 0 && event.Key() == tcell.KeyRune {
			col := ""
			switch event.Rune() {
			case 'P':
				col = "PID"
			case 'U':
				col = "USER"
			case 'A':
				col = "APP"
			case 'S':
				col = "STATE"
			case 'T':
				col = "TXN AGE"
			case 'Q':
				col = "Q AGE"
			case 'L':
				col = "LOCKS"
			}
			if col != "" {
				v.toggleSort(col)
				return nil
			}
		}
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
		v.done = nil
	}
}

func (v *Transactions) toggleSort(col string) {
	v.mu.Lock()
	if v.sortCol == col {
		v.sortAsc = !v.sortAsc
	} else {
		v.sortCol = col
		v.sortAsc = true
	}
	v.mu.Unlock()
	v.render()
}

func (v *Transactions) sortData(data []db.Transaction) {
	if v.sortCol == "" {
		return
	}
	sort.SliceStable(data, func(i, j int) bool {
		var less bool
		switch v.sortCol {
		case "PID":
			less = data[i].PID < data[j].PID
		case "USER":
			less = data[i].User < data[j].User
		case "APP":
			less = data[i].App < data[j].App
		case "STATE":
			less = data[i].State < data[j].State
		case "TXN AGE":
			less = data[i].XactDuration < data[j].XactDuration
		case "Q AGE":
			less = data[i].QueryDuration < data[j].QueryDuration
		case "LOCKS":
			less = data[i].LockCount < data[j].LockCount
		default:
			return false
		}
		if !v.sortAsc {
			return !less
		}
		return less
	})
}

// SetFilter sets the filter text for searching across all columns.
func (v *Transactions) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
}

func (v *Transactions) refresh() {
	ctx := context.Background()
	data, err := v.db.GetTransactions(ctx)
	if err != nil {
		return
	}
	if v.queryHistory != nil {
		v.queryHistory.RecordTransactions(data)
	}

	currentPIDs := make(map[int]bool)
	for _, t := range data {
		currentPIDs[t.PID] = true
	}

	v.mu.Lock()
	if v.prevPIDs != nil {
		for pid, prev := range v.prevPIDs {
			if !currentPIDs[pid] && prev.User != "(system)" {
				prev.State = "completed"
				v.completed = append(v.completed, prev)
			}
		}
	}
	if len(v.completed) > 20 {
		v.completed = v.completed[len(v.completed)-20:]
	}

	v.prevPIDs = make(map[int]db.Transaction)
	for _, t := range data {
		v.prevPIDs[t.PID] = t
	}
	v.data = data
	v.mu.Unlock()
}

func (v *Transactions) render() {
	v.mu.Lock()
	defer v.mu.Unlock()
	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"PID", "USER", "APP", "STATE", "TXN AGE", "Q AGE", "QUERIES", "LOCKS", "VIOLATIONS"}
	for i, h := range headers {
		if h == v.sortCol {
			arrow := "▲"
			if !v.sortAsc {
				arrow = "▼"
			}
			headers[i] = h + " " + arrow
		}
	}
	v.sortData(v.data)

	var violatedPIDs map[int]rules.Violation
	if v.engine != nil {
		violatedPIDs = v.engine.ViolatedPIDs()
	}

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
		pid := fmt.Sprintf("%d", txn.PID)
		txnAge := formatDuration(txn.XactDuration)
		qAge := formatDuration(txn.QueryDuration)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{pid, txn.User, txn.App, txn.State, txnAge, qAge} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		v.visibleData = append(v.visibleData, txn)

		color := theme.ColorFg
		v.table.SetCell(row, 0, tview.NewTableCell(pid).SetTextColor(color))
		v.table.SetCell(row, 1, tview.NewTableCell(txn.User).SetTextColor(color))
		v.table.SetCell(row, 2, tview.NewTableCell(txn.App).SetTextColor(color).SetExpansion(1))
		v.table.SetCell(row, 3, tview.NewTableCell(txn.State).SetTextColor(color))
		v.table.SetCell(row, 4, tview.NewTableCell(txnAge).SetTextColor(color))
		v.table.SetCell(row, 5, tview.NewTableCell(qAge).SetTextColor(color))

		queryCount := 1
		if v.queryHistory != nil {
			if entries := v.queryHistory.Get(txn.PID, txn.XactStart); len(entries) > 0 {
				queryCount = len(entries)
			}
		}
		v.table.SetCell(row, 6, tview.NewTableCell(fmt.Sprintf("%d", queryCount)).SetTextColor(color))
		v.table.SetCell(row, 7, tview.NewTableCell(fmt.Sprintf("%d", txn.LockCount)).SetTextColor(color))

		// Violations column
		violStr := ""
		violCol := theme.ColorFg
		if violatedPIDs != nil {
			if viol, ok := violatedPIDs[txn.PID]; ok {
				violStr = viol.RuleName + " " + violationIcon(viol)
				violCol = violationColor(violatedPIDs, txn.PID)
			}
		}
		v.table.SetCell(row, 8, tview.NewTableCell(violStr).SetTextColor(violCol))
		row++
	}

	// Completed transactions in grey
	for _, txn := range v.completed {
		if txn.User == "(system)" {
			continue
		}
		pid := fmt.Sprintf("%d", txn.PID)
		txnAge := formatDuration(txn.XactDuration)
		qAge := formatDuration(txn.QueryDuration)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{pid, txn.User, txn.App, "completed", txnAge, qAge} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		v.visibleData = append(v.visibleData, txn)
		grey := theme.ColorDim

		queryCount := 1
		if v.queryHistory != nil {
			if entries := v.queryHistory.Get(txn.PID, txn.XactStart); len(entries) > 0 {
				queryCount = len(entries)
			}
		}

		v.table.SetCell(row, 0, tview.NewTableCell(pid).SetTextColor(grey))
		v.table.SetCell(row, 1, tview.NewTableCell(txn.User).SetTextColor(grey))
		v.table.SetCell(row, 2, tview.NewTableCell(txn.App).SetTextColor(grey).SetExpansion(1))
		v.table.SetCell(row, 3, tview.NewTableCell("completed").SetTextColor(grey))
		v.table.SetCell(row, 4, tview.NewTableCell(txnAge).SetTextColor(grey))
		v.table.SetCell(row, 5, tview.NewTableCell(qAge).SetTextColor(grey))
		v.table.SetCell(row, 6, tview.NewTableCell(fmt.Sprintf("%d", queryCount)).SetTextColor(grey))
		v.table.SetCell(row, 7, tview.NewTableCell(fmt.Sprintf("%d", txn.LockCount)).SetTextColor(grey))
		v.table.SetCell(row, 8, tview.NewTableCell("").SetTextColor(grey))
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
