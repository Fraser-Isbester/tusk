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

// Tables is the tables view.
type Tables struct {
	table  *tview.Table
	db     *db.DB
	data       []db.TableInfo
	filterText string
	mu         sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
}

// NewTablesView creates a new Tables view.
func NewTablesView(database *db.DB) *Tables {
	v := &Tables{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		db:    database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(false)
	return v
}

// Table returns the underlying tview.Table.
func (v *Tables) Table() *tview.Table { return v.table }

// ItemCount returns the number of tables.
func (v *Tables) ItemCount() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.data) }

// Start begins the periodic refresh loop.
func (v *Tables) Start(app *tview.Application) {
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

// Stop stops the periodic refresh loop.
func (v *Tables) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

// SelectedTable returns the table info at the currently selected row.
func (v *Tables) SelectedTable() (db.TableInfo, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.data) {
		return db.TableInfo{}, false
	}
	return v.data[idx], true
}

// SetFilter sets the filter text for searching across all columns.
func (v *Tables) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
}

func (v *Tables) refresh() {
	ctx := context.Background()
	data, err := v.db.GetTables(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.data = data
	v.mu.Unlock()
}

func (v *Tables) render() {
	v.mu.Lock()
	defer v.mu.Unlock()
	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"SCHEMA", "TABLE", "SIZE", "ROWS", "DEAD%", "SEQ/IDX", "LAST VAC"}
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
	for _, t := range v.data {
		// Dead tuple percentage
		var deadPct float64
		total := t.LiveTuples + t.DeadTuples
		if total > 0 {
			deadPct = float64(t.DeadTuples) / float64(total) * 100
		}
		deadStr := fmt.Sprintf("%.1f%%", deadPct)

		// Seq/Idx scan ratio
		var seqIdx string
		if t.IdxScan == 0 {
			seqIdx = "seq only"
		} else {
			seqIdx = fmt.Sprintf("%d/%d", t.SeqScan, t.IdxScan)
		}

		// Last vacuum
		lastVac := timeAgo(t.LastVacuum)
		if t.LastAutoVacuum != nil && (t.LastVacuum == nil || t.LastAutoVacuum.After(*t.LastVacuum)) {
			lastVac = timeAgo(t.LastAutoVacuum)
		}

		sizeStr := formatSize(t.TotalSize)
		rows := fmt.Sprintf("%d", t.LiveTuples)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{t.Schema, t.Name, sizeStr, rows, deadStr, seqIdx, lastVac} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		// Row color
		var rowColor tcell.Color
		switch {
		case deadPct > 10:
			rowColor = theme.ColorRed
		case deadPct > 5:
			rowColor = theme.ColorYellow
		default:
			rowColor = theme.ColorFg
		}

		values := []string{
			t.Schema,
			t.Name,
			sizeStr,
			rows,
			deadStr,
			seqIdx,
			lastVac,
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
