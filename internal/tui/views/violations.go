package views

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Violations is the violation history view.
type Violations struct {
	table      *tview.Table
	engine     *rules.Engine
	ruleFilter string
	visible    []rules.Violation
	onSelect   func(rules.Violation)
	mu         sync.Mutex
	ticker     *time.Ticker
	done       chan struct{}
}

// NewViolationsView creates a new Violations view.
func NewViolationsView(engine *rules.Engine) *Violations {
	v := &Violations{
		table:  tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		engine: engine,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
	return v
}

// Table returns the underlying tview.Table.
func (v *Violations) Table() *tview.Table { return v.table }

// ItemCount returns the number of recent violations.
func (v *Violations) ItemCount() int {
	if v.engine == nil {
		return 0
	}
	return len(v.engine.RecentViolations())
}

// SetOnSelect sets a callback invoked when Enter is pressed on a violation row.
func (v *Violations) SetOnSelect(fn func(rules.Violation)) {
	v.onSelect = fn
	v.table.SetSelectedFunc(func(row, col int) {
		if viol, ok := v.SelectedViolation(); ok && v.onSelect != nil {
			v.onSelect(viol)
		}
	})
}

// SelectedViolation returns the violation at the currently selected row.
func (v *Violations) SelectedViolation() (rules.Violation, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.visible) {
		return rules.Violation{}, false
	}
	return v.visible[idx], true
}

// SetFilter filters violations by rule name (substring match).
func (v *Violations) SetFilter(text string) {
	v.mu.Lock()
	v.ruleFilter = text
	v.mu.Unlock()
}

// Start begins the periodic refresh loop.
func (v *Violations) Start(app *tview.Application) {
	v.done = make(chan struct{})
	v.ticker = time.NewTicker(2 * time.Second)
	go func() {
		app.QueueUpdateDraw(func() { v.render() })
		for {
			select {
			case <-v.ticker.C:
				app.QueueUpdateDraw(func() { v.render() })
			case <-v.done:
				return
			}
		}
	}()
}

// Stop stops the periodic refresh loop.
func (v *Violations) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
		v.done = nil
	}
}

func (v *Violations) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"TIME", "RULE", "PID", "EXPRESSION", "ACTION", "STATUS", "EVENTS"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 3 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, col, cell)
	}

	if v.engine == nil {
		v.table.SetCell(1, 0, tview.NewTableCell("No rules engine configured").
			SetTextColor(theme.ColorDim).SetSelectable(false))
		return
	}

	all := v.engine.RecentViolations()

	v.visible = v.visible[:0]
	for _, viol := range all {
		if v.ruleFilter != "" && !strings.Contains(strings.ToLower(viol.RuleName), strings.ToLower(v.ruleFilter)) {
			continue
		}
		v.visible = append(v.visible, viol)
	}

	if len(v.visible) == 0 {
		msg := "No violations recorded"
		if v.ruleFilter != "" {
			msg = fmt.Sprintf("No violations for %q", v.ruleFilter)
		}
		v.table.SetCell(1, 0, tview.NewTableCell(msg).
			SetTextColor(theme.ColorDim).SetSelectable(false))
		return
	}

	for i, viol := range v.visible {
		row := i + 1
		ts := viol.CreatedAt().Format("15:04:05.000")

		lastEvt := viol.LastEvent()
		status := string(lastEvt.Kind)
		statusColor := theme.ColorDim
		switch lastEvt.Kind {
		case rules.EventDetected:
			statusColor = theme.ColorYellow
		case rules.EventAction:
			statusColor = theme.ColorLabel
		case rules.EventSent:
			statusColor = theme.ColorRed
		case rules.EventError:
			statusColor = theme.ColorRed
		case rules.EventClosed:
			statusColor = theme.ColorDim
		}

		rowColor := theme.ColorFg
		if !viol.Active {
			rowColor = theme.ColorDim
			statusColor = theme.ColorDim
		}

		expr := viol.Expression
		if len(expr) > 60 {
			expr = expr[:57] + "..."
		}

		v.table.SetCell(row, 0, tview.NewTableCell(ts).SetTextColor(theme.ColorDim))
		v.table.SetCell(row, 1, tview.NewTableCell(viol.RuleName).SetTextColor(rowColor))
		v.table.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%d", viol.PID)).SetTextColor(rowColor))
		v.table.SetCell(row, 3, tview.NewTableCell(expr).SetTextColor(theme.ColorDim).SetExpansion(1))
		v.table.SetCell(row, 4, tview.NewTableCell(viol.ActionName).SetTextColor(rowColor))
		v.table.SetCell(row, 5, tview.NewTableCell(status).SetTextColor(statusColor))
		v.table.SetCell(row, 6, tview.NewTableCell(fmt.Sprintf("%d", len(viol.Events))).SetTextColor(rowColor))
	}

	if sel > 0 && sel < v.table.GetRowCount() {
		v.table.Select(sel, 0)
	}
}
