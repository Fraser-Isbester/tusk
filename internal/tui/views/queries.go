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

// Queries is the active queries view.
type Queries struct {
	table        *tview.Table
	db           *db.DB
	queries      []db.ActiveQuery
	visibleData  []db.ActiveQuery
	userFilter   string
	filterText   string
	queryHistory *db.QueryHistory
	mu           sync.Mutex
	ticker       *time.Ticker
	done         chan struct{}
}

// NewQueriesView creates a new Queries view.
func NewQueriesView(database *db.DB) *Queries {
	v := &Queries{
		table: tview.NewTable().
			SetSelectable(true, false).
			SetFixed(1, 0).
			SetSelectedStyle(theme.SelectedStyle),
		db: database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(false)

	v.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		row, _ := v.table.GetSelection()
		if row < 1 {
			return event
		}
		pid := v.pidAtRow(row)
		if pid == 0 {
			return event
		}
		ctx := context.Background()
		switch event.Rune() {
		case 'c':
			_ = v.db.CancelQuery(ctx, pid)
			v.refresh()
			return nil
		case 't':
			_ = v.db.TerminateBackend(ctx, pid)
			v.refresh()
			return nil
		}
		return event
	})

	return v
}

// Table returns the underlying tview.Table.
func (v *Queries) Table() *tview.Table { return v.table }

// ItemCount returns the number of visible queries.
func (v *Queries) ItemCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	count := 0
	for _, q := range v.queries {
		if q.User != "(system)" {
			count++
		}
	}
	return count
}

// Start begins the background refresh loop.
func (v *Queries) Start(app *tview.Application) {
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
func (v *Queries) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

func (v *Queries) refresh() {
	ctx := context.Background()
	queries, err := v.db.GetActiveQueries(ctx)
	if err != nil {
		return
	}
	if v.queryHistory != nil {
		v.queryHistory.RecordAll(queries)
	}
	v.mu.Lock()
	v.queries = queries
	v.mu.Unlock()
}

func (v *Queries) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	selectedRow, _ := v.table.GetSelection()

	v.table.Clear()

	headers := []string{"PID", "USER", "APP", "STATE", "WAIT", "DURATION", "STMTS"}
	for i, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if i == 2 || i == 4 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, i, cell)
	}

	v.visibleData = v.visibleData[:0]
	row := 1
	for _, q := range v.queries {
		if q.User == "(system)" {
			continue
		}
		// Only show actively running queries.
		if q.State != "active" {
			continue
		}
		if v.userFilter != "" && q.User != v.userFilter {
			continue
		}
		v.visibleData = append(v.visibleData, q)

		pid := fmt.Sprintf("%d", q.PID)
		wait := q.WaitEventType
		if q.WaitEvent != "" {
			wait += ":" + q.WaitEvent
		}
		durStr := ""
		if q.Duration > 0 && q.State == "active" {
			durStr = formatDuration(q.Duration)
		}

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{pid, q.User, q.AppName, q.State, wait, durStr} {
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
		isLongDur := q.Duration >= 30*time.Second
		if isLongDur {
			color = theme.ColorRed
		} else if q.Duration >= 1*time.Second {
			color = theme.ColorYellow
		}

		v.table.SetCell(row, 0, tview.NewTableCell(pid).SetTextColor(color))
		v.table.SetCell(row, 1, tview.NewTableCell(q.User).SetTextColor(color))
		v.table.SetCell(row, 2, tview.NewTableCell(q.AppName).SetTextColor(color).SetExpansion(1))

		v.table.SetCell(row, 3, tview.NewTableCell(q.State).SetTextColor(color))

		v.table.SetCell(row, 4, tview.NewTableCell(wait).SetTextColor(color).SetExpansion(1))

		durCell := tview.NewTableCell(durStr).SetTextColor(color)
		if isLongDur {
			durCell.SetAttributes(tcell.AttrBlink)
		}
		v.table.SetCell(row, 5, durCell)

		stmtCount := countStatements(q.Query)
		v.table.SetCell(row, 6, tview.NewTableCell(fmt.Sprintf("%d", stmtCount)).SetTextColor(color))
		row++
	}

	if selectedRow > 0 && selectedRow < v.table.GetRowCount() {
		v.table.Select(selectedRow, 0)
	}
}

// SelectedQuery returns the query at the currently selected row.
func (v *Queries) SelectedQuery() (db.ActiveQuery, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.visibleData) {
		return db.ActiveQuery{}, false
	}
	return v.visibleData[idx], true
}

// SetFilter sets the filter text for searching across all columns.
func (v *Queries) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
}

// SetUserFilter restricts the view to queries from a specific user.
func (v *Queries) SetUserFilter(user string) {
	v.mu.Lock()
	v.userFilter = user
	v.mu.Unlock()
}

// SetQueryHistory sets the shared query history tracker.
func (v *Queries) SetQueryHistory(h *db.QueryHistory) {
	v.queryHistory = h
}

func (v *Queries) pidAtRow(row int) int {
	cell := v.table.GetCell(row, 0)
	if cell == nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(cell.Text, "%d", &pid); err != nil {
		return 0
	}
	return pid
}
