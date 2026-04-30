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
	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// Queries is the active queries view.
type Queries struct {
	table        *tview.Table
	db           *db.DB
	queries      []db.Query
	completed    []db.Query // recently finished queries shown in grey
	visibleData  []db.Query
	engine       *rules.Engine
	userFilter   string
	filterText   string
	queryHistory *db.QueryHistory
	prevPIDs     map[int]db.Query // previous poll's active queries
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
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)

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
		v.done = nil
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

	// Build set of currently active PIDs
	currentPIDs := make(map[int]bool)
	for _, q := range queries {
		if q.State == "active" {
			currentPIDs[q.PID] = true
		}
	}

	v.mu.Lock()

	// Detect queries that were active last poll but aren't anymore → completed
	if v.prevPIDs != nil {
		for pid, prev := range v.prevPIDs {
			if !currentPIDs[pid] && prev.User != "(system)" {
				prev.State = "completed"
				v.completed = append(v.completed, prev)
			}
		}
	}

	// Keep only the last 20 completed entries
	if len(v.completed) > 20 {
		v.completed = v.completed[len(v.completed)-20:]
	}

	// Store current active queries for next comparison
	v.prevPIDs = make(map[int]db.Query)
	for _, q := range queries {
		if q.State == "active" {
			v.prevPIDs[q.PID] = q
		}
	}

	v.queries = queries
	v.mu.Unlock()
}

func (v *Queries) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	selectedRow, _ := v.table.GetSelection()

	v.table.Clear()

	headers := []string{"PID", "USER", "APP", "QHASH", "STATE", "WAIT", "DURATION", "STMTS", "BLOCKED", "RULES"}

	// Get violated PIDs for this tick
	var violatedPIDs map[int]rules.Violation
	if v.engine != nil {
		violatedPIDs = v.engine.ViolatedPIDs()
	}
	for i, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if i == 2 || i == 5 { // APP and WAIT expand
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
			for _, val := range []string{pid, q.User, q.App, q.State, wait, durStr} {
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

		qhash := ""
		if q.QueryID != 0 {
			qhash = fmt.Sprintf("%x", uint64(q.QueryID))
			if len(qhash) > 8 {
				qhash = qhash[:8]
			}
		}

		v.table.SetCell(row, 0, tview.NewTableCell(pid).SetTextColor(color))
		v.table.SetCell(row, 1, tview.NewTableCell(q.User).SetTextColor(color))
		v.table.SetCell(row, 2, tview.NewTableCell(q.App).SetTextColor(color).SetExpansion(1))
		v.table.SetCell(row, 3, tview.NewTableCell(qhash).SetTextColor(theme.ColorDim))
		v.table.SetCell(row, 4, tview.NewTableCell(q.State).SetTextColor(color))
		v.table.SetCell(row, 5, tview.NewTableCell(wait).SetTextColor(color).SetExpansion(1))

		durCell := tview.NewTableCell(durStr).SetTextColor(color)
		if isLongDur {
			durCell.SetAttributes(tcell.AttrBlink)
		}
		v.table.SetCell(row, 6, durCell)

		stmtCount := countStatements(q.QueryText)
		v.table.SetCell(row, 7, tview.NewTableCell(fmt.Sprintf("%d", stmtCount)).SetTextColor(color))

		blockedStr := ""
		if q.BlockedBy > 0 {
			blockedStr = fmt.Sprintf("%d", q.BlockedBy)
		}
		v.table.SetCell(row, 8, tview.NewTableCell(blockedStr).SetTextColor(color))

		ruleStr := ""
		if viol, ok := violatedPIDs[q.PID]; ok {
			ruleStr = viol.RuleName + " " + violationIcon(viol)
		}
		v.table.SetCell(row, 9, tview.NewTableCell(ruleStr).SetTextColor(violationColor(violatedPIDs, q.PID)))
		row++
	}

	// Render completed queries in grey below active ones
	for _, q := range v.completed {
		if v.userFilter != "" && q.User != v.userFilter {
			continue
		}

		pid := fmt.Sprintf("%d", q.PID)
		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{pid, q.User, q.App, "completed"} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		v.visibleData = append(v.visibleData, q)
		grey := theme.ColorDim

		durStr := ""
		if q.Duration > 0 {
			durStr = formatDuration(q.Duration)
		}

		qhash := ""
		if q.QueryID != 0 {
			qhash = fmt.Sprintf("%x", uint64(q.QueryID))
			if len(qhash) > 8 {
				qhash = qhash[:8]
			}
		}

		v.table.SetCell(row, 0, tview.NewTableCell(pid).SetTextColor(grey))
		v.table.SetCell(row, 1, tview.NewTableCell(q.User).SetTextColor(grey))
		v.table.SetCell(row, 2, tview.NewTableCell(q.App).SetTextColor(grey).SetExpansion(1))
		v.table.SetCell(row, 3, tview.NewTableCell(qhash).SetTextColor(grey))
		v.table.SetCell(row, 4, tview.NewTableCell("completed").SetTextColor(grey))
		v.table.SetCell(row, 5, tview.NewTableCell("").SetTextColor(grey).SetExpansion(1))
		v.table.SetCell(row, 6, tview.NewTableCell(durStr).SetTextColor(grey))
		stmtCount := countStatements(q.QueryText)
		v.table.SetCell(row, 7, tview.NewTableCell(fmt.Sprintf("%d", stmtCount)).SetTextColor(grey))
		v.table.SetCell(row, 8, tview.NewTableCell("").SetTextColor(grey))
		v.table.SetCell(row, 9, tview.NewTableCell("").SetTextColor(grey))
		row++
	}

	if selectedRow > 0 && selectedRow < v.table.GetRowCount() {
		v.table.Select(selectedRow, 0)
	}
}

// SelectedQuery returns the query at the currently selected row.
func (v *Queries) SelectedQuery() (db.Query, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.visibleData) {
		return db.Query{}, false
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

// SetEngine sets the rules engine for violation indicators.
func (v *Queries) SetEngine(e *rules.Engine) {
	v.engine = e
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
